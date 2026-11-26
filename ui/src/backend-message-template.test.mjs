import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";
import { localizeBackendMessage } from "./backend-message.mjs";
import { setMessages, translate } from "./message-catalog.mjs";

const english = JSON.parse(readFileSync(new URL("./locales/en.json", import.meta.url), "utf8"));

test("catalog keys with placeholders translate backend text built from them", () => {
  setMessages(english);
  try {
    // fmt.Errorf("传递文件失败：%w", err) with a known nested message.
    assert.equal(localizeBackendMessage("传递文件失败：上传窗口已关闭，请重新选择文件", translate),
      "Couldn't transfer files: The upload window closed. Choose the files again");
    // Values that are not Chinese are kept as they are.
    assert.equal(localizeBackendMessage("Windows IPv4 路由查询超时；Cage lite 外网保持关闭: context deadline exceeded", translate),
      "The Windows IPv4 route query timed out; Cage lite internet access stays closed: context deadline exceeded");
    // A whole-message template wins over the broader "设置已保存，…" rule.
    assert.equal(localizeBackendMessage("设置已保存，环境已启动。外网仍关闭，修复以下问题后可重新检测：检测会话已失效，请重新检测", translate),
      "Settings saved and the environment started. Internet access is still closed; fix the following, then check again: The check session expired. Check again");
    // The safety summary: title, state, evidence and next step are each translated.
    assert.equal(localizeBackendMessage("联网检查未通过：代理连接（未通过）。没有取得检测结果。请稍后重新检测。外网保持关闭。", translate),
      "Network check failed: Proxy connection (Failed). No check result. Check again later. Internet access stays closed.");
  } finally {
    setMessages({});
  }
});

test("known sentences joined at run time are translated one by one", () => {
  setMessages(english);
  try {
    assert.equal(localizeBackendMessage("请检查网络与代理，再重新检测。外网保持关闭。", translate),
      "Check the network and proxy, then check again. Internet access stays closed.");
    // A template that would leave a Chinese value yields to the known lead.
    assert.equal(localizeBackendMessage("操作失败：dial tcp 1.2.3.4: refused", translate), "Operation failed: dial tcp 1.2.3.4: refused");
    assert.equal(localizeBackendMessage("诊断包导出失败：disk full", translate), "Diagnostics export failed: disk full");
    // A reason and the help after it, the help a catalog entry with "；".
    assert.equal(localizeBackendMessage("程序提前结束（退出码 1），未返回可识别的原因。打开“操作详情”查看最后的错误；仍无法判断时，在设置中导出诊断包，并附上出错时正在执行的操作。", translate),
      "The program ended early (exit code 1) without a recognizable reason. Open the operation details to see the last error. If that doesn't explain it, export a diagnostics bundle in Settings and describe what you were doing.");
    assert.equal(localizeBackendMessage("Claude Desktop 的文件服务已经退出。请从 Cage lite 重新打开 Claude Desktop；若应用反复退出，请在设置中查看资源用量并导出诊断包。", translate),
      "Claude Desktop's file service has exited. Open Claude Desktop again from Cage lite. If it keeps exiting, check resource usage in Settings and export a diagnostics bundle.");
    // An unknown sentence keeps the whole message as it was.
    assert.equal(localizeBackendMessage("外网保持关闭。这是一句没有翻译的话。", translate), "外网保持关闭。这是一句没有翻译的话。");
  } finally {
    setMessages({});
  }
});

test("without an English catalog the Chinese text is kept", () => {
  setMessages({});
  const message = "传递文件失败：上传窗口已关闭，请重新选择文件";
  assert.equal(localizeBackendMessage(message, translate), message);
});
