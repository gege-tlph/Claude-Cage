// The flows the failure card's tools run (failure-card.jsx). Each carries
// what the user started through to its end, never only the step that failed:
// a node is applied again and the network checked, a subscription updated
// and its node reapplied (the backend does both in one job).

// waitForJob polls until job id has ended; the job, or null when another
// one replaced it.
export async function waitForJob(api, id, interval = 800) {
  for (;;) {
    const status = await api("/status");
    if (status.job?.id !== id) return null;
    if (!status.job.running) return status.job;
    await new Promise((resolve) => setTimeout(resolve, interval));
  }
}

// settleJob waits for the job that is running, if any, and returns how it
// ended (null when none ran or another replaced it). The backend runs one job
// at a time: settings saved while a node change restarts the environment
// would be refused (409).
export async function settleJob(api, interval) {
  const status = await api("/status");
  return status.job?.running ? waitForJob(api, status.job.id, interval) : null;
}

// ended throws for a job that failed, so the tool shows 失败 and the job's
// own failure card takes over.
function ended(job) {
  if (job?.error) throw Object.assign(new Error(job.error), { failure: job.failure });
  if (job?.canceled) throw new Error(job.message || "已取消");
  return job;
}

// reapplyNode applies the saved node (or chain) again; with the environment
// running the backend then checks the network.
export async function reapplyNode(api) {
  const proxy = await api("/proxy");
  const route = proxy.route?.length ? proxy.route : proxy.selected ? [proxy.selected] : [];
  if (!route.length) throw new Error("尚未选择代理节点");
  const job = await api("/proxy/route", { node_ids: route, port: proxy.port });
  return ended(await waitForJob(api, job.id));
}

// recheckNetwork runs the network check, or starts the environment when it
// cannot be checked in place.
export async function recheckNetwork(api) {
  const status = await api("/status");
  const job = await api(status.ready && status.safety?.gate ? "/safety/check" : "/start", {});
  return ended(await waitForJob(api, job.id));
}

// refreshSource downloads a subscription again, through the computer's own
// network with host.
export async function refreshSource(api, id, host = false) {
  if (!id) throw new Error("订阅来源不存在");
  const job = await api("/proxy/refresh", { id, host_download: Boolean(host) });
  return ended(await waitForJob(api, job.id));
}

// enableClientHelloSplit saves the TLS ClientHello split setting, then
// checks the network again with it; a stopped environment starts instead
// (start), so the flow still ends with a checked network.
export async function enableClientHelloSplit(api, start) {
  const settings = await api("/settings");
  const payload = { ...settings, tls_client_hello_split: true };
  delete payload.browser_home_url;
  const job = await api("/settings", payload);
  ended(await waitForJob(api, job.id));
  if ((await api("/status")).vm_running) return recheckNetwork(api);
  if (start) return start();
}

// enableInstallTLS12 saves the install TLS 1.2 mode, then continues the
// install (carryOn), which ends where the install ends.
export async function enableInstallTLS12(api, carryOn) {
  const settings = await api("/settings");
  const payload = { ...settings, install_tls12_compat: true };
  delete payload.browser_home_url;
  const job = await api("/settings", payload);
  ended(await waitForJob(api, job.id));
  return carryOn?.();
}

// restartEnvironment stops the environment, waits for the stop to end, then
// starts it: one step for a setting that applies at start (a new exit's time
// zone). The start runs on in the status bar.
export async function restartEnvironment(api) {
  const stopping = await api("/stop", {});
  if (stopping?.id) ended(await waitForJob(api, stopping.id));
  return api("/start", {});
}

// openSettingsSection asks the settings page to show one section (加密 DNS).
export function openSettingsSection(section) {
  setTimeout(() => window.dispatchEvent(new CustomEvent("cage-settings-section", { detail: section })), 0);
}

// desktopFlow runs Claude Desktop actions in turn, each to its end: install
// or update then open, close then open. The flow ends with the window open.
export async function desktopFlow(api, ...actions) {
  let job;
  for (const action of actions) {
    const accepted = await api(`/claude-desktop/${action}`, {});
    job = ended(await waitForJob(api, accepted.id));
  }
  return job;
}
