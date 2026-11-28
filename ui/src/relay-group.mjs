// Relay auto-switch groups (#1063) in our chain editor (user, 2026-11-28:
// set inside the 前置 row). A route entry is a node ID ("" while still to
// choose) or a group { relay: { strategy, source_id, node_ids }, mode }, where
// mode is which range the row shows: "source" (整份订阅) or "nodes" (自选节点).
// Only 前置 hops can be groups; the exit stays one fixed node.

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
// source with more than one node.
export function newGroup(sources = [], nodeID = "") {
  const owner = sources.find((s) => s.nodes?.some((n) => n.id === nodeID))
    || sources.find((s) => (s.nodes?.length || 0) > 1)
    || sources[0];
  return group({ strategy: STRATEGIES[0], source_id: owner?.id || "" });
}

export const withMode = (hop, mode) => ({ ...hop, mode });
export const withStrategy = (hop, strategy) => ({ ...hop, relay: { ...hop.relay, strategy } });
export const withSource = (hop, sourceID) => ({ ...hop, relay: { ...hop.relay, source_id: sourceID } });

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

// groupValid: at least two usable candidates; chosen nodes must all be usable.
export function groupValid(hop, route, nodes) {
  const usable = candidates(hop, route, nodes);
  if (usable.length < 2) return false;
  return hop.mode !== "nodes" || usable.length === hop.relay.node_ids.length;
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
