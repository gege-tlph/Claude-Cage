const minimumWidth = 220;
const minimumHeight = 128;
const dividerSize = 8;

export function containsPane(tree, pane) {
  return typeof tree === "string" ? tree === pane
    : containsPane(tree.first, pane) || containsPane(tree.second, pane);
}

function keepPanes(tree, keep) {
  if (typeof tree === "string") return keep(tree) ? tree : null;
  const first = keepPanes(tree.first, keep);
  const second = keepPanes(tree.second, keep);
  if (!first || !second) return first || second;
  return first === tree.first && second === tree.second ? tree : { ...tree, first, second };
}

function keepGroups(groups, keep) {
  const next = groups.map(tree => keepPanes(tree, keep));
  return next.every((tree, index) => tree === groups[index]) ? groups
    : next.filter(tree => tree && typeof tree !== "string");
}

export function reconcileSplits(groups, paneIds) {
  return keepGroups(groups, id => paneIds.has(id));
}

export function splitTerminal(groups, target, pane, side, id) {
  if (target === pane) return groups;
  const remaining = keepGroups(groups, current => current !== pane);
  const before = side === "left" || side === "top";
  const axis = side === "left" || side === "right" ? "x" : "y";
  const split = { id, axis, ratio: 0.5, first: before ? pane : target, second: before ? target : pane };
  const insert = tree => typeof tree === "string" ? tree === target ? split : tree
    : { ...tree, first: insert(tree.first), second: insert(tree.second) };
  const index = remaining.findIndex(tree => containsPane(tree, target));
  return index === -1 ? [...remaining, split]
    : remaining.map((tree, i) => i === index ? insert(tree) : tree);
}

// Prefer the sibling in the smallest branch that still has a surviving pane.
export function adjacentTerminal(tree, pane, available) {
  if (typeof tree === "string") return "";
  const inFirst = containsPane(tree.first, pane);
  const selected = inFirst ? tree.first : tree.second;
  const sibling = inFirst ? tree.second : tree.first;
  let nearest = keepPanes(sibling, id => available.has(id));
  while (nearest && typeof nearest !== "string") nearest = inFirst ? nearest.first : nearest.second;
  return adjacentTerminal(selected, pane, available) || nearest || "";
}

export function resizeTerminalSplit(groups, id, ratio) {
  const resize = tree => {
    if (typeof tree === "string") return tree;
    if (tree.id === id) return tree.ratio === ratio ? tree : { ...tree, ratio };
    const first = resize(tree.first);
    const second = resize(tree.second);
    return first === tree.first && second === tree.second ? tree : { ...tree, first, second };
  };
  return groups.map(resize);
}

export function terminalGeometry(tree, width, height) {
  const sizes = new Map();
  function measure(node) {
    if (typeof node === "string") return { width: minimumWidth, height: minimumHeight };
    const first = measure(node.first), second = measure(node.second);
    const size = node.axis === "x"
      ? { width: first.width + second.width + dividerSize, height: Math.max(first.height, second.height) }
      : { width: Math.max(first.width, second.width), height: first.height + second.height + dividerSize };
    sizes.set(node, size);
    return size;
  }

  const panes = new Map(), dividers = [];
  function place(node, bounds) {
    if (typeof node === "string") {
      panes.set(node, bounds);
      return;
    }
    const across = node.axis === "x";
    const length = across ? "width" : "height";
    const start = across ? "left" : "top";
    const leafMinimum = across ? minimumWidth : minimumHeight;
    const available = bounds[length] - dividerSize;
    const min = sizes.get(node.first)?.[length] ?? leafMinimum;
    const max = available - (sizes.get(node.second)?.[length] ?? leafMinimum);
    const first = Math.min(max, Math.max(min, Math.round(available * node.ratio)));
    dividers.push({ id: node.id, axis: node.axis, first, min, max, available,
      bounds: { ...bounds, [start]: bounds[start] + first, [length]: dividerSize } });
    place(node.first, { ...bounds, [length]: first });
    place(node.second, { ...bounds, [start]: bounds[start] + first + dividerSize, [length]: available - first });
  }

  const minimum = measure(tree);
  place(tree, { left: 0, top: 0, width: Math.max(width, minimum.width), height: Math.max(height, minimum.height) });
  return { panes, dividers, minimum };
}
