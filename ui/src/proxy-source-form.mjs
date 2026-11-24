// Whether the add/edit source form holds changes of its own. Opening an
// untouched "add source" or "edit source" form must not block setup.
// From upstream PR #915 (isSourceFormDirty); the defaults match
// proxy-settings.jsx's emptyForm and emptyManual.

const defaultInterval = 24;

export function isSourceFormDirty(form, mode, manual, sources = [], nodeEdit = null) {
  if (!form) return false;
  if (form.id) {
    const existing = sources.find((src) => src.id === form.id);
    if (!existing) return true;
    if ((form.name ?? "") !== (existing.name ?? "")) return true;
    if (Number(form.interval_hours ?? 0) !== Number(existing.interval_hours ?? 0)) return true;
    if (String(form.url ?? "").trim() !== "") return true;
    if (String(form.content ?? "").trim() !== "") return true;
    // An address typed into the node editor is not saved yet either.
    if (nodeEdit?.id) {
      const node = existing.nodes?.find((item) => item.id === nodeEdit.id);
      if (node && (String(nodeEdit.server ?? "") !== String(node.server ?? "") || String(nodeEdit.port ?? "") !== String(node.port ?? ""))) return true;
    }
    return false;
  }
  if (String(form.name ?? "").trim() !== "") return true;
  if (mode === "url") {
    return (
      String(form.url ?? "").trim() !== "" ||
      Number(form.interval_hours ?? defaultInterval) !== defaultInterval ||
      Boolean(form.host_download)
    );
  }
  if (mode === "file") return String(form.content ?? "").trim() !== "";
  if (mode === "manual" && manual) {
    const defaultPort = manual.type === "ssh" ? "22" : "1080";
    return Boolean(
      String(manual.name ?? "").trim() ||
      manual.type !== "socks5" ||
      String(manual.server ?? "").trim() ||
      String(manual.port ?? "") !== defaultPort ||
      String(manual.username ?? "").trim() ||
      String(manual.password ?? "") ||
      manual.sshAuth !== "password" ||
      String(manual.privateKey ?? "").trim() ||
      String(manual.passphrase ?? "") ||
      String(manual.hostKey ?? "").trim(),
    );
  }
  return false;
}
