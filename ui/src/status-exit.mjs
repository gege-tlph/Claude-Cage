// Text of the status bar's exit item (#804). While hidden, the address and
// place never reach the page, so screenshots, screen sharing and assistive
// tools all miss them. States that reveal nothing stay as they are.
export function statusExitText(status, hidden, tr) {
  if (status?.network_error) return tr("出口待确认");
  const ip = status?.safety?.exit_ip || status?.location?.ip;
  if (!status?.location && !ip) return tr("指定代理出口");
  if (hidden) return tr("IP 已隐藏");
  return [ip, status.location?.country, status.location?.city].filter(Boolean).join(" · ");
}
