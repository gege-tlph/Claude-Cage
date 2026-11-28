// Runs the actual workbench with isolated API fixtures. Real core forwarding is
// covered by proxy-chain-smoke.mjs; this script covers user input and recovery
// in the chain editor (proxy-route-editor.jsx) and the settings submit bar.
import assert from 'node:assert/strict';
import fs from 'node:fs/promises';
import path from 'node:path';
import { createWorkbench } from './workbench-fixture.mjs';

const output = path.resolve(process.env.CAGE_CHAIN_QA_OUTPUT || 'ui/.oil-ui/proxy-chain-20261128/shots');
await fs.mkdir(output, { recursive: true });
const f = await createWorkbench();
const { page, state, browser, errors } = f;
const saved = {
 sources: [
  { id:'airport', name:'旅行订阅', nodes:[{id:'entry',name:'🇺🇸 USA Los Angeles 02',type:'anytls'}, {id:'extra',name:'Tokyo Transit',type:'trojan'}]},
  { id:'home',name:'住宅代理',nodes:[{id:'exit',name:'LoopsForge · AI 住宅 IP · 节点001',type:'vless'}]},
 ], selected:'entry',route:['entry'],active:true,running:true,core_available:true,port:17896,state_label:'待启动环境',route_error:'',
};
let failRoute = false, submissions = [];
await page.route('**/api/proxy', r=>r.fulfill({json:saved}));
await page.route('**/api/proxy/route', r=>{
 const body=r.request().postDataJSON();
 submissions.push(body);
 const available=saved.sources.flatMap(s=>s.nodes.map(n=>n.id));
 const valid=body.node_ids.length>0 && body.node_ids.every(id=>available.includes(id)) && new Set(body.node_ids).size===body.node_ids.length;
 const error=failRoute ? '代理核心拒绝该配置；原配置已保留' : !valid ? '代理链节点无效' : '';
 if (!error) { saved.route=[...body.node_ids];saved.selected=body.node_ids.at(-1); }
 state.status.job={id:`route-${submissions.length}`,kind:'proxy',running:false,error,message:error?'':'节点已应用；启动环境后完成联网检查'};
 return r.fulfill({json:{id:state.status.job.id}});
});
const hops = page.locator('.proxy-route-hop');
const bar = page.locator('.settings-submit-bar');
const apply = bar.getByRole('button', { name: /^(保存|应用)$/ });
const node = (name) => page.getByRole('button', { name: `选择节点 ${name}`, exact: true });
const enter = async()=>{
 await page.getByRole('button',{name:/^设置/}).click();
 await page.getByRole('button',{name:'网络与代理',exact:true}).click();
 await page.getByRole('tab',{name:'节点',exact:true}).waitFor();
};
const settle = () => page.locator('.settings-submit.open').waitFor({ state: 'detached' });
try {
 await enter();
 // Switching to a chain keeps the old node as the exit; its up arrow makes it the relay.
 await page.getByRole('button',{name:'链式代理',exact:true}).click();
 assert.equal(await hops.count(),2);
 assert.equal(await apply.isDisabled(),true,'a chain with a hop to choose can be applied');
 await hops.nth(1).getByRole('button',{name:'上移',exact:true}).click();
 assert.match(await hops.nth(0).innerText(), /Los Angeles/);
 assert.match(await hops.nth(1).innerText(), /在下面选择节点/);
 await node('LoopsForge · AI 住宅 IP · 节点001').click();
 assert.match(await hops.nth(1).innerText(), /LoopsForge/);
 assert.equal(submissions.length,0,'editing a chain applied it early');
 await bar.getByText(/链式代理 🇺🇸 USA Los Angeles 02/).waitFor();
 assert.match(await bar.innerText(), /LoopsForge/);
 assert.equal(await bar.locator('svg.route-text-arrow').count(), 1);
 // Leaving or changing the section keeps the draft unless the user discards it.
 await page.getByRole('button',{name:'返回工作台',exact:true}).first().click();
 // ← stays while changes wait; the submit bar flashes instead of a dialog.
 await page.locator('.settings-submit-bar.is-attention').waitFor();
 assert.equal(await page.getByRole('dialog').count(),0,'leaving opened a dialog');
 await page.screenshot({path:path.join(output,'leave-flash.png')});
 await page.locator('.settings-submit-bar.is-attention').waitFor({state:'detached'});
 assert.equal(await hops.count(),2);
 await page.getByRole('button',{name:'通用',exact:true}).click();
 await page.getByRole('dialog',{name:'放弃尚未应用的代理修改？',exact:true}).waitFor();
 await page.getByRole('dialog').getByRole('button',{name:'取消',exact:true}).click();
 assert.equal(await hops.count(),2);
 await page.locator('.settings-content').evaluate(el=>el.scrollTop=0);
 await page.screenshot({path:path.join(output,'chain-draft.png')});
 // One atomic apply from the submit bar.
 await apply.click(); await settle();
 assert.deepEqual(saved.route,['entry','exit']);
 assert.deepEqual(submissions[0],{node_ids:['entry','exit'],port:17896});
 await page.reload();await enter();
 assert.match(await hops.nth(0).innerText(),/Los Angeles/);
 assert.match(await hops.nth(1).innerText(),/LoopsForge/);
 // A second relay, a failed apply that keeps the draft, then the retry.
 await page.getByRole('button',{name:'添加前置',exact:true}).click();
 assert.match(await hops.nth(1).innerText(), /在下面选择节点/);
 await node('Tokyo Transit').click();
 assert.match(await hops.nth(1).innerText(),/Tokyo/);
 failRoute=true;
 await apply.click();
 await page.getByRole('alert').filter({hasText:'代理核心拒绝该配置'}).waitFor();
 assert.deepEqual(saved.route,['entry','exit'],'failed route replaced persisted selection');
 assert.match(await hops.nth(1).innerText(),/Tokyo/,'failed save lost the draft');
 await page.screenshot({path:path.join(output,'failed-apply.png')});
 failRoute=false;
 await apply.click(); await settle();
 assert.deepEqual(saved.route,['entry','extra','exit']);
 // Remove, undo, single node.
 await hops.nth(1).getByRole('button',{name:'移除前置',exact:true}).click();
 assert.equal(await hops.count(),2);
 await bar.getByRole('button',{name:'撤销修改',exact:true}).click();
 assert.equal(await hops.count(),3);
 await page.getByRole('button',{name:'单节点',exact:true}).click();
 assert.equal(await hops.count(),0);
 await apply.click(); await settle();
 assert.deepEqual(saved.route,['exit']);
 // A changed backend list cannot silently shorten a pending route.
 saved.sources[0].nodes=saved.sources[0].nodes.filter(n=>n.id!=='extra');
 await page.getByRole('button',{name:'链式代理',exact:true}).click();
 await node('🇺🇸 USA Los Angeles 02').click();
 await apply.click(); await settle();
 assert.deepEqual(saved.route,['entry','exit']);
 await page.locator('.settings-content').evaluate(el=>el.scrollTop=0);
 await page.locator('.proxy-route').screenshot({path:path.join(output,'chain-panel.png'),animations:'disabled'});
 await page.setViewportSize({width:760,height:900});
 await page.locator('.settings-content').evaluate(el=>el.scrollTop=0);
 const bounds=await page.locator('.proxy-route').boundingBox();
 assert.ok(bounds.x>=0 && bounds.x+bounds.width<=760,'route panel overflows the narrow viewport');
 assert.equal(await page.locator('.settings-page').count(),1);
 await page.evaluate(()=>new Promise(resolve=>requestAnimationFrame(()=>requestAnimationFrame(resolve))));
 await page.screenshot({path:path.join(output,'narrow.png'),animations:'disabled'});
 await page.setViewportSize({width:1280,height:850});
 await page.emulateMedia({colorScheme:'dark',reducedMotion:'reduce'});
 await page.evaluate(()=>new Promise(resolve=>requestAnimationFrame(()=>requestAnimationFrame(resolve))));
 await page.screenshot({path:path.join(output,'dark.png'),animations:'disabled'});
 assert.deepEqual(errors,[]);
 console.log(JSON.stringify({passed:true,submissions:submissions.length,checks:['old exit becomes the relay with its up arrow','list fills the slot being chosen','no early apply','leave and section-change keep the draft','atomic apply from the submit bar','reload persistence','failed apply keeps draft','retry','second relay','remove and undo','single-node mode','narrow viewport'],browserErrors:errors}));
} finally {await browser.close();}
