import React, { useEffect, useRef, useState } from "react";
import { InlineSelect } from "./inline-select";
import "./session-account.css";
import { useTranslation } from "./interface-language.mjs";

const failures = {rate_limit:"请求受到限流，请核对额度后选择等待或接续",authentication_failed:"账号登录失效，请重新登录",account_on_hold:"账号受限，自动接续已停止",overloaded:"服务暂时过载，保留原账号",server_error:"服务暂时异常，保留原账号"};

// Desktop and mobile use the same session operation and durable request ID.
export function SessionAccount({ id, request, onChanged, compact = false, defaultOpen = false, paused = false }) {
  const tr = useTranslation();
  const [open,setOpen]=useState(defaultOpen), [view,setView]=useState(null), [busy,setBusy]=useState(false), [error,setError]=useState("");
  const [target,setTarget]=useState(""), [confirmed,setConfirmed]=useState(false), [continueTask,setContinueTask]=useState(false);
  const pending=useRef(null), current=useRef({request,onChanged}); current.current={request,onChanged};
  const terminalID=useRef(null);
  const path="/sessions/"+id+"/account";
  useEffect(() => {
    let live=true,timer;
    const load=async()=>{
      try {
        const result=await current.current.request(path,{body:{action:"inspect"}});
        if(live) {
          setView(result); setTarget(previous=>result.candidates.some(a=>a.id===previous)?previous:(result.candidates[0]?.id || ""));
          const next=result.session.desktop_terminal_id;
          if(terminalID.current && next && next!==terminalID.current)current.current.onChanged?.(result.session);
          terminalID.current=next;
        }
      } catch(e) { if(live && open) setError(e.message); }
      if(live) timer=setTimeout(load,open?2000:10000);
    };
    void load(); return()=>{live=false;clearTimeout(timer);};
  },[id,path,open]);
  const change=async(body)=>{
    if(busy)return; setBusy(true);setError("");
    try {
      const result=await current.current.request(path,{body}); setView(result);current.current.onChanged?.(result.session);
      if(body.action==="switch") {pending.current=null;setConfirmed(false);}
    } catch(e){setError(e.message);} finally{setBusy(false);}
  };
  if(!view) {
    if(!defaultOpen)return null;
    return <section className="session-account" aria-label={tr("会话账号")}><div className="session-account-options">
      {error?<p role="alert">{tr(error)}</p>:<p>{tr("正在读取会话…")}</p>}
    </div></section>;
  }
  if(!view.supported) {
    if(!defaultOpen)return null;
    return <section className="session-account" aria-label={tr("会话账号")}><div className="session-account-options">
      <p>{tr("此会话不能进行账号接续")}</p>
    </div></section>;
  }
  const session=view.session, sw=session.account_switch;
  const sessionPaused = paused || session.paused;
  const occupied = sessionPaused || session.active;
  const switching=sw && sw.phase!=="completed";
  return <section className={`session-account ${compact?"session-account--compact":""}`} aria-label={tr("会话账号")}>
    {!defaultOpen && <button type="button" className="quiet-button" aria-expanded={open} onClick={()=>setOpen(v=>!v)}>
      {session.account_label || tr("Claude 账号")} · {session.account_failure?tr("需要处理"):switching?tr("正在交接"):view.unopened?tr("用其他账号继续"):tr("会话绑定")}
    </button>}
    {open && <div className="session-account-options">
      {view.unopened
        ?<p>{tr("原账号：")}{session.account_label || session.account_id}</p>
        :<p>{tr("当前账号：")}{session.account_label || session.account_id}</p>}
      {session.account_failure && <p role="status">{failures[session.account_failure] || tr("本轮发生错误，原会话已保留")}</p>}
      {sessionPaused && <p role="status">{tr("Claude 已暂停")}</p>}
      {sw && <p role="status">{sw.phase==="completed"?tr("已接续至"):tr("接续目标：")}{sw.target_label}{sw.error && ` · ${sw.error}`}</p>}
      <label>{tr("此会话切换授权")}<InlineSelect aria-label={tr("此会话切换授权")} disabled={busy || switching} value={view.switch_policy} onValueChange={policy=>change({action:"policy",policy})}>
        <option value="ask">{tr("每次询问")}</option><option value="allow">{tr("额度耗尽后自动接续")}</option><option value="wait">{tr("始终等待原账号")}</option>
      </InlineSelect></label>
      {!switching && <>
        <label>{tr("接续账号")}<InlineSelect aria-label={tr("接续账号")} value={target} disabled={busy || !view.candidates.length} onValueChange={value=>{setTarget(value);setConfirmed(false);pending.current=null;}}>
          {view.candidates.length?view.candidates.map(a=><option key={a.id} value={a.id}>{a.label}</option>):<option value="">{tr("无可用账号")}</option>}
        </InlineSelect></label>
        {!view.candidates.length && null}
        <label className="check-line"><input type="checkbox" checked={confirmed} onChange={e=>setConfirmed(e.target.checked)} />{tr("允许将此会话历史交给所选账号")}</label>
        <label className="check-line"><input type="checkbox" checked={continueTask} onChange={e=>{setContinueTask(e.target.checked);pending.current=null;}} />{tr("恢复后继续未完成的任务")}</label>
        <button type="button" disabled={busy || occupied || !target || !confirmed} onClick={()=>{
          pending.current ||= {action:"switch",target,request_id:crypto.randomUUID(),source_id:view.source_id,confirm:true,continue:continueTask};
          void change(pending.current);
        }}>{busy?tr("正在交接…"):view.unopened?tr("用所选账号继续"):tr("切换并恢复历史")}</button>
      </>}
      {switching && sw.phase!=="starting" && <button disabled={busy || occupied} onClick={()=>change({action:"switch",request_id:sw.id,source_id:view.source_id,target,confirm:true})}>{tr("重试账号交接")}</button>}
      {switching && sw.phase==="preparing" && <button disabled={busy} onClick={()=>change({action:"cancel",request_id:sw.id})}>{tr("取消交接，保留原账号")}</button>}
      {switching && sw.phase==="starting" && <p>{tr("正在等待目标账号连接。请保留当前页面，或从会话列表重新打开。")}</p>}
      {error && <p role="alert">{tr(error)}</p>}
    </div>}
  </section>;
}
