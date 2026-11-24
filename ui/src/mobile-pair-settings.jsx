import React, { useEffect, useRef, useState } from "react";
import { api, writeClipboard } from "./api";
import { QrCode } from "lucide-react";
import { useTranslation } from "./interface-language.mjs";
import { localizeBackendMessage } from "./backend-message.mjs";

export function MobilePairSettings({ relay, savedRelay, onChange, onConfirm }) {
  const tr = useTranslation();
  const [custom, setCustom] = useState(Boolean(relay));
  const [token, setToken] = useState("");
  const [code, setCode] = useState(null);
  const [paired, setPaired] = useState(false);
  const [device, setDevice] = useState(null);
  const [power, setPower] = useState(null);
  const [link, setLink] = useState(null);
  const [officialRelay, setOfficialRelay] = useState("");
  const [mobileAccessBlocked, setMobileAccessBlocked] = useState(false);
  const [busy, setBusy] = useState(false);
  const [notice, setNotice] = useState("");
  const statusRevision = useRef(0);
  useEffect(() => {
    let active = true;
    const check = async () => {
      const revision = statusRevision.current;
      try {
        const state = await api("/mobile-pair");
        if (!active || revision !== statusRevision.current) return;
        setPaired(state.paired);
        setDevice(state.paired ? state.device ?? null : null);
        setPower(state.paired ? state.keep_awake ?? null : null);
        setLink(state.paired && state.relay ? { relay: state.relay, state: state.link || "" } : null);
        setOfficialRelay(state.official_relay || "");
        setMobileAccessBlocked(Boolean(state.mobile_access_blocked));
        if (state.paired) setCode(null);
        else if (code && Date.now() >= Date.parse(code.expires)) {
          setCode(null);
          setNotice("二维码已过期，请重新生成。");
        } else if (code && state.status.includes("未完成")) {
          setCode(null);
          setNotice(state.status);
        }
      } catch (e) {
        if (active && revision === statusRevision.current) setNotice(e.message);
      }
    };
    check();
    const timer = setInterval(check, code ? 1500 : 3000);
    return () => {
      active = false;
      clearInterval(timer);
    };
  }, [code]);
  async function generate() {
    // The official relay is reached over the host network. Name it and ask
    // before Cage sends it anything (#740).
    if (
      !custom &&
      !(await onConfirm({
        title: "连接 Cage 官方中继？",
        body: `${officialRelay} · ${tr("直连")}`,
        confirm: "连接",
      }))
    )
      return;
    setBusy(true);
    setNotice("");
    setCode(null);
    try {
      setCode(
        await api("/mobile-pair", {
          relay: savedRelay,
          admin: custom ? token : "",
          confirm: custom ? "" : officialRelay,
        }),
      );
    } catch (e) {
      setNotice(e.message);
    } finally {
      setBusy(false);
    }
  }
  async function remove() {
    if (
      !(await onConfirm({
        title: "移除配对手机？",
        body: device?.model
          ? tr("移除 {0} 后，这部手机需要重新扫码才能访问工作台。", device.model)
          : "这部手机需要重新扫码才能访问工作台。",
        confirm: "移除手机",
      }))
    )
      return;
    setBusy(true);
    try {
      await api("/mobile-pair", {}, "DELETE");
      statusRevision.current++;
      setPaired(false);
      setDevice(null);
      setPower(null);
      setLink(null);
      setNotice("");
    } catch (e) {
      setNotice(e.message);
    } finally {
      setBusy(false);
    }
  }
  // Center the QR and its actions. Relay controls use the same theme below.
  return (
    <section className="remote-link-settings" aria-label={tr("远程连接")}>
      <div className="section-title">
        <h3>{tr("远程连接")}</h3>
      </div>
      <section className="remote-pair-card" aria-label={tr("手机配对")}>
        <h4>{paired ? tr("手机已配对") : tr("用手机连接 Cage lite")}</h4>
        {paired && (
          <p className="remote-pair-device" aria-label={tr("已配对手机")}>
            {device?.model ? (
              <>
                <strong>{device.model}</strong>
                <span>{device.platform === "ios" ? "iOS" : "Android"}</span>
              </>
            ) : mobileAccessBlocked ? (
              <span>{tr("当前账户模式暂不支持手机访问，请在「设置 → 账户」设置密码或开启「记住登录」后重新连接。")}</span>
            ) : (
              <span>{tr("型号待获取，请更新手机端 Cage lite 后重新连接。")}</span>
            )}
          </p>
        )}
        {link && (
          <p className="remote-pair-device remote-pair-relay" aria-label={tr("中继")}>
            <span>{tr("中继")}</span>
            <strong>{link.relay}</strong>
            {link.state && <span>{tr(link.state)}</span>}
          </p>
        )}
        {mobileAccessBlocked && (
          <p role="alert">
            {tr("当前电脑处于免密码本机工作区或未开启「记住登录」，手机连接后会无法访问。请在「设置 → 账户」设置账户密码或开启「记住登录」。")}
          </p>
        )}
        {paired ? (
          null
        ) : (
          null
        )}
        {paired && power?.active && <p className="note">{tr("主机保持唤醒中")}</p>}
        {paired && power?.error && <p role="alert">{tr("主机防休眠未生效：")}{tr(power.error)}</p>}
        {code && (
          <div className="remote-pair-code">
            <img
              src={code.image}
              alt={tr("手机配对二维码")}
              width="320"
              height="320"
            />
          </div>
        )}
        {!code && !paired && (
          <div className="remote-pair-placeholder" aria-hidden="true">
            <QrCode size={64} strokeWidth={1.25} />
          </div>
        )}
        <div className="remote-pair-actions">
          {paired ? (
            <button type="button" disabled={busy} onClick={remove}>
              {tr("移除配对手机")}
            </button>
          ) : (
            <button
              type="button"
              className="primary"
              disabled={busy || relay !== savedRelay || (custom && !relay) || (!custom && !officialRelay)}
              onClick={generate}
            >
              <QrCode size={18} />
              {busy ? tr("正在生成…") : code ? tr("刷新二维码") : tr("生成配对二维码")}
            </button>
          )}
          {code && (
            <button
              type="button"
              onClick={async () => {
                try {
                  await writeClipboard(code.manual);
                  setNotice("一次性配对码已复制，可在手机上粘贴。");
                } catch {
                  setNotice("复制失败，请使用二维码。");
                }
              }}
            >
              {tr("复制配对码")}
            </button>
          )}
        </div>
        {!paired && (
          null
        )}
        {notice && <p role="status">{localizeBackendMessage(notice, tr)}</p>}
      </section>
      <section className="remote-relay-settings" aria-label={tr("中继设置")}>
        <div className="remote-relay-row">
          <div>
            <h4>{tr("中继服务")}</h4>
          </div>
          <div
            className="remote-relay-options"
            role="group"
            aria-label={tr("中继服务")}
          >
            <button
              type="button"
              aria-pressed={!custom}
              onClick={() => {
                setCustom(false);
                setToken("");
                onChange("");
                setCode(null);
              }}
            >
              {tr("Cage 官方中继")}
            </button>
            <button
              type="button"
              aria-pressed={custom}
              onClick={() => setCustom(true)}
            >
              {tr("自建中继")}
            </button>
          </div>
        </div>
        {custom && (
          <>
            <label>
              {tr("自建中继地址")}
              <input
                value={relay}
                placeholder="https://relay.example.com"
                onChange={(e) => {
                  onChange(e.target.value);
                  setToken("");
                  setCode(null);
                }}
              />
            </label>
            <label>
              {tr("管理令牌（可选）")}
              <input
                type="password"
                autoComplete="off"
                value={token}
                onChange={(e) => setToken(e.target.value)}
              />
            </label>
          </>
        )}
        {relay !== savedRelay && (
          null
        )}
      </section>
    </section>
  );
}
