import React, { useEffect, useState } from "react";
import { api } from "./api";
import { useAccount } from "./account";
import { FerryMark, FerrySetup, RELAY_SOURCE } from "./ferry-setup.jsx";
import { useTranslation } from "./interface-language.mjs";

// 「从 Ferry 导入」 in the proxy settings: the Ferry steps for an account that
// exists, to finish a first run that stopped part way, or to take an exit
// Ferry changed. The relay subscription imported before is used again.
export function FerryImport({ proxy, busy, onChanged }) {
  const tr = useTranslation();
  const { account } = useAccount();
  const [open, setOpen] = useState(false);
  const [known, setKnown] = useState(null);
  useEffect(() => {
    let live = true;
    api("/ferry").then((value) => { if (live) setKnown(value); }, () => { if (live) setKnown({}); });
    return () => { live = false; };
  }, [open]);
  const relaySource = proxy?.sources?.find((source) => source.name === RELAY_SOURCE && source.remote) || null;
  if (open && known) {
    return (
      <FerrySetup
        inline
        account={account}
        known={known}
        relaySource={relaySource}
        onBack={() => setOpen(false)}
        onDone={() => { setOpen(false); onChanged?.(); }}
      />
    );
  }
  return (
    <section className="ferry-import" aria-label={tr("从 Ferry 导入")}>
      <div className="ferry-import-heading">
        <h4><FerryMark size={20} />{tr("从 Ferry 导入")}</h4>
        <button type="button" disabled={busy || !known} onClick={() => setOpen(true)}>{tr("登录 Ferry")}</button>
      </div>
      {known?.user && <p className="ferry-import-state">{known.user}</p>}
    </section>
  );
}
