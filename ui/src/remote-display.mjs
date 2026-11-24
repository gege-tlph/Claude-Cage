// The guest reports its running pixel density. Zoom changes its logical workspace size,
// while noVNC fits the resulting framebuffer into the available CSS area.
// A guest that follows its viewer is given pixelRatio, the density of the
// screen showing it: it renders one pixel per screen pixel, and zoom changes
// the density instead of the pixel count. The reduced-clarity setting (1.5)
// keeps three quarters of those pixels in each direction.
export function displayGeometry({ width, height }, { desktop = false, zoom = 1, density = 2, pixelRatio } = {}) {
  if (width < (desktop ? 320 : 160) || height < 240) return null;
  const follow = pixelRatio > 0;
  const automatic = desktop && zoom === "auto" && !follow;
  const zoomFactor = Number.isFinite(zoom) && zoom > 0 ? zoom : 1;
  let scale = !follow && desktop && Number.isFinite(density) && density >= 1 && density <= 8
    ? density : density === 1.5 ? 1.5 : 2;
  let ratio = scale / zoomFactor;
  if (follow) ratio = Math.max(1, Math.max(1, Math.min(2, pixelRatio)) * scale / 2);
  // Native Windows uses guest DPI; Linux follows the host screen density.
  // Only native automatic mode limits allocation to twelve million pixels.
  const limit = Math.min(1, 8192 / (width * ratio), 8192 / (height * ratio),
    automatic ? Math.sqrt(12_000_000 / (width * height * ratio * ratio)) : 1);
  // Chromium applies a density in steps of 1/64.
  if (follow) scale = Math.round(Math.max(0.75, Math.min(4, ratio * limit * zoomFactor)) * 64) / 64;
  return {
    width: Math.max(desktop ? 320 : Math.ceil(160 * scale), Math.floor(width * ratio * limit)),
    height: Math.max(desktop ? 240 : Math.ceil(240 * scale), Math.floor(height * ratio * limit)),
    ...(follow ? { scale } : {}),
  };
}

// Reports that the window moved to a screen of another density, or that the
// page zoom changed. A media query matches one ratio only, so each change
// registers the next. A window dragged to another screen keeps its CSS size,
// so no resize follows, and a host WebView may deliver no media change either:
// the ratio is also compared once a second. Returns the function that stops.
export function watchPixelRatio(changed, view = window, { interval = 1000 } = {}) {
  let query, ratio, stopped = false;
  const update = () => {
    if (stopped) return;
    query.removeEventListener?.("change", update);
    listen();
    changed();
  };
  const listen = () => {
    ratio = view.devicePixelRatio;
    query = view.matchMedia(`(resolution: ${ratio}dppx)`);
    query.addEventListener?.("change", update, { once: true });
  };
  listen();
  const timer = view.setInterval?.(() => { if (view.devicePixelRatio !== ratio) update(); }, interval);
  return () => { stopped = true; view.clearInterval?.(timer); query.removeEventListener?.("change", update); };
}

// Serialize guest changes: a slow old request must finish before the newest
// size is applied. Failed changes get bounded retries even without another
// window resize. Disposing never starts another request.
export function displaySizeSync(resize, { onError = () => {}, onSuccess = () => {}, delay = 160 } = {}) {
  let desired, applied = "", timer, busy = false, disposed = false, retries = 0;
  const key = (size) => size ? `${size.width}x${size.height}@${size.scale ?? ""}` : "";
  const schedule = (ms) => { clearTimeout(timer); timer = setTimeout(apply, ms); };
  async function apply() {
    timer = null;
    if (disposed || busy || !desired || key(desired) === applied) return;
    const target = desired;
    let retryDelay;
    busy = true;
    try {
      await resize(target);
      if (!disposed) {
        applied = key(target);
        if (key(desired) === applied) { retries = 0; onSuccess(); }
      }
    } catch (error) {
      if (!disposed && key(desired) === key(target)) {
        onError(error);
        if (retries < 3) retryDelay = 500 * 2 ** retries++;
      }
    } finally {
      busy = false;
      if (!disposed) {
        if (key(desired) !== key(target)) schedule(delay);
        else if (retryDelay !== undefined) schedule(retryDelay);
      }
    }
  }
  return {
    update(size) {
      if (disposed || !size || key(size) === key(desired)) return;
      desired = size;
      // Replace a retry timer, but let an ordinary resize tick fire during a drag.
      if (!busy && (timer == null || retries > 0)) schedule(delay);
      retries = 0;
    },
    retry() { if (!disposed) { applied = ""; retries = 0; schedule(0); } },
    // The newest size this viewer asked for.
    target: () => desired,
    dispose() { disposed = true; clearTimeout(timer); },
  };
}
