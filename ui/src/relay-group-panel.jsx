import React, { useId } from "react";
import { InlineSelect } from "./inline-select";
import { useTranslation } from "./interface-language.mjs";
import { STRATEGIES, problemName, strategyName, withMode, withSource, withStrategy } from "./relay-group.mjs";
import "./relay-group.css";

// RelayGroupPanel is the range and strategy of an auto-switch 前置, under its
// row in the chain editor. Choosing 自选节点 makes the row the one being
// chosen: the node list below then ticks the nodes of the group.
// problem is why the backend would refuse it (groupProblem), shown as its
// state. onChange(next, pick) receives the group and whether to start picking.
export function RelayGroupPanel({ hop, sources, current, problem, disabled, onChange }) {
  const tr = useTranslation();
  const name = useId();
  const trouble = problemName(problem, hop, tr);
  return <div className="proxy-group-panel">
    <div className="proxy-group-line" role="radiogroup" aria-label={tr("范围")}>
      <span className="proxy-group-label">{tr("范围")}</span>
      <label className="check-line proxy-group-choice">
        <input type="radio" name={name} checked={hop.mode === "source"} disabled={disabled}
          onChange={() => onChange(withMode(hop, "source"), false)} />
        {tr("整份订阅")}
      </label>
      {hop.mode === "source" && <InlineSelect aria-label={tr("中转订阅")} value={hop.relay.source_id} disabled={disabled}
        onValueChange={(next) => onChange(withSource(hop, next), false)}>
        {sources.map((s) => <option key={s.id} value={s.id}>{s.name}</option>)}
      </InlineSelect>}
      <label className="check-line proxy-group-choice">
        <input type="radio" name={name} checked={hop.mode === "nodes"} disabled={disabled}
          onChange={() => onChange(withMode(hop, "nodes"), true)} />
        {tr("自选节点")}
      </label>
      {hop.mode === "nodes" && <span className="proxy-group-count">{tr("已选 {0} 个", hop.relay.node_ids.length)}</span>}
      {trouble && <span className="proxy-group-problem">{trouble}</span>}
    </div>
    <div className="proxy-group-line">
      <span className="proxy-group-label">{tr("策略")}</span>
      <span className="proxy-route-mode" role="group" aria-label={tr("策略")}>
        {STRATEGIES.map((strategy) => <button type="button" key={strategy} aria-pressed={hop.relay.strategy === strategy} disabled={disabled}
          onClick={() => onChange(withStrategy(hop, strategy), false)}>{strategyName(strategy, tr)}</button>)}
      </span>
      {current && <span className="proxy-group-current">{tr("当前 {0}", current)}</span>}
    </div>
  </div>;
}
