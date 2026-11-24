// Settings a running environment reads only when it starts. The status names
// the saved ones it does not use yet (restart_pending); the workbench offers
// a restart for them.
export const restartSettingNames = {
  balanced_display: "优先流畅",
  project_file_cache: "项目文件缓存",
  memory: "内存上限",
  cpus: "CPU 上限",
  disk: "硬盘上限",
};

// newlyPending lists what became pending since the last status; nothing is
// new on the first one, so settings left pending earlier do not ask again.
export function newlyPending(seen, next) {
  if (!seen || !Array.isArray(next)) return [];
  return next.filter((id) => !seen.includes(id));
}

export function restartDialog(pending, tr) {
  return {
    title: "重启环境以应用更改",
    body: [pending.map((id) => tr(restartSettingNames[id] ?? id)).join(" · "), "终端会话会结束，文件会保留。"],
    confirm: "立即重启",
    cancel: "稍后",
  };
}
