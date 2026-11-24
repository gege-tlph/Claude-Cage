import assert from "node:assert/strict";
import { readdirSync, readFileSync } from "node:fs";
import test from "node:test";
import { backendMessageKeys, localizeBackendMessage } from "./backend-message.mjs";
import { formatMessage } from "./message-catalog.mjs";
import { httpErrorMessage, nativeFailureMessage } from "./error-message.mjs";

const read = (path) => readFileSync(new URL(path, import.meta.url), "utf8");
const languages = JSON.parse(read("../../internal/uilocale/languages.json")).slice(1);

test("localizeBackendMessage preserves original Chinese in default locale", () => {
  const errLocalNode = "本机代理端口不能用作节点。请导入原客户端的具体节点，在 Cage lite 中按中转到最终出口的顺序重新配置并应用";
  assert.equal(localizeBackendMessage(errLocalNode, formatMessage), errLocalNode);
  const routeErr = `节点 "local-1080" 需要迁移：${errLocalNode}`;
  assert.equal(localizeBackendMessage(routeErr, formatMessage), routeErr);
});

test("localizeBackendMessage translates ErrLocalProxyNode and dynamic backend errors in every non-default language", () => {
  const errLocalNode = "本机代理端口不能用作节点。请导入原客户端的具体节点，在 Cage lite 中按中转到最终出口的顺序重新配置并应用";
  const samples = [
    errLocalNode,
    `节点 "local-socks" 需要迁移：${errLocalNode}`,
    `没有可导入的节点，已跳过 2 个；第 1 个节点 "socks1"：${errLocalNode}；第 2 个节点 "bad"：节点协议暂不支持`,
    `没有可导入的节点，已跳过 1 个；第 1 个节点 "hk"：Clash Verge Ninja 专用协议，无法导入，请改用标准 Mihomo 节点`,
    `没有可导入的节点，已跳过 1 个；第 1 个节点 "neo"：Clash Neo 专用协议，无法导入，请改用标准 Mihomo 节点`,
    `订阅更新移除或跳过了当前出口或中转节点 "HK-01"：节点未包含在更新中；原配置已保留，请先应用其他节点再更新`,
    "订阅服务器返回 HTTP 403",
    "环境存储目录已不存在：E:\CageData",
    "环境存储不可用，请重新连接原磁盘：E:\CageData",
    "测速失败：代理核心返回 HTTP 504",
    "测速失败：节点未载入核心；请先应用节点以载入当前列表",
    "节点字段 server 存在重复写法，请仅保留一种",
    "Hysteria2 字段 up 与 up_mbps 冲突",
    "订阅的第 2 个节点 DNS 端口无效",
    "有 3 份节点列表未能从本机缓存读取，这些节点不会导入。可先在原软件中更新配置，再重新检测。",
    "以下节点未导入：WireGuard 暂不支持（2 个）；协议 ssr 暂不支持（1 个）",
    "包含暂不支持的连接选项：proxycommand",
    "另一个操作仍在进行，请稍候：当前正在应用代理节点。请等待完成，或先停止当前操作。",
    "已取消：已被启动环境替换",
    "应用代理节点失败",
    "恢复联网失败：等待响应超时。请重新检测；仍失败时导出诊断包。",
    "恢复代理连接失败：程序提前结束（退出码 1），未返回可识别的原因。连接恢复后会自动重试，也可手动重新检测。",
    // failure_message.go, storage and the 2026-11-24 night upstream sync
    "启动环境失败：环境目录路径太长，虚拟机的本机套接字超出系统长度限制。请把配置目录或环境存储改到更短的路径（例如 D:\\Cage）后重试。",
    "传递文件失败：上传窗口已关闭。请重新点击应用内的上传按钮，再选择文件。",
    "传递文件失败：服务返回 HTTP 500，尚未取得可识别的具体原因。在设置中导出诊断包，并附上出错时正在执行的操作。",
    "代理核心当前绑定的出口网卡（Ethernet 2）已失效或不再是默认物理网卡（当前为 Wi-Fi）；请重启代理核心以重新绑定",
    "代理核心当前绑定的出口网卡或节点路由接口已变化；请重启代理核心以重新绑定",
    "设置已保存，但重启内置代理核心失败：代理核心启动超时，请查看私有代理日志",
    "候选代理核心启动失败：exit status 1",
    "宿主代理已退出但环境处于异常状态（Broken），无法确认安全停止；请查看 hostagent 日志",
    "环境存储不能放在 AppData 或临时目录中，请选择其他本地文件夹",
    "请选择独立空目录，不能与程序、配置、工作区、现有存储或备份重叠",
    "无法移走 E:\\CageData：环境正在运行或迁移中，请停止环境并等待当前操作完成",
    "环境存储待恢复，请在设置中恢复后继续。环境存储不可用，请重新连接原磁盘：E:\\CageData",
    "请先停止虚拟机 default，再迁移环境",
  ];

  for (const { id } of languages) {
    const catalog = JSON.parse(read(`./locales/${id}.json`));
    const localize = (source, ...values) => formatMessage(Object.hasOwn(catalog, source) ? catalog[source] : source, ...values);

    for (const key of backendMessageKeys) {
      assert.ok(typeof catalog[key] === "string" && catalog[key].trim(), `${id} missing backend key: ${key}`);
    }
    for (const sample of samples) {
      const translated = localizeBackendMessage(sample, localize);
      assert.notEqual(translated, sample, `${id} failed to translate: ${sample}`);
      if (id !== "zh-TW") {
        assert.doesNotMatch(translated, /\p{Script=Han}/u, `${id} leaked Han characters in: ${translated}`);
      }
    }

    // Verify HTTP text/plain and native error bridges also translate backend messages.
    const httpTranslated = httpErrorMessage("/proxy/import", 400, errLocalNode, "text/plain; charset=utf-8", "POST", localize);
    assert.notEqual(httpTranslated, errLocalNode, `${id} httpErrorMessage did not translate ErrLocalProxyNode`);
    if (id !== "zh-TW") {
      assert.doesNotMatch(httpTranslated, /\p{Script=Han}/u, `${id} httpErrorMessage leaked Han: ${httpTranslated}`);
    }
    const nativeTranslated = nativeFailureMessage("uploadHostFiles", "每个文件不能超过 2 GB", localize);
    assert.notEqual(nativeTranslated, "每个文件不能超过 2 GB", `${id} nativeFailureMessage did not translate`);
    if (id !== "zh-TW") {
      assert.doesNotMatch(nativeTranslated, /\p{Script=Han}/u, `${id} nativeFailureMessage leaked Han: ${nativeTranslated}`);
    }
  }
});

test("English ErrLocalProxyNode and daily refresh interval are translated", () => {
  const catalog = JSON.parse(read("./locales/en.json"));
  const localize = (source, ...values) => formatMessage(Object.hasOwn(catalog, source) ? catalog[source] : source, ...values);

  assert.equal(localize("每天"), "Daily");
  const errLocalNode = "本机代理端口不能用作节点。请导入原客户端的具体节点，在 Cage lite 中按中转到最终出口的顺序重新配置并应用";
  const enErr = localizeBackendMessage(errLocalNode, localize);
  assert.match(enErr, /^A local proxy port can't be used as a node/);
  assert.doesNotMatch(enErr, /\p{Script=Han}/u);
});

// Job messages and setup progress labels reach the UI as they are written in
// the Go backend. Each one, and each message the backend builds at run time,
// needs an English translation.
test("every job message and setup progress label from the backend is translated into English", () => {
  const catalog = JSON.parse(read("./locales/en.json"));
  const localize = (source, ...values) => formatMessage(Object.hasOwn(catalog, source) ? catalog[source] : source, ...values);
  const lit = String.raw`"((?:[^"\\n]|\.)*)"`;
  const patterns = [
    String.raw`\bsetMessage\(\s*` + lit + String.raw`\s*\)`,
    String.raw`\.message\(\s*` + lit + String.raw`\s*\)`,
    String.raw`SetupProgress\{[^}]*?Label:\s*` + lit + String.raw`\s*[,}]`,
    String.raw`\bLabel = (?:"[^"]*", )?` + lit + String.raw`[ \t]*$`,
    String.raw`\bstate\.Message = ` + lit + String.raw`[ \t]*$`,
  ].map((source) => new RegExp(source, "gm"));
  const root = new URL("../../internal/", import.meta.url);
  const files = readdirSync(root, { recursive: true }).filter((name) => name.endsWith(".go") && !name.endsWith("_test.go"));
  const messages = new Set();
  for (const name of files) {
    const text = readFileSync(new URL(name.replaceAll("\\", "/"), root), "utf8");
    for (const pattern of patterns) {
      for (const [, value] of text.matchAll(pattern)) {
        const message = JSON.parse(`"${value}"`);
        if (/\p{Script=Han}/u.test(message)) messages.add(message);
      }
    }
  }
  assert.ok(messages.size > 60, `found only ${messages.size} backend messages`);
  const untranslated = [...messages].filter((message) => /\p{Script=Han}/u.test(localizeBackendMessage(message, localize)));
  assert.deepEqual(untranslated, []);

  for (const [message, english] of [
    ["正在准备 Debian 13 镜像", "Preparing Debian 13 image"],
    ["正在准备 Debian 12 安装材料", "Preparing Debian 12 install materials"],
    ["正在下载 Debian 13 系统镜像", "Downloading Debian 13 system image"],
    ["正在校验 Debian 13 系统镜像", "Verifying Debian 13 system image"],
    ["正在迁移环境数据 · 1.5 / 20.0 GiB", "Moving environment data · 1.5 / 20.0 GiB"],
    ["正在把旧 Ubuntu 环境原样移到 state/guests-removed/ubuntu …", "Moving the old Ubuntu environment to state/guests-removed/ubuntu …"],
    ["独立配置 a1b2c3", "Separate profile a1b2c3"],
  ]) {
    assert.equal(localizeBackendMessage(message, localize), english);
  }
});
