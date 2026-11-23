import React, { useEffect, useState } from "react";
import { FolderOpen } from "lucide-react";
import { api, native, nativeAvailable } from "./api";
import { useTranslation } from "./interface-language.mjs";

export function InitializationStorage({ value, onChange, disabled }) {
  const tr = useTranslation();
  const [error, setError] = useState("");
  const [attempt, retry] = useState(0);
  useEffect(() => {
    if (value) return;
    let live = true;
    api("/environment/storage").then(location => {
      if (live) onChange({ location, directory: location.suggested_directory || location.directory });
    }).catch(error => { if (live) setError(error.message); });
    return () => { live = false; };
  }, [Boolean(value), onChange, attempt]);
  const locked = disabled || !value || value.location.has_environment;
  async function choose() {
    try {
      const directory = await native("chooseFolder", { purpose: "environmentStorage" });
      if (directory) { onChange({ ...value, directory }); setError(""); }
    } catch (error) { setError(error.message); }
  }
  return <section className="initialization-storage" aria-label={tr("环境存储目录")}>
    <label htmlFor="initialization-directory">{tr("存储目录")}</label>
    <div className="initialization-directory-control">
      <input id="initialization-directory" value={value?.directory || ""} disabled={locked} spellCheck={false} placeholder={value ? tr("请输入存储目录的完整路径") : tr("正在读取默认目录…")} onChange={event => onChange({ ...value, directory: event.target.value })} />
      {nativeAvailable() && <button type="button" disabled={locked} onClick={choose}><FolderOpen size={16} />{tr("选择文件夹")}</button>}
    </div>
    {value?.location.has_environment && <p>{tr("已有虚拟磁盘，继续使用原存储目录")}</p>}
    {error && <p role="alert">{tr(error)}<button onClick={() => { setError(""); retry(value => value + 1); }}>{tr("重新读取")}</button></p>}
  </section>;
}
