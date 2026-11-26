import React, { useEffect, useState } from "react";
import { LoaderCircle } from "lucide-react";
import { api } from "./api";
import { useAccount } from "./account";
import { FerryMark, FerrySetup, RELAY_SOURCE } from "./ferry-setup.jsx";
import { exitRegion, pickRelay } from "./relay-pick.mjs";
import { useTranslation } from "./interface-language.mjs";
import { localizeBackendMessage } from "./backend-message.mjs";

const sleep = (ms) => new Promise((resolve) => setTimeout(resolve, ms));

async function proxyJob(action, input, tr) {
  const job = await api(`/proxy/${action}`, input);
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
}

// 「从 Ferry 导入」 in the proxy settings: the Ferry steps for an account that
// exists, to finish a first run that stopped part way, or to take an exit
// Ferry changed. The relay subscription imported before is used again.
// The card also says whether the route ends at a Ferry exit: when it does not
// (the user changed it), 恢复 builds the Ferry chain again from the nodes
// already imported; when the relay is not in the exit's region, 重新选择前置.
export function FerryImport({ proxy, busy, onChanged }) {
  const tr = useTranslation();
  const { account } = useAccount();
  const [open, setOpen] = useState(false);
  const [known, setKnown] = useState(null);
  const [working, setWorking] = useState(false);
  const [error, setError] = useState("");
  useEffect(() => {
    let live = true;
    api("/ferry").then((value) => { if (live) setKnown(value); }, () => { if (live) setKnown({}); });
    return () => { live = false; };
  }, [open]);
  const sources = proxy?.sources || [];
  const relaySource = sources.find((source) => source.name === RELAY_SOURCE && source.remote) || null;
  const ferrySource = sources.find((source) => source.id && source.id === known?.source) || sources.find((source) => source.name === "Ferry" && source.remote) || null;
  const exits = known?.exits || [];
  const route = proxy?.route || [];
  const exitNode = ferrySource?.nodes.find((node) => node.id === proxy?.selected) || null;
  const regionOf = (node) => exitRegion(exits.find((exit) => exit.name === node.name) || { name: node.name });
  const relayNode = route.length > 1 ? relaySource?.nodes.find((node) => node.id === route[route.length - 2]) : null;
  const sameRegion = !exitNode || !regionOf(exitNode) || (relayNode && exitRegion({ name: relayNode.name }) === regionOf(exitNode));
  const canRestore = Boolean(relaySource && ferrySource && (!exitNode || !sameRegion));

  const restore = async () => {
    if (working) return;
    setWorking(true);
    setError("");
    try {
      const ids = relaySource.nodes.filter((node) => !node.blocked_reason).map((node) => node.id).slice(0, 300);
      if (!ids.length) throw new Error(tr("前置订阅没有可用节点"));
      // Measuring needs the core: apply a relay node first when it is stopped.
      if (!proxy.running) await proxyJob("select", { node_id: ids[0] }, tr);
      const measured = await api("/proxy/delay", { node_ids: ids });
      let exit = exitNode;
      if (!exit) {
        const usable = ferrySource.nodes.filter((node) => !node.blocked_reason);
        for (const candidate of exits) {
          exit = usable.find((node) => node.name === candidate.name);
          if (exit) break;
        }
        exit ||= usable[0];
      }
      if (!exit) throw new Error(tr("Ferry 账号没有可用出口"));
      const pick = pickRelay({ region: regionOf(exit), candidates: relaySource.nodes, results: measured.results || [] });
      if (!pick) throw new Error(tr("前置订阅没有可用节点"));
      await proxyJob("route", { node_ids: [pick.node.id, exit.id] }, tr);
      await onChanged?.();
    } catch (e) {
      setError(e.message);
    } finally {
      setWorking(false);
    }
  };

  if (open && known) {
    return (
      <FerrySetup
        inline
        account={account}
        known={known}
        relaySource={relaySource}
        onBack={() => setOpen(false)}
        onDone={() => { setOpen(false); onChanged?.(); }}
      />
    );
  }
  const state = [known?.user, exitNode ? `${tr("Ferry 出口")} · ${exitNode.name}` : ferrySource ? tr("未使用 Ferry 出口") : ""].filter(Boolean).join(" · ");
  return (
    <section className="ferry-import" aria-label={tr("从 Ferry 导入")}>
      <div className="ferry-import-heading">
        <h4><FerryMark size={20} />{tr("从 Ferry 导入")}</h4>
        <div className="ferry-import-tools">
          {canRestore && (
            <button type="button" disabled={busy || working} onClick={restore}>
              {working && <LoaderCircle size={14} className="spin" />}{tr(exitNode ? "重新选择前置" : "恢复")}
            </button>
          )}
          <button type="button" disabled={busy || working || !known} onClick={() => setOpen(true)}>{tr("登录 Ferry")}</button>
        </div>
      </div>
      {state && (
        <p className="ferry-import-state">
          {state}
          {exitNode && !sameRegion && <span className="ferry-tag">{tr("无同地区前置")}</span>}
        </p>
      )}
      {error && <p className="account-error ferry-import-error" role="alert">{localizeBackendMessage(error, tr)}</p>}
    </section>
  );
}
