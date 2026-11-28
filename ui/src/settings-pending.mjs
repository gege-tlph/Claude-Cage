import { canonicalExitPolicy } from "./safety-state.mjs";

// The names the settings pages show for each setting, for the submit bar's
// 「待应用：…」 (settings-submit-bar.jsx).
const NAMES = {
  dns_url: "加密 DNS", dns_fallback_urls: "备用加密 DNS", dns_bootstrap_urls: "节点域名解析",
  dns_public_fallback: "公共 DNS 备用", install_tls12_compat: "安装与工具下载兼容模式",
  tls_client_hello_split: "TLS ClientHello 分段传输兼容模式", location_ipinfo_fallback: "出口位置查询备用服务 ipinfo.io",
  exit_policy: "IP 变化规则", direct_lan: "局域网直连",
  timezone: "时区", auto_timezone: "时区", locale: "系统语言", auto_language: "系统语言", browser_language: "网页首选语言",
  clock_12_hour: "时间显示格式", claude_terminal_command: "Claude 终端启动命令", reuse_claude_login: "使用无痕标签页登录 Claude",
  balanced_display: "优先流畅", project_file_cache: "项目文件缓存", transfer_allow_unclean: "允许未支持格式原样传递",
  keyboard: "键盘习惯", mobile_relay: "远程连接",
};

function differs(key, draft, current) {
  if (key === "exit_policy") return JSON.stringify(canonicalExitPolicy(draft[key])) !== JSON.stringify(canonicalExitPolicy(current[key]));
  return JSON.stringify(draft[key] ?? null) !== JSON.stringify(current[key] ?? null);
}

// pendingSummary lists what the submit bar would apply: the node or chain
// first, then each changed setting by its name, at most three and the count
// of the rest. A changed time zone shows the zone itself.
export function pendingSummary(draft, current, proxyPending, tr = (text, ...values) => values.reduce((out, value, i) => out.replace(`{${i}}`, value), text)) {
  const names = [];
  for (const key of Object.keys({ ...current, ...draft })) {
    if (key === "browser_home_url" || !differs(key, draft, current)) continue;
    const name = NAMES[key];
    if (!name) continue;
    const text = name === "时区" && draft.timezone && !draft.auto_timezone ? tr("时区 {0}", draft.timezone) : tr(name);
    if (!names.includes(text)) names.push(text);
  }
  const parts = [proxyPending?.summary, ...names].filter(Boolean);
  if (parts.length > 3) return parts.slice(0, 3).join(" · ") + " · " + tr("等 {0} 项", parts.length - 3);
  return parts.join(" · ");
}
