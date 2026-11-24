import React, { useEffect, useRef, useState } from "react";
import { Check, ChevronLeft, LoaderCircle, ShieldCheck } from "lucide-react";
import { api, native, nativeAvailable } from "./api";
import { isRemoteClient } from "./client-context.mjs";
import { useAccount } from "./account";
import { GuestSystemPicker, validGuestChoice } from "./guest-system-picker";
import { InitializationStorage } from "./initialization-storage";
import { GuestProfileOptions, guestOptionsForSystem } from "./guest-profile";
import { ProxySettings } from "./proxy-settings";
import { InitializationDNS } from "./initialization-dns";
import { SetupProgress } from "./setup-progress";
import { DiagnosticSettings } from "./diagnostics.jsx";
import { InitializationDetails } from "./initialization-checks";
import { Modal } from "./modal";
import { SafetyMessage } from "./safety-message";
import { needsEnvironmentRestart, needsProxySetup } from "./onboarding-state.mjs";
import { beginInstall, formatSpeed, mirrorNames } from "./debian-mirrors.mjs";
import { networkState } from "./safety-state.mjs";
import { useSSHNavigation } from "./remote-hosts";
import { StartupBrand } from "./startup-brand.jsx";
import "./initialization.css";
import { useTranslation } from "./interface-language.mjs";
import { localizeBackendMessage } from "./backend-message.mjs";

const steps = [["system", "系统与存储"], ["proxy", "网络代理"], ["install", "安装与检查"]];

function InitializationFrame({ children }) {
  const tr = useTranslation();
  return <main className="initialization">
    <StartupBrand label={tr("首次设置")} />
    {children}
  </main>;
}

const setupApi = (path, body, method, headers = {}, options) =>
  api(path, body, method, { "X-Cage-Initiator": "setup", ...headers }, options);

export function InitializationGate({ children }) {
  const tr = useTranslation();
  const [state, setState] = useState(null);
  const [error, setError] = useState("");
  const [attempt, retry] = useState(0);
  const remote = isRemoteClient();
  useEffect(() => {
    // On a high-density screen "balanced display" leaves the guest at 3/4 of
    // the screen's pixels and the remote display blurry; the host turns it
    // off once, before the environment's first start reads it.
    if (remote || !(window.devicePixelRatio > 1)) return;
    api("/display-density", { device_pixel_ratio: window.devicePixelRatio }).catch(() => {});
  }, [remote]);
  useEffect(() => {
    if (remote) return;
    let live = true;
    setError("");
    (async () => {
      const next = await setupApi("/environment/initialization");
      if (next.required && !next.tracked && !next.started) await setupApi("/environment/initialization", { action: "begin" });
      if (live) setState(next);
    })().catch(error => { if (live) setError(error.message); });
    return () => { live = false; };
  }, [remote, attempt]);
  if (remote) return children;
  if (error) return <InitializationFrame><section className="initialization-content"><h1>{tr("继续准备 Cage")}</h1><p role="alert"><SafetyMessage message={error} /></p><button className="primary" onClick={() => retry(value => value + 1)}>{tr("重新连接")}</button><DiagnosticSettings /></section></InitializationFrame>;
  if (!state) return <InitializationFrame><section className="initialization-content"><h1>{tr("正在读取环境状态")}</h1><p role="status"><LoaderCircle size={18} className="spin" />{tr("正在检查已有配置和安装进度…")}</p></section></InitializationFrame>;
  return state.required ? <Initialization initial={state} onComplete={() => setState({ required: false })} /> : children;
}

function Initialization({ initial, onComplete }) {
  const tr = useTranslation();
  const { account, refresh } = useAccount();
  const navigation = useSSHNavigation();
  const [step, setStep] = useState(initial.started || account?.guest_system ? "install" : "system");
  const [status, setStatus] = useState(null);
  const [proxy, setProxy] = useState(null);
  const [choice, setChoice] = useState(undefined);
  const [guestProfile, setGuestProfile] = useState(null);
  const [storage, setStorage] = useState(null);
  const [pending, setPending] = useState(null);
  const [canReselect, setCanReselect] = useState(false);
  const [submitting, setSubmitting] = useState(false);
  const [entering, setEntering] = useState(false);
  const [error, setError] = useState("");
  const [connectionError, setConnectionError] = useState("");
  const [mirrors, setMirrors] = useState(null);
  const [mirrorChoice, setMirrorChoice] = useState("");
  const [measuring, setMeasuring] = useState(false);
  const [details, setDetails] = useState(false);
  const [help, setHelp] = useState(false);
  const [dialog, setDialog] = useState(null);
  const [proxyDirty, setProxyDirty] = useState(false);
  const alive = useRef(true);
  const pollNow = useRef(null);
  const actionPending = useRef(false);
  const restored = useRef(false);
  const job = status?.job || {};
  const busy = submitting || Boolean(job.running);
  const network = networkState(status);
  const needsRestart = needsEnvironmentRestart(status);
  const showReady = entering || Boolean(status?.ready && status.vm_running && !busy && !job.error && !needsRestart && network.mode === "open");
  const confirm = options => new Promise(resolve => setDialog({ ...options, resolve }));
  const settleDialog = value => { dialog?.resolve(value); setDialog(null); };

  useEffect(() => {
    alive.current = true;
    let timer, polling = false;
    async function poll() {
      if (polling || !alive.current) return;
      clearTimeout(timer);
      polling = true;
      try {
        const next = await setupApi("/status");
        if (!alive.current) return;
        setStatus(next);
        const results = await Promise.allSettled([
          setupApi("/proxy"),
          account?.guest_system ? Promise.resolve(null) : setupApi("/environment/guest-systems"),
        ]);
        if (!alive.current) return;
        // A successful status read does not mean the related configuration
        // reads recovered. Keep the notice mounted until the full poll succeeds.
        const failedRead = results.find(result => result.status === "rejected");
        setConnectionError(failedRead ? failedRead.reason.message : "");
        if (results[0].status === "fulfilled") setProxy(results[0].value);
        if (results[1].status === "fulfilled") {
          const install = results[1].value;
          setPending(install?.pending || null);
          setCanReselect(Boolean(install?.reselect));
          if (!restored.current) {
            restored.current = true;
            if (initial.started && !initial.prepared && !account?.guest_system && !install?.pending && !next.job?.running && !next.job?.setup_progress && !next.ready) setStep("system");
          }
        }
        if (next.job?.running && ["start", "guest-install", "safety"].includes(next.job.kind)) setStep("install");
      } catch (error) { if (alive.current) setConnectionError(error.message); }
      finally {
        polling = false;
        if (alive.current) timer = setTimeout(poll, 1200);
      }
    }
    pollNow.current = poll;
    poll();
    if (nativeAvailable()) native("workbenchReady").catch(() => {});
    window.cageWorkbenchAction = async action => { if (action === "quit") await native("quitNow"); };
    return () => { alive.current = false; clearTimeout(timer); pollNow.current = null; delete window.cageWorkbenchAction; };
  }, [account?.guest_system]);

  async function run(operation) {
    if (actionPending.current) return;
    actionPending.current = true;
    setSubmitting(true); setError("");
    try { await operation(); }
    catch (error) { if (alive.current) setError(error.message); }
    finally { actionPending.current = false; if (alive.current) { setSubmitting(false); pollNow.current?.(); } }
  }
  function startOptions() {
    if (account?.guest_system) return {};
    if (pending) return { guest_system: pending.system };
    const preferences = guestProfile ? { guest_profile: guestOptionsForSystem(guestProfile, choice?.guest_system) } : {};
    if (!choice) return job.setup_progress?.system ? { guest_system: job.setup_progress.system } : preferences;
    return { guest_system: choice.guest_system, ...preferences };
  }
  async function install(mirrorChoice) {
    if (needsProxySetup(null, status, proxy)) { setError("请先选择并应用代理节点。"); setStep("proxy"); return; }
    // Before the first install, measure the Debian sources through the proxy
    // once; the saved choice is reused by later updates and maintenance.
    const checkMirrors = !status?.vm_running && !job.setup_progress;
    await run(async () => {
      setMeasuring(checkMirrors && typeof mirrorChoice !== "string");
      let outcome;
      try { outcome = await beginInstall(setupApi, { mirrorChoice: typeof mirrorChoice === "string" ? mirrorChoice : "", checkMirrors, startOptions: startOptions() }); }
      finally { if (alive.current) setMeasuring(false); }
      if (!alive.current) return;
      if (outcome.mirrors) { setMirrors(outcome.mirrors); setMirrorChoice(outcome.mirrors.preference); setStep("install"); return; }
      setMirrors(null);
      const { accepted } = outcome;
      setStep("install");
      setStatus(current => ({ ...current, job: { ...accepted, running: true, kind: accepted.kind || "start", message: accepted.message || tr("正在准备环境…") } }));
    });
  }
  async function stop() {
    if (!await confirm({ title: "停止当前安装？", body: "安装进度、已下载文件和虚拟磁盘会保留。你可以在此界面继续安装。", confirm: "停止安装" })) return;
    await run(async () => { const accepted = await setupApi("/stop", {}); setStatus(current => ({ ...current, job: { ...accepted, running: true } })); });
  }
  async function waitForJob(accepted, kind) {
    if (!accepted.id) throw new Error(tr("操作未返回任务编号，请核对状态后重试。"));
    while (alive.current) {
      const next = await setupApi("/status");
      if (!alive.current) break;
      setStatus(next);
      const current = next.job;
      if (current?.id !== accepted.id || current.kind !== kind) throw new Error(tr("操作状态已变化，请核对状态后重试。"));
      if (current.error || current.canceled) throw new Error(current.error || tr("操作已取消，请核对状态后重试。"));
      if (!current.running) return next;
      await new Promise(resolve => setTimeout(resolve, 300));
    }
    throw new Error(tr("初始化页面已关闭，操作未继续。"));
  }
  // A changed exit or environment configuration only takes effect after a full
  // stop. Confirm first, then start only after the accepted stop has finished.
  async function restart() {
    await run(async () => {
      const running = status.vm_running;
      if (!await confirm({ title: running ? tr("重启环境并检查？") : tr("启动环境并检查？"), body: tr("当前终端会话会结束。检查通过前外网保持关闭。"), confirm: running ? tr("重启并检查") : tr("启动并检查") })) return;
      const current = await setupApi("/status");
      if (!alive.current) return;
      if (current.job?.running) throw new Error(tr("其他操作仍在进行，请等待完成后重试。"));
      if (current.vm_running) {
        const stopped = await waitForJob(await setupApi("/stop", {}), "stop");
        if (stopped.vm_running) throw new Error(tr("环境尚未停止，请核对状态后重试。"));
      }
      if (!alive.current) return;
      const accepted = await setupApi("/start", startOptions());
      if (!accepted.id) throw new Error(tr("启动请求未返回任务编号，请核对状态后重试。"));
      setStatus(current => ({ ...current, job: { ...accepted, running: true, kind: "start", message: accepted.message || tr("正在启动并检查环境…") } }));
    });
  }
  async function enter() {
    await run(async () => {
      setEntering(true);
      try {
        await setupApi("/environment/initialization", { action: "complete" });
        await refresh();
        if (alive.current) onComplete();
      } finally {
        if (alive.current) setEntering(false);
      }
    });
  }
  async function checkNetwork() {
    await run(async () => {
      const accepted = await setupApi("/safety/check", {});
      setStatus(current => ({ ...current, job: { ...accepted, running: true, kind: "safety", message: tr("正在检查代理出口和环境隔离…") } }));
    });
  }
  async function back() {
    if (step === "proxy" && proxyDirty && !await confirm({ title: "放弃尚未应用的代理修改？", body: "已经保存并应用的代理会保留，当前未应用的表单修改会丢弃。", confirm: "放弃修改" })) return;
    setProxyDirty(false);
    setStep(step === "install" ? "proxy" : pending && !canReselect || account?.guest_system ? "install" : "system");
  }
  async function nextFromSystem() {
    await run(async () => {
      const directory = storage.directory.trim();
      if (directory !== storage.location.directory) {
        const accepted = await setupApi("/environment/storage", { directory, previous: storage.location.directory, migrate: false });
        let job;
        do {
          await new Promise(resolve => setTimeout(resolve, 300));
          if (!alive.current) return;
          const next = await setupApi("/status");
          setStatus(next); job = next.job;
          if (job.id !== accepted.id) throw new Error("存储操作状态已变化，请核对目录后重试。");
        } while (job.running);
        if (job.error) throw new Error(job.error);
        const location = await setupApi("/environment/storage");
        if (location.directory !== directory) throw new Error("存储目录尚未保存，请重新选择后重试。");
        setStorage({ location, directory: location.directory });
      }
      setStep("proxy");
    });
  }

  const failed = Boolean(job.error || error);
  const stopped = !busy && !status?.ready && (job.kind === "stop" || job.canceled);
  const networkPending = Boolean(status?.ready && status.vm_running && network.mode !== "open");
  const progress = !status?.ready && job.setup_progress ? { ...job, setup_progress: job.setup_progress } : null;
  const index = steps.findIndex(([key]) => key === step);
  const title = steps[index][1];
  // Only the install's own jobs describe this step; another job's message
  // (such as a saved DNS setting's) is not install state (S1).
  const installMessage = ["start", "guest-install", "safety", "stop"].includes(job.kind) ? localizeBackendMessage(job.message, tr) : "";
  const resumable = Boolean(initial.started || pending || job.setup_progress);
  const checkPending = Boolean(status?.ready && network.mode !== "open" && !busy && !needsRestart);
  return <InitializationFrame>
    <section className="initialization-content">
      <ol className="initialization-steps" aria-label={tr("首次设置步骤")}>{steps.map(([key, label], position) => <li key={key} aria-current={step === key ? "step" : undefined}><span>{position < index ? <Check size={14} /> : position + 1}</span>{tr(label)}</li>)}</ol>
      <div className="initialization-title"><h1>{tr(title)}</h1></div>
      {connectionError && <div className="initialization-notice" role="alert"><SafetyMessage message={connectionError} /><button onClick={() => pollNow.current?.()}>{tr("重新连接")}</button></div>}
      {!status ? <p role="status"><LoaderCircle size={18} className="spin" />{tr("正在读取安装状态…")}</p> : <>
        <div className="initialization-stage" key={step}>
          {step === "system" && <>
            <GuestSystemPicker system={choice} onChange={setChoice} onReselect={() => { setPending(null); setCanReselect(false); }} preserveOnUnmount />
            {pending ? null : <>
              <InitializationStorage value={storage} onChange={setStorage} disabled={busy} />
              <GuestProfileOptions value={guestProfile} onChange={setGuestProfile} disabled={busy} system={choice?.guest_system} />
            </>}
          </>}
          {step === "proxy" && <><ProxySettings initializing status={status} onConfirm={confirm} onDirty={setProxyDirty} onApplied={async () => { setProxy(await api("/proxy")); pollNow.current?.(); }} /><InitializationDNS status={status} disabled={busy} open={Boolean(job.error)} /></>}
          {step === "install" && mirrors && <section className="initialization-notice" aria-label={tr("Debian 软件源")}><h2>{tr("Debian 软件源")}</h2>
            <table className="mirror-samples"><tbody>{mirrors.samples.map(sample => <tr key={sample.id}><td>{tr(sample.name)}</td><td><code>{sample.host}</code></td><td>{sample.error ? <span className="mirror-error">{tr(sample.error)}</span> : formatSpeed(sample.bytes_per_second)}</td></tr>)}</tbody></table>
            <label className="mirror-option"><input type="radio" name="debian-mirror" value="auto" checked={mirrorChoice === "auto"} onChange={() => setMirrorChoice("auto")} />{tr("官方源优先")}{mirrors.recommended === "auto" && <em>{tr("推荐")}</em>}</label>
            <label className="mirror-option"><input type="radio" name="debian-mirror" value={mirrors.domestic} checked={mirrorChoice === mirrors.domestic} onChange={() => setMirrorChoice(mirrors.domestic)} />{tr("{0}优先", tr(mirrorNames[mirrors.domestic] ?? mirrors.domestic))}{mirrors.recommended === mirrors.domestic && <em>{tr("推荐")}</em>}</label>
            <button className="primary" disabled={busy || !mirrorChoice || Boolean(connectionError)} onClick={() => install(mirrorChoice)}>{submitting && <LoaderCircle size={16} className="spin" />}{tr("开始安装")}</button></section>}
          {step === "install" && <>
            {showReady ? <div className="initialization-ready"><ShieldCheck size={36} /><h2 role={entering ? "status" : undefined}>{tr(entering ? "正在进入工作台" : "环境已就绪")}</h2></div> : <>
            {needsRestart && <section className="initialization-notice" aria-label={tr("需要重启环境")}><h2>{tr("需要重启环境")}</h2><button className="primary" disabled={busy || Boolean(connectionError)} onClick={restart}>{status.vm_running ? tr("重启环境并检查") : tr("启动环境并检查")}</button></section>}
            {progress ? <SetupProgress job={progress} onDetails={() => setDetails(true)} onStop={stop} onRetry={needsRestart ? restart : install} disconnected={Boolean(connectionError) || submitting} /> : checkPending
              // Installed and connected: the network check is this card's state (no second block).
              ? <div className="initialization-operation" role="status"><p>{status.safety?.error || job.error || status.network_error || tr("网络与隔离未检查")}</p><button className="primary" disabled={Boolean(connectionError)} onClick={checkNetwork}>{tr("网络与隔离检查")}</button></div>
              : mirrors ? null : <div className="initialization-operation" role="status"><p>{busy && <LoaderCircle size={18} className="spin" />}{measuring ? tr("正在测量软件源速度…") : stopped ? tr("安装已停止") : installMessage || (status.vm_running ? tr("环境已启动") : resumable ? tr("安装未完成") : tr("未开始"))}</p>{!busy && !job.error && !mirrors && !needsRestart && <button className="primary" disabled={Boolean(connectionError)} onClick={install}>{status.ready ? tr("重新启动并检查") : resumable ? tr("继续安装") : tr("开始安装")}</button>}{busy && !measuring && <button onClick={stop} disabled={submitting}>{tr("停止安装")}</button>}</div>}
            {job.error && !progress && !checkPending && <div className="initialization-notice" role="alert"><SafetyMessage message={job.error} />{!networkPending && !needsRestart && <button className="primary" disabled={busy || Boolean(connectionError)} onClick={install}>{tr("继续安装")}</button>}</div>}
            </>}
            <div className="initialization-recovery">{canReselect && <button disabled={busy} onClick={() => setStep("system")}>{tr("查看或撤销原安装选择")}</button>}<button onClick={() => setStep("proxy")} disabled={busy}>{tr("网络代理")}</button>{!progress && <button onClick={() => setDetails(true)}>{tr("操作详情")}</button>}</div>
          </>}
        </div>
        {error && <p className="initialization-notice" role="alert"><SafetyMessage message={error} /></p>}
        <footer className="initialization-actions">
          {step === "system" ? <span /> : <button className="text-button" disabled={busy} onClick={back}><ChevronLeft size={16} />{tr("上一步")}</button>}
          {step === "system" && <button className="primary" disabled={busy || !storage?.directory.trim() || choice === undefined || !validGuestChoice(choice) || Boolean(connectionError)} onClick={nextFromSystem}>{submitting && <LoaderCircle size={16} className="spin" />}{tr("下一步：配置代理")}</button>}
          {step === "proxy" && <button className="primary" disabled={busy || proxyDirty || !account?.guest_system && !pending && !validGuestChoice(choice) || needsProxySetup(null, status, proxy) || Boolean(connectionError)} onClick={install}>{submitting ? <LoaderCircle size={16} className="spin" /> : null}{tr("下一步：安装")}</button>}
          {step === "install" && showReady && <button className="primary" disabled={busy || Boolean(connectionError)} aria-busy={entering} onClick={enter}>{entering && <LoaderCircle size={16} className="spin" />}{tr(entering ? "正在进入…" : "进入工作台")}</button>}
        </footer>
      </>}
      {(status || connectionError) && <div className="initialization-support"><button className="text-button" aria-expanded={help} onClick={() => setHelp(!help)}>{tr("日志与诊断")}</button>{navigation && !busy && <button className="text-button" onClick={() => navigation.open()}>{tr("只连接 SSH 主机")}</button>}</div>}
      {help && <DiagnosticSettings />}
    </section>
    {details && <Modal title={tr("操作详情")} onClose={() => setDetails(false)}><InitializationDetails status={status} /></Modal>}
    {dialog && <Modal title={dialog.title} onClose={() => settleDialog(false)} footer={<><button onClick={() => settleDialog(false)}>{tr("取消")}</button><button className="primary" onClick={() => settleDialog(true)}>{tr(dialog.confirm)}</button></>}><div className="modal-body"><p>{tr(dialog.body)}</p></div></Modal>}
  </InitializationFrame>;
}
