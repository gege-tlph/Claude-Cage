import React from "react";
import { ArrowDown, ArrowRight, ArrowUp, Plus, Route, X } from "lucide-react";
import { useTranslation } from "./interface-language.mjs";
import { addRelay, canMakeRelay, makeRelay, move, remove } from "./proxy-route.mjs";

// ProxyRouteEditor edits a chain (demo v11 「链式代理」): one row per hop from
// the first 前置 to the 出口, the last row. The row being chosen has a blue
// frame and takes the next node picked in the list; ↑↓ trade places; a fresh
// chain offers 「设为前置」 on the old exit, so the old node does not have to
// stay the exit. onChange receives { route, active }.
export function ProxyRouteEditor({ route, active, nameOf, onChange, disabled, addable = true }) {
  const tr = useTranslation();
  const last = route.length - 1;
  return <div className="proxy-route-editor">
    <ol className="proxy-route-hops" aria-label={tr("链式代理")}>
      {route.map((id, index) => {
        const exit = index === last;
        const role = exit ? tr("出口") : tr("前置");
        const name = id ? nameOf(id) : index === active ? tr("在下面选择节点") : tr("待选择");
        return <li key={index} className={`proxy-route-hop${exit ? " is-exit" : ""}${index === active ? " is-active" : ""}`}>
          <button type="button" className="proxy-hop-pick" disabled={disabled} aria-pressed={index === active}
            onClick={() => onChange({ route, active: index })}>
            <span className="proxy-hop-role">{role}</span>
            <span className={`proxy-hop-name${id ? "" : " empty"}`}>{name}</span>
          </button>
          <span className="proxy-hop-tools">
            {exit && canMakeRelay(route) && <button type="button" className="proxy-hop-swap" disabled={disabled}
              onClick={() => onChange(makeRelay(route))}>{tr("设为前置")}</button>}
            <button type="button" className="icon-button" disabled={disabled || index === 0} aria-label={tr("上移")} title={tr("上移")}
              onClick={() => onChange(move(route, index, -1))}><ArrowUp size={14} /></button>
            <button type="button" className="icon-button" disabled={disabled || exit} aria-label={tr("下移")} title={tr("下移")}
              onClick={() => onChange(move(route, index, 1))}><ArrowDown size={14} /></button>
            {!exit && <button type="button" className="icon-button" disabled={disabled} aria-label={tr("移除{0}", role)} title={tr("移除{0}", role)}
              onClick={() => onChange(remove(route, index))}><X size={14} /></button>}
          </span>
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
          {id ? <strong>{nameOf(id)}</strong> : <span>{tr("待选择")}</span>}
        </React.Fragment>)}
        <ArrowRight size={12} aria-hidden="true" />
        <span>{tr("目标网站")}</span>
      </span>
    </div>
  </div>;
}
