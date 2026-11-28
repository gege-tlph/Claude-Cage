// The install's overall progress (design agreed 2026-11-28, demo v6–v8):
// about a dozen steps, each weighted by how long it usually takes, read from
// the progress labels the backend sends (internal/workbench setup_progress.go).
// The download step is refined by its bytes. The number only moves forward.
const STEPS = [
  [/^正在准备 /, 2],
  [/^正在下载 .*(系统)?镜像$/, 24],
  // The image, or the full package's installation files.
  [/^正在校验/, 3],
  [/^正在启动 Linux 基础环境/, 7],
  [/^正在复制安装文件/, 4],
  [/^正在安装 Linux 基础软件包/, 12],
  [/ mise$/, 2],
  [/^正在安装 Python/, 5],
  [/Chromium/, 10],
  [/^正在安装 Node\.js/, 6],
  [/GitHub CLI/, 4],
  [/Claude Code/, 9],
  [/code-server/, 8],
  [/^正在配置桌面服务/, 7],
  [/^正在启动浏览器和编辑器/, 5],
];
const TOTAL = STEPS.reduce((sum, [, weight]) => sum + weight, 0);

// stepOf is the step a label belongs to, -1 when unknown.
export function stepOf(label = "") {
  return STEPS.findIndex(([pattern]) => pattern.test(label));
}

// setupPercent: { done, current, percent } in percent of the whole install
// for a progress report ({ label, received, total }); previous keeps a step
// whose label is unknown where the last known one was, and the number from
// going back.
export function setupPercent(progress, previous = { step: -1, percent: 0 }) {
  let step = stepOf(progress?.label);
  if (step < 0) step = previous.step;
  if (step < 0) return { step, done: previous.percent, current: 0, percent: previous.percent };
  const before = STEPS.slice(0, step).reduce((sum, [, weight]) => sum + weight, 0);
  const weight = STEPS[step][1];
  const received = Number(progress?.received), total = Number(progress?.total);
  const fraction = Number.isFinite(received) && total > 0 && received <= total ? received / total : 0;
  const done = (before + weight * fraction) / TOTAL * 100;
  const percent = Math.max(previous.percent, Math.floor(done));
  return { step, done: Math.max(done, previous.percent), current: Math.max(0, (before + weight) / TOTAL * 100 - Math.max(done, previous.percent)), percent };
}

export const SETUP_STEPS = STEPS.length;
