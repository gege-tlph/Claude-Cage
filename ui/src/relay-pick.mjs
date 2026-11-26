import { detectNodeRegion } from "./proxy-region.mjs";

// detectNodeRegion answers "ZZ" when a name shows no region.
const regionOf = (name) => {
  const code = detectNodeRegion(name);
  return code === "ZZ" ? "" : code;
};

// The region of an exit: the one its panel declares (Ferry's ISO code), else
// the one its name shows (flag, place name, airport code).
export function exitRegion(exit) {
  return exit?.region || regionOf(exit?.name);
}

// The relay in front of an exit: the fastest working candidate in the exit's
// region, else the fastest working candidate anywhere (sameRegion false).
// results are POST /api/proxy/delay's: [{ node_id, delay } | { node_id, error }].
export function pickRelay({ region, candidates, results, exclude = [] }) {
  const delays = new Map();
  for (const result of results || []) {
    if (!result?.error && Number.isFinite(result?.delay) && result.delay > 0) delays.set(result.node_id, result.delay);
  }
  const working = (candidates || []).filter((node) => delays.has(node.id) && !exclude.includes(node.id));
  const fastest = (list) => list.reduce((best, node) => (!best || delays.get(node.id) < delays.get(best.id) ? node : best), null);
  const same = region ? fastest(working.filter((node) => regionOf(node.name) === region)) : null;
  const node = same || fastest(working);
  return node ? { node, delay: delays.get(node.id), sameRegion: Boolean(same) } : null;
}
