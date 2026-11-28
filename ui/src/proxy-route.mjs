// Editing a proxy route (design agreed 2026-11-28, demo v11 「链式代理」).
// A route lists node IDs from the first hop to the exit; "" is a hop still to
// choose. The slot being chosen is `active` (-1: none). Every function
// returns the next { route, active } and leaves its input alone.

// nextEmpty is the first hop still to choose, or -1.
export function nextEmpty(route) {
  return route.findIndex((id) => !id);
}

// toChain switches a single exit to a chain: the old exit stays the exit and
// a 前置 slot opens in front of it, the one being chosen.
export function toChain(route) {
  if (route.length > 1) return { route, active: nextEmpty(route) };
  return { route: ["", route[0] || ""], active: 0 };
}

// toSingle keeps the exit (or the only node chosen) as the single node.
export function toSingle(route) {
  const exit = route.at(-1) || route.find(Boolean) || "";
  return { route: [exit], active: -1 };
}

// makeRelay is 「设为前置」 on the exit of a fresh chain: the old exit moves in
// front, and the exit becomes the slot being chosen.
export function makeRelay(route) {
  if (route.length !== 2 || route[0] || !route[1]) return { route, active: nextEmpty(route) };
  return { route: [route[1], ""], active: 1 };
}

// canMakeRelay: the chain is fresh (an empty 前置 before a chosen exit).
export function canMakeRelay(route) {
  return route.length === 2 && !route[0] && Boolean(route[1]);
}

// move swaps hop index with its neighbour (-1 up, +1 down); the moved hop
// stays the one being chosen.
export function move(route, index, by) {
  const to = index + by;
  if (to < 0 || to >= route.length) return { route, active: index };
  const next = [...route];
  [next[index], next[to]] = [next[to], next[index]];
  return { route: next, active: to };
}

// remove drops a 前置 (never the exit); a chain of one becomes single.
export function remove(route, index) {
  if (index === route.length - 1 || route.length < 2) return { route, active: nextEmpty(route) };
  const next = route.filter((_, i) => i !== index);
  return { route: next, active: next.length > 1 ? nextEmpty(next) : -1 };
}

// addRelay opens a 前置 slot just before the exit.
export function addRelay(route) {
  const next = [...route.slice(0, -1), "", route.at(-1) || ""];
  return { route: next, active: next.length - 2 };
}

// choose puts node id into the slot being chosen (or the first empty one);
// a node already elsewhere in the route trades places with what was there.
// A single route just takes the node as its exit.
export function choose(route, active, id) {
  if (route.length < 2) return { route: [id], active: -1 };
  const slot = active >= 0 ? active : nextEmpty(route);
  if (slot < 0) return { route, active };
  const next = [...route];
  const at = next.indexOf(id);
  if (at >= 0 && at !== slot) next[at] = next[slot];
  next[slot] = id;
  return { route: next, active: nextEmpty(next) };
}
