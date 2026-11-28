// Relay auto-switch groups (#1063) in our chain editor (user, 2026-11-28:
// set inside the 前置 row). A route entry is a node ID ("" while still to
// choose) or a group { relay: { strategy, source_id, node_ids }, mode }, where
// mode is which range the row shows: "source" (整份订阅) or "nodes" (自选节点).
// Only 前置 hops can be groups; the exit stays one fixed node.

import { MAX_RELAY_NODES } from "./proxy-route.mjs";

export const STRATEGIES = ["fallback", "url-test"];

export const isGroup = (hop) => Boolean(hop && typeof hop === "object" && hop.relay);

function group(relay) {
  const nodeIDs = Array.isArray(relay?.node_ids) ? [...relay.node_ids] : [];
  return {
    relay: {
      strategy: STRATEGIES.includes(relay?.strategy) ? relay.strategy : STRATEGIES[0],
      source_id: nodeIDs.length ? "" : relay?.source_id || "",
      node_ids: nodeIDs,
    },
    mode: nodeIDs.length ? "nodes" : "source",
  };
}

// hopsFromProxy is the saved chain with its groups, or null when the backend
// reports none (the editor then reads proxy.route as before).
export function hopsFromProxy(proxy) {
  const hops = proxy?.route_hops;
  if (!Array.isArray(hops) || !hops.some((hop) => hop?.relay)) return null;
  return hops.map((hop) => (hop?.relay ? group(hop.relay) : hop?.node_id || ""));
}

// newGroup turns a 前置 into a group over its node's source, or the first
// source with more than one node; sources another group of route takes whole
// come last (the backend refuses one source for two groups).
export function newGroup(sources = [], nodeID = "", route = []) {
  const taken = new Set(route.filter((hop) => isGroup(hop) && hop.mode === "source").map((hop) => hop.relay.source_id));
  const free = sources.filter((s) => !taken.has(s.id));
  const owner = free.find((s) => s.nodes?.some((n) => n.id === nodeID))
    || free.find((s) => (s.nodes?.length || 0) > 1)
    || free[0] || sources[0];
  return group({ strategy: STRATEGIES[0], source_id: owner?.id || "" });
}

export const withMode = (hop, mode) => ({ ...hop, mode });
export const withStrategy = (hop, strategy) => ({ ...hop, relay: { ...hop.relay, strategy } });
export const withSource = (hop, sourceID) => ({ ...hop, relay: { ...hop.relay, source_id: sourceID } });

// moveNode moves the chosen node at index by delta (-1 up, 1 down): 故障转移
// uses the first usable node in this order.
export function moveNode(hop, index, delta) {
  const ids = [...hop.relay.node_ids];
  const to = index + delta;
  if (to < 0 || to >= ids.length) return hop;
  [ids[index], ids[to]] = [ids[to], ids[index]];
  return { ...hop, relay: { ...hop.relay, node_ids: ids } };
}

export function toggleNode(hop, id) {
  const ids = hop.relay.node_ids;
  return { ...hop, relay: { ...hop.relay, node_ids: ids.includes(id) ? ids.filter((x) => x !== id) : [...ids, id] } };
}

// candidates are the nodes a group switches between: usable ones in its range
// that are not a fixed hop of the same chain. nodes carry sourceID.
export function candidates(hop, route, nodes) {
  const fixed = new Set(route.filter((h) => typeof h === "string" && h));
  const pool = hop.mode === "nodes"
    ? hop.relay.node_ids.map((id) => nodes.find((n) => n.id === id)).filter(Boolean)
    : nodes.filter((n) => n.sourceID === hop.relay.source_id);
  return pool.filter((n) => !n.blocked_reason && !n.relay && !fixed.has(n.id));
}

// sourceNodes lists the nodes of sources with their sourceID, as candidates
// reads them.
export const sourceNodes = (sources = []) => sources.flatMap((s) => (s.nodes || []).map((n) => ({ ...n, sourceID: s.id })));

// groupProblem is why the backend would refuse the group, or "": "unusable"
// (a chosen node cannot be used), "few" (under two usable candidates), "many"
// (over MAX_RELAY_NODES) or "overlap" (a candidate of another group of route,
// which also covers two groups over one source).
export function groupProblem(hop, route, nodes) {
  const usable = candidates(hop, route, nodes);
  if (hop.mode === "nodes" && usable.length !== hop.relay.node_ids.length) return "unusable";
  if (usable.length < 2) return "few";
  if (usable.length > MAX_RELAY_NODES) return "many";
  const mine = new Set(usable.map((n) => n.id));
  const shared = route.some((other) => other !== hop && isGroup(other) && candidates(other, route, nodes).some((n) => mine.has(n.id)));
  return shared ? "overlap" : "";
}

export const groupValid = (hop, route, nodes) => !groupProblem(hop, route, nodes);

// problemName names a group problem for its panel; "few" while nodes are
// still being chosen shows as the count alone.
export function problemName(problem, hop, tr) {
  if (problem === "unusable") return tr("含不可用节点");
  if (problem === "few") return hop.mode === "nodes" ? "" : tr("可用节点不足 2 个");
  if (problem === "many") return tr("超过 {0} 个节点", MAX_RELAY_NODES);
  if (problem === "overlap") return tr("和其他自动前置重复");
  return "";
}

// routeBody is the POST /api/proxy/route body: node_ids without a group, hops
// with one.
export function routeBody(route) {
  if (!route.some(isGroup)) return { node_ids: route };
  return {
    hops: route.map((hop) => (isGroup(hop)
      ? { relay: hop.mode === "nodes"
        ? { strategy: hop.relay.strategy, node_ids: hop.relay.node_ids }
        : { strategy: hop.relay.strategy, source_id: hop.relay.source_id } }
      : { node_id: hop })),
  };
}

// routeKey compares routes as they are saved.
export const routeKey = (route) => JSON.stringify(routeBody(route));

// groupRange names a group's range: the source, or how many nodes are chosen.
export function groupRange(hop, sources, tr) {
  if (hop.mode === "nodes") return tr("自选 {0} 个", hop.relay.node_ids.length);
  return sources.find((s) => s.id === hop.relay.source_id)?.name || tr("待选择");
}

export const strategyName = (strategy, tr) => (strategy === "url-test" ? tr("最低延迟") : tr("故障转移"));
