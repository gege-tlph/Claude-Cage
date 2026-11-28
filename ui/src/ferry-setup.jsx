import React, { useRef, useState } from "react";
import { Check, ChevronLeft, CircleAlert, LoaderCircle, Settings, X } from "lucide-react";
import { api } from "./api";
import { CageLogo } from "./cage-logo.jsx";
import { useTranslation } from "./interface-language.mjs";
import { localizeBackendMessage } from "./backend-message.mjs";
import { exitRegion, pickRelay } from "./relay-pick.mjs";
import { regionLabel } from "./proxy-region.mjs";
import { FERRY_SERVER } from "./ferry-preset.mjs";
import "./ferry-setup.css";

// The Ferry mark, as the Ferry console draws it: a hull on the water and a sail.
export function FerryMark({ size = 64, className = "" }) {
  return (
    <svg className={`ferry-mark ${className}`} width={size} height={size} viewBox="0 0 48 48" aria-hidden="true">
      <path d="M24 7v23" fill="none" stroke="#2f8cf6" strokeWidth="5" strokeLinecap="round" />
      <path d="M28.5 10.5 38 27.5H28.5z" fill="#1fd27a" stroke="#1fd27a" strokeWidth="3" strokeLinejoin="round" />
      <path d="M8 33.5h32l-4.5 7.5h-23z" fill="#2f8cf6" stroke="#2f8cf6" strokeWidth="3" strokeLinejoin="round" />
    </svg>
  );
}

// The first screen of a new profile: set up by hand, or from the
// organisation's Ferry panel (its exits, behind the employee's own relay).
export function SetupChooser({ onCage, onFerry }) {
  const tr = useTranslation();
  return (
    <section className="setup-chooser" aria-label={tr("首次设置")}>
      <button type="button" className="setup-choice" onClick={onCage}>
        <CageLogo className="setup-choice-logo" />
        <span>{tr("从 Cage 配置")}</span>
      </button>
      <button type="button" className="setup-choice" onClick={onFerry}>
        <FerryMark size={64} />
        <span>{tr("从 Ferry 配置")}</span>
      </button>
    </section>
  );
}

// The back button of every first-run card, in its top left corner.
export function SetupBack({ onClick, disabled }) {
  const tr = useTranslation();
  return (
    <button type="button" className="setup-back" disabled={disabled} onClick={onClick}>
      <ChevronLeft size={16} />{tr("返回")}
    </button>
  );
}

const STEPS = [
  ["account", "创建账户"],
  ["relay", "导入前置订阅"],
  ["connect", "连接前置"],
  ["ferry", "登录 Ferry"],
  ["exit", "导入出口"],
  ["chain", "组成代理链"],
];
export const RELAY_SOURCE = "前置订阅";
const FERRY_SOURCE = "Ferry";
const sleep = (ms) => new Promise((resolve) => setTimeout(resolve, ms));
const httpsAddress = (value) => {
  try { const u = new URL(value.trim()); return u.protocol === "https:" && Boolean(u.hostname); } catch { return false; }
};
const newSource = (before, after) => after.sources.find((s) => !before.sources.some((b) => b.id === s.id)) || null;

// Ferry first run, in pages: the employee's own subscription (the relay),
// then the Ferry user name and password, then the steps. The account is
// created with the Ferry user name and password, the subscription is imported
// and its fastest node applied so the network works, then Ferry is reached
// through it: its exits are imported, and the relay becomes the fastest node
// in the exit's region. Finished steps are kept, so a retry continues where it
// stopped. The Ferry address is preset; the gear shows it.
//
// With an account already there (inline, the 「从 Ferry 导入」 card of the proxy
// settings), the same steps run without creating the account: a relay
// subscription imported before is used again unless replaced, and the Ferry
// source is updated in place.
export function FerrySetup({ account, act, rememberLogin, onBack, onManual, onDone, inline = false, known = {}, relaySource = null }) {
  const tr = useTranslation();
  const [page, setPage] = useState(inline && relaySource ? "login" : "relay");
  const [form, setForm] = useState({ server: known.server || FERRY_SERVER, username: inline ? known.user || account?.username || "" : "", password: "", relay: "" });
  const [showServer, setShowServer] = useState(!httpsAddress(known.server || FERRY_SERVER));
  const [remember, setRemember] = useState(true);
  const [states, setStates] = useState({});
  const [running, setRunning] = useState(false);
  const [error, setError] = useState("");
  const [loginFailed, setLoginFailed] = useState(false);
  const [summary, setSummary] = useState(null);
  const run = useRef({ done: new Set(), password: "", username: "" });
  const accountMade = run.current.done.has("account");
  const field = (key) => ({ value: form[key], onChange: (event) => setForm({ ...form, [key]: event.target.value }) });
  const serverValid = httpsAddress(form.server);
  // Inline, an empty field keeps the relay subscription imported before.
  const relayValid = httpsAddress(form.relay) || inline && Boolean(relaySource) && !form.relay.trim();
  const stepList = inline ? STEPS.filter(([key]) => key !== "account") : STEPS;
  const loginValid = form.username.trim() && [...form.username.trim()].length <= 40 && [...form.password].length >= 8;

  const mark = (key, state) => setStates((current) => ({ ...current, [key]: state }));
  const waitJob = async (job) => {
    if (!job?.id) throw new Error(tr("操作未返回任务编号，请核对状态后重试。"));
    for (;;) {
      await sleep(500);
      const current = (await api("/status")).job;
      if (current?.id !== job.id) throw new Error(tr("操作已被其他任务替代，请查看当前状态"));
      if (!current.running) {
        if (current.error) throw new Error(current.error);
        return current;
      }
    }
  };
  const proxyJob = async (action, input) => waitJob(await api(`/proxy/${action}`, input));

  const steps = {
    account: async (ctx) => {
      if (account?.exists) return;
      await act("create", { username: form.username.trim(), password: form.password, confirm_password: form.password });
      ctx.username = form.username.trim();
      ctx.password = form.password;
      await rememberLogin(remember, form.password);
    },
    relay: async (ctx) => {
      const url = form.relay.trim();
      if (inline && relaySource) {
        // Replace the link of the relay imported before, keeping its place.
        if (url) {
          await proxyJob("edit", { id: relaySource.id, name: relaySource.name, url, interval_hours: relaySource.interval_hours || 24 });
          await proxyJob("refresh", { id: relaySource.id, host_download: true });
        }
        ctx.relaySource = (await api("/proxy")).sources.find((s) => s.id === relaySource.id) || null;
      } else {
        const before = await api("/proxy");
        await proxyJob("import", { name: RELAY_SOURCE, url, interval_hours: 24, host_download: true });
        ctx.relaySource = newSource(before, await api("/proxy"));
      }
      if (!ctx.relaySource?.nodes.length) throw new Error(tr("前置订阅没有可用节点"));
    },
    connect: async (ctx) => {
      const ids = ctx.relaySource.nodes.filter((n) => !n.blocked_reason).map((n) => n.id).slice(0, 300);
      if (!ids.length) throw new Error(tr("前置订阅没有可用节点"));
      // The core runs once a route is applied: apply one node, then measure them all.
      await proxyJob("select", { node_id: ids[0] });
      const measured = await api("/proxy/delay", { node_ids: ids });
      ctx.relayResults = measured.results || [];
      if (!measured.best) throw new Error(tr("前置订阅没有可用节点"));
      if (measured.best !== ids[0]) await proxyJob("select", { node_id: measured.best });
    },
    ferry: async (ctx) => {
      // A corrected user name or password becomes the account's too.
      const username = form.username.trim();
      if (ctx.username && username !== ctx.username) {
        await act("profile", { username, avatar: account?.avatar || "" });
        ctx.username = username;
      }
      if (ctx.password && form.password !== ctx.password) {
        await act("password", { old_password: ctx.password, password: form.password, confirm_password: form.password });
        ctx.password = form.password;
        await rememberLogin(remember, form.password);
      }
      const before = await api("/proxy");
      let answer;
      try {
        answer = await api("/ferry/login", { server: form.server.trim(), username, password: form.password });
      } catch (e) {
        setLoginFailed(e.status === 422);
        throw e;
      }
      setLoginFailed(false);
      ctx.exits = answer.exits || [];
      ctx.ferryJob = answer.job;
      ctx.ferryBefore = before;
    },
    exit: async (ctx) => {
      try {
        await waitJob(ctx.ferryJob);
      } catch (e) {
        // The failed job cannot be waited for again: retry signs in anew.
        ctx.done.delete("ferry");
        throw e;
      }
      // A new source, or the one imported before and now updated in place.
      const after = await api("/proxy");
      ctx.ferrySource = newSource(ctx.ferryBefore, after) || after.sources.find((s) => s.name === FERRY_SOURCE && s.remote) || null;
      if (!ctx.ferrySource?.nodes.length) throw new Error(tr("Ferry 账号没有可用出口"));
    },
    chain: async (ctx) => {
      const nodes = ctx.ferrySource.nodes.filter((n) => !n.blocked_reason);
      let exit = null, info = null;
      for (const candidate of ctx.exits) {
        exit = nodes.find((n) => n.name === candidate.name);
        if (exit) { info = candidate; break; }
      }
      exit ||= nodes[0];
      if (!exit) throw new Error(tr("Ferry 账号没有可用出口"));
      const region = exitRegion(info || { name: exit.name });
      const pick = pickRelay({ region, candidates: ctx.relaySource.nodes, results: ctx.relayResults });
      if (!pick) throw new Error(tr("前置订阅没有可用节点"));
      await proxyJob("route", { node_ids: [pick.node.id, exit.id] });
      setSummary({ relay: pick.node.name, delay: pick.delay, sameRegion: pick.sameRegion, exit: exit.name, region });
    },
  };

  const start = async () => {
    if (running || !serverValid || !relayValid || !loginValid) return;
    setPage("run");
    setRunning(true);
    setError("");
    const ctx = run.current;
    // A retry starts again from the first step not done.
    setStates(Object.fromEntries(stepList.filter(([key]) => ctx.done.has(key)).map(([key]) => [key, "done"])));
    try {
      for (const [key] of stepList) {
        if (ctx.done.has(key)) continue;
        mark(key, "running");
        try { await steps[key](ctx); }
        catch (e) { mark(key, "failed"); throw e; }
        ctx.done.add(key);
        mark(key, "done");
      }
    } catch (e) {
      setError(e.message);
    } finally {
      setRunning(false);
    }
  };

  // The first run shows each page as a card with the Ferry mark; inline, the
  // pages fill the card of the proxy settings.
  const frame = ({ label, title, back, gear, children }) => inline ? (
    <section className="ferry-import ferry-setup" aria-label={tr(label)}>
      <div className="ferry-import-heading">
        <h4>{tr(title)}</h4>
        <div className="ferry-import-tools">
          {gear}
          {back && <button type="button" className="icon-button" aria-label={tr(back.close ? "关闭" : "返回")} title={tr(back.close ? "关闭" : "返回")} disabled={running} onClick={back.onClick}>{back.close ? <X size={16} /> : <ChevronLeft size={16} />}</button>}
        </div>
      </div>
      {children}
    </section>
  ) : (
    <section className="account-entry ferry-setup" aria-label={tr(label)}>
      {back && <SetupBack onClick={back.onClick} disabled={running} />}
      {gear}
      <FerryMark size={58} className="ferry-card-mark" />
      <h1>{tr(title)}</h1>
      {children}
    </section>
  );

  if (summary) {
    return frame({
      label: inline ? "从 Ferry 导入" : "从 Ferry 配置",
      title: "代理链已就绪",
      children: <>
        <dl className="ferry-summary">
          <dt>{tr("前置")}</dt>
          <dd>{summary.relay}<span className="ferry-muted"> · {tr("{0} ms", summary.delay)}</span>{!summary.sameRegion && <span className="ferry-tag">{tr("无同地区前置")}</span>}</dd>
          <dt>{tr("出口")}</dt>
          <dd>{summary.exit}{summary.region && <span className="ferry-muted"> · {regionLabel(summary.region)}</span>}</dd>
        </dl>
        <button className="primary account-submit" onClick={() => onDone()}>{tr(inline ? "完成" : "继续")}</button>
      </>,
    });
  }

  // The address can change until Ferry has answered.
  const serverEditable = !running && !run.current.done.has("ferry");
  if (showServer && serverEditable) return frame({
    label: "Ferry 地址",
    title: "Ferry 地址",
    back: serverValid ? { onClick: () => setShowServer(false) } : inline ? { close: true, onClick: onBack } : null,
    children: (
      <form onSubmit={(event) => { event.preventDefault(); if (serverValid) setShowServer(false); }}>
        <fieldset>
          <label className="account-field">{tr("Ferry 地址")}
            <input autoFocus type="url" inputMode="url" autoComplete="url" spellCheck={false} placeholder="https://" aria-invalid={!serverValid} {...field("server")} />
          </label>
          <button className="primary account-submit" disabled={!serverValid}>{tr("完成")}</button>
          {form.server !== FERRY_SERVER && <button type="button" className="text-button ferry-link" onClick={() => setForm({ ...form, server: FERRY_SERVER })}>{tr("恢复默认地址")}</button>}
        </fieldset>
      </form>
    ),
  });

  // Back: the relay page leaves (the first screen, or closes the card); the
  // login page returns to the relay page unless the relay is kept from before.
  let back = null;
  if (page === "relay") back = inline && relaySource ? { onClick: () => setPage("login") } : onBack && { close: inline, onClick: onBack };
  else if (page === "login") back = inline && relaySource ? { close: true, onClick: onBack } : { onClick: () => setPage("relay") };
  else if (inline && !running) back = { close: true, onClick: onBack };
  if (!inline && accountMade) back = null;
  const gear = serverEditable && (
    <button type="button" className={inline ? "icon-button" : "setup-gear"} aria-label={tr("Ferry 地址")} title={tr("Ferry 地址")} onClick={() => setShowServer(true)}>
      <Settings size={inline ? 16 : 17} />
    </button>
  );
  const credentials = <>
    <label className="account-field">{tr("用户名")}
      <input autoFocus={!inline || !form.username} autoComplete="username" maxLength={40} required {...field("username")} />
    </label>
    <label className="account-field">{tr("密码")}
      <input type="password" autoFocus={inline && Boolean(form.username)} autoComplete="current-password" required {...field("password")} />
    </label>
  </>;
  const relayHost = () => { try { return new URL(form.relay.trim()).hostname; } catch { return ""; } };
  return frame({
    label: inline ? "从 Ferry 导入" : "从 Ferry 配置",
    title: inline ? "从 Ferry 导入" : "从 Ferry 配置",
    back,
    gear,
    children: <>
      {page === "relay" && (
        <form onSubmit={(event) => { event.preventDefault(); if (relayValid && serverValid) setPage("login"); }}>
          <fieldset>
            <label className="account-field">{tr("前置订阅")}
              <input autoFocus type="url" inputMode="url" spellCheck={false} placeholder={inline && relaySource?.host ? relaySource.host : "https://"} {...field("relay")} />
            </label>
            <button className="primary account-submit" disabled={!relayValid || !serverValid}>{tr("下一步")}</button>
          </fieldset>
        </form>
      )}
      {page === "login" && (
        <form onSubmit={(event) => { event.preventDefault(); start(); }}>
          <fieldset>
            {inline && relaySource && (
              <p className="ferry-relay-line">
                <span className="ferry-muted">{tr("前置订阅")}</span>
                <span>{relayHost() || relaySource.host || relaySource.name}</span>
                <button type="button" className="text-button" onClick={() => setPage("relay")}>{tr("更换")}</button>
              </p>
            )}
            {credentials}
            {!inline && (
              <label className="account-device-option check-line">
                <input type="checkbox" checked={remember} onChange={(e) => setRemember(e.target.checked)} />
                <span><strong>{tr("记住我")}</strong></span>
              </label>
            )}
            <button className="primary account-submit" disabled={!loginValid || !serverValid}>{tr("开始")}</button>
          </fieldset>
        </form>
      )}
      {page === "run" && (
        <form onSubmit={(event) => { event.preventDefault(); start(); }}>
          <fieldset disabled={running}>
            {loginFailed && !running && credentials}
            <ol className="ferry-steps" aria-label={tr("配置步骤")}>
              {stepList.map(([key, label]) => {
                const state = states[key] || "waiting";
                return (
                  <li key={key} data-state={state}>
                    <span className="ferry-step-mark">
                      {state === "done" ? <Check size={14} /> : state === "running" ? <LoaderCircle size={14} className="spin" /> : state === "failed" ? <CircleAlert size={14} /> : null}
                    </span>
                    <span>{tr(label)}</span>
                  </li>
                );
              })}
            </ol>
            {error && <p className="account-error" role="alert">{localizeBackendMessage(error, tr)}</p>}
            {!running && error && <button className="primary account-submit" disabled={!loginValid || !serverValid}>{tr("重试")}</button>}
            {running && <button className="primary account-submit" disabled>{tr("正在配置…")}</button>}
            {!running && error && (inline || !accountMade) && <button type="button" className="text-button ferry-link" onClick={() => { setError(""); setPage("login"); }}>{tr("修改")}</button>}
            {!running && error && !inline && accountMade && <button type="button" className="text-button ferry-link" onClick={onManual}>{tr("手动配置")}</button>}
          </fieldset>
        </form>
      )}
    </>,
  });
}
