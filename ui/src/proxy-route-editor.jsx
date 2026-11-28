import React from "react";
import { ArrowDown, ArrowRight, ArrowUp, Plus, Route, X } from "lucide-react";
import { useTranslation } from "./interface-language.mjs";
import { addRelay, move, remove } from "./proxy-route.mjs";
import { groupRange, isGroup, newGroup, strategyName } from "./relay-group.mjs";
import { RelayGroupPanel } from "./relay-group-panel";

// ProxyRouteEditor edits a chain (demo v12 「链式代理」): one row per hop from
// the first 前置 to the 出口, the last row. The row being chosen has a blue
// frame and takes the next node picked in the list; the arrow icons trade
// places, so the old exit of a fresh chain becomes the relay with its ↑
// (user: arrows only, as icons, never characters). A 前置 can switch between
// 固定 (one node) and 自动 (an auto-switch group, #1063) in its row
// (relay-group-panel.jsx). onChange receives { route, active }.
// sources enables groups; current names each saved group's active node.
export function ProxyRouteEditor({ route, active, nameOf, onChange, disabled, addable = true, sources, current = [] }) {
  const tr = useTranslation();
  const last = route.length - 1;
  const replace = (index, hop) => route.map((current, i) => (i === index ? hop : current));
  return <div className="proxy-route-editor">
    <ol className="proxy-route-hops" aria-label={tr("链式代理")}>
      {route.map((id, index) => {
        const exit = index === last;
        const group = isGroup(id);
        const role = exit ? tr("出口") : tr("前置");
        const name = group ? tr("自动 · {0} · {1}", strategyName(id.relay.strategy, tr), groupRange(id, sources || [], tr))
          : id ? nameOf(id) : index === active ? tr("在下面选择节点") : tr("待选择");
        return <li key={index} className={`proxy-route-hop${exit ? " is-exit" : ""}${index === active ? " is-active" : ""}${group ? " is-group" : ""}`}>
          <button type="button" className="proxy-hop-pick" disabled={disabled} aria-pressed={index === active}
            onClick={() => onChange({ route, active: index })}>
            <span className="proxy-hop-role">{role}</span>
            <span className={`proxy-hop-name${id ? "" : " empty"}`}>{name}</span>
          </button>
          {!exit && sources && <span className="proxy-hop-kind" role="group" aria-label={tr("{0}方式", role)}>
            <button type="button" aria-pressed={!group} disabled={disabled}
              onClick={() => group && onChange({ route: replace(index, ""), active: index })}>{tr("固定")}</button>
            <button type="button" aria-pressed={group} disabled={disabled}
              onClick={() => !group && onChange({ route: replace(index, newGroup(sources, id)), active: active === index ? -1 : active })}>{tr("自动")}</button>
          </span>}
          <span className="proxy-hop-tools">
            <button type="button" className="icon-button" disabled={disabled || index === 0 || (exit && isGroup(route[index - 1]))} aria-label={tr("上移")} title={tr("上移")}
              onClick={() => onChange(move(route, index, -1))}><ArrowUp size={14} /></button>
            <button type="button" className="icon-button" disabled={disabled || exit || (group && index === last - 1)} aria-label={tr("下移")} title={tr("下移")}
              onClick={() => onChange(move(route, index, 1))}><ArrowDown size={14} /></button>
            {!exit && <button type="button" className="icon-button" disabled={disabled} aria-label={tr("移除{0}", role)} title={tr("移除{0}", role)}
              onClick={() => onChange(remove(route, index))}><X size={14} /></button>}
          </span>
          {group && <RelayGroupPanel hop={id} sources={sources || []} current={current[index]} disabled={disabled}
            onChange={(next, pick) => onChange({ route: replace(index, next), active: pick ? index : active === index && next.mode !== "nodes" ? -1 : active })} />}
        </li>;
      })}
    </ol>
    <div className="proxy-route-foot">
      {addable && <button type="button" className="text-button proxy-route-add" disabled={disabled} onClick={() => onChange(addRelay(route))}>
        <Plus size={14} />{tr("添加前置")}
      </button>}
      <span className="proxy-route-path" aria-label={tr("待应用链路")}>
        <Route size={14} aria-hidden="true" />
        <span>Cage lite</span>
        {route.map((id, index) => <React.Fragment key={index}>
          <ArrowRight size={12} aria-hidden="true" />
          {isGroup(id) ? <strong>{tr("自动（{0}）", groupRange(id, sources || [], tr))}</strong>
            : id ? <strong>{nameOf(id)}</strong> : <span>{tr("待选择")}</span>}
        </React.Fragment>)}
        <ArrowRight size={12} aria-hidden="true" />
        <span>{tr("目标网站")}</span>
      </span>
    </div>
  </div>;
}
