# 归档区：死代码与失效测试清单

2026-11-24 大扫除的结果。Cage lite 持续接收上游（Lee-xeo/Claude-Cage）更新，所以**上游文件不移动、不删除**：挪走或删掉的文件在上游下次改动时都会变成 modify/delete 冲突。这里只登记位置、原因和核实方法，供以后修复、向上游提交删除，或等上游自己清理后对照。

fork 自己新增的文件逐个核过，都还在用，没有需要归档的。

核对基准：fork `5fa32e3b`（chore/cleanup 起点），上游 `origin/main` `66b9ddfd`，二者的合并基点 `c3fa57ba`。

## 1. Go 死代码

`deadcode -test`（golang.org/x/tools）分别以 `GOOS=windows`、`GOOS=linux` 跑，两边都不可达才算；再用 `staticcheck -checks U1000` 补未用的非导出变量和字段。darwin 需要 cgo，无法在这台机器上分析，所以另外 `git grep` 全仓（含 `_darwin.go`、Swift、shell、Python）确认没有调用方。"失去调用方"一列是 `git log -S` 找到的、删掉最后一个调用的提交。

| 位置 | 项 | 失去调用方 | 说明 |
|---|---|---|---|
| `internal/workbench/resources.go:221` | `(*Server).shrinkEnvironmentDisk` | `800a8b17`（#808） | #808 把缩小硬盘改成直接返回 `cage.DiskShrinkUnavailable`，整条缩小流程从此不可达。可能是上游有意保留、等以后重新开放 |
| `internal/cage/disk_resize.go:263` | `GuestInstallShrinkHookAndStageScript` | 同上 | 只被 `shrinkEnvironmentDisk` 调用 |
| `internal/cage/disk_resize.go:376` | `GuestRelocateGPTAndTrimScript` | 同上 | 同上 |
| `internal/clipboardfiles/archive.go:19,59,222` | `LinkCachedFiles`/`Save`/`Copy` 及其内部实现 `linkCachedFiles`/`save`/`copyFiles` | `845df7bc` | 原来由 `cmd/cage-guest-files` 调用，那个命令后来整个删掉了；`ownership.go:81` 的注释还提到它们 |
| `internal/cage/gate_probe.go:271` | `queryExitIPService` | `cc1fb590`（#251） | 调用方全部换成 `queryExitIPServiceTimed`，旧包装没删 |
| `internal/cage/storage.go:747` | `recoverStorage` | `484080df` | 调用方换成 `recoverStorageWithRuntime` |
| `internal/cage/storage_copy.go:18` | `copyStorage` | `845df7bc` | 调用方换成 `copyStorageExcluding` |
| `internal/workbench/host_file_upload.go:100` | `UploadHostFiles` | `78a7edf8` | 调用方换成 `UploadHostFilesForConnection` |
| `internal/managedproxy/fetch.go:22` | `BootstrapClient` | `3bc10e04`（#542） | 调用方换成 `BootstrapClientWith` |
| `internal/workbench/shared_clipboard.go:31` | 变量 `linuxClipboardCommand` | `558e4e78`（#198） | 注释说"保留给已有 Linux 客机"，但已没有任何代码读它 |
| `internal/workbench/projects.go:139` | 字段 `stderr *fileControlStderr` | — | 结构体里从未读写 |
| `internal/containerenv/config.go:132` | `(*Runtime).SSHTarget` | 从未调用 | `78a7edf8` 引入时就没有调用方 |
| `internal/containerenv/worker.go:521` | `(*Runtime).GuestCommand` | 从未调用 | 同上 |
| `internal/hostbridge/env.go:49` | `UserEnv` | 从未调用 | `d3bac10c` 引入；包内直接用 `userEnv()` |

## 2. UI 死代码

`ui/src` 里的导出逐个在 `ui/`、`mobile/` 全文搜索名字，没有任何其他文件引用的列在这里。ESLint（`npm run lint`）无告警。

| 位置 | 项 | 失去调用方 | 说明 |
|---|---|---|---|
| `ui/src/api.js:155` | `readClipboard` | `ec28fdb9` | `remote-input.js` 的粘贴不再经过它 |
| `ui/src/shared-clipboard.mjs:1` | `copyGuestText` | `4129aa21`（#531） | 只剩测试在用：`shared-clipboard.test.mjs` 前三个 "web copy …" 用例只测它，算死测试；同文件其余用例测的是在用的代码 |
| `ui/src/window-visibility.js:40` | `useWindowFocus` | 从未调用 | `3ccf8111` 引入时就没有调用方 |

## 3. 没有接入任何运行入口的测试

在 CI 工作流、`Makefile`、`ui/package.json`、Go 测试（`exec.Command` 调 node/python）和 PowerShell 脚本里都找不到调用的测试文件。被测代码都还在用，不是死代码，只是没人跑，会慢慢失效。

| 文件 | 本机结果 | 说明 |
|---|---|---|
| `internal/cage/browser-history.test.mjs` | 4/4 通过 | 能跑，只是没接进 CI |
| `internal/cage/browser-phone.test.mjs` | 3/3 通过 | 设 `CAGE_TEST_CHROMIUM`（Chrome 路径）才跑全部 |
| `ui/scripts/overlay-display.test.mjs` | 1/1 通过 | 要在仓库根目录运行，并设 `CAGE_BROWSER_TEST_PLAYWRIGHT`（playwright-core 的 `index.mjs`）和 `CAGE_BROWSER_TEST_CHROMIUM`，否则跳过 |
| `internal/cage/guest-audio-input.test.py` | Windows 上 1 错误 | 用到 `os.mkfifo`，只能在 Linux 客机跑 |
| `internal/cage/desktop-files.test.py` | Windows 上无法导入 | 用到 `socketserver.UnixStreamServer` |
| `internal/workbench/desktop_codex_test.py` | Windows 上 1 失败 | 测试改的是 `HOME`，Windows 上 `Path.home()` 读 `USERPROFILE` |
| `internal/workbench/host_tools_config_test.py` | Windows 上 3 失败 | 断言 POSIX 文件权限 0600/0640 |

`t.Skip`、`unittest.skipUnless`、`{ skip: … }` 全部带平台或环境变量条件，在 CI 或客机里会运行，没有无条件跳过的测试。

## 4. 冒烟脚本

`ui/scripts/` 下基于 `workbench-fixture.mjs`（模拟后端）的冒烟脚本，fork 有 100 个，其中 94 个上游也有，6 个是 fork 自己加的。在 fork 和上游 `origin/main` 各起一个 Vite，各跑一遍（3 个并发）；失败的再逐个单独重跑一次，两次都失败才算失败。两边都有的 94 个：

| 类别 | 数量 |
|---|---|
| 两边都通过 | 35 |
| 两边都失败，脚本已失效（上游就坏了） | 44 |
| 两边都失败，但不是脚本坏了 | 4 |
| 只在 fork 失败 | 10 |
| 只在上游新版失败 | 1 |

fork 自己加的 6 个：`guest-system-error-smoke`、`mobile-relay-consent-smoke`、`restart-pending-smoke`、`storage-reset-smoke` 通过；`layout-shift-scan`、`preview-workbench` 是工具，不是通过/失败型测试（见 4.3 末尾）。

另有 2 个（`remote-focus-handoff-smoke.mjs`、`terminal-copy-smoke.mjs`）在上游并发跑时失败、单独重跑通过，属于负载下不稳定。

### 4.1 上游就已失效（44 个）

在上游 `origin/main` 上同样失败，原因多是等的文字或控件已经不在界面上，或断言的布局已经变了。这些是上游的死测试，等上游修，或在合并上游后再决定改还是删。"卡在"一列是单独重跑时上游那边的第一处失败。

| 脚本 | 卡在 |
|---|---|
| `account-device-smoke.mjs` | `page.waitForFunction: Timeout 30000ms exceeded.` |
| `account-scheduling-smoke.mjs` | 等待 `getByText('调度设置 · 均衡分配', { exact: true })` |
| `account-smoke.mjs` | 等待 `getByRole('heading', { name: '账户', exact: true })` |
| `appearance-smoke.mjs` | 等待 `getByRole('alert').filter({ hasText: '外观设置未能保存' })` |
| `archive-position-smoke.mjs` | 等待 `getByRole('button', { name: '历史会话 17', exact: true })` |
| `browser-close-smoke.mjs` | 等待 `locator('.terminal-lane:not([hidden])')` |
| `browser-menu-smoke.mjs` | `AssertionError [ERR_ASSERTION]: menu stays compact and within the viewport` |
| `browser-metadata-smoke.mjs` | 等待 `getByText('已复制到本机', { exact: true })` |
| `builtin-theme-smoke.mjs` | 等待 `getByLabel('浅色主题状态配色预览', { exact: true }).getByText('成功', { exact: true })` |
| `claude-accounts-navigation-smoke.mjs` | 等待 `getByRole('button', { name: '设置', exact: true })` |
| `claude-accounts-smoke.mjs` | `AssertionError [ERR_ASSERTION]` |
| `cli-session-management-smoke.mjs` | 等待 `getByRole('status').filter({ hasText: '测试：原终端仍在运行' })` |
| `desktop-environments-smoke.mjs` | `TypeError: Cannot read properties of undefined (reading 'connection_id')` |
| `desktop-focus-smoke.mjs` | 等待 `getByRole('button', { name: '关闭应用', exact: true })` |
| `desktop-idle-smoke.mjs` | 等待 `getByRole('button', { name: '刷新 Claude Desktop 状态', exact: true })` |
| `desktop-memory-smoke.mjs` | 等待 `getByRole('alert').filter({ hasText: '无法读取 Claude Desktop 状态' })` |
| `environment-smoke.mjs` | `AssertionError [ERR_ASSERTION]（脚本第 18 行）` |
| `environment-storage-smoke.mjs` | 等待 `getByRole('region', { name: '本机环境存储' }).getByRole('button', { name: '选择文件夹' })` |
| `guest-system-icon-smoke.mjs` | 等待 `getByRole('complementary', { name: '项目与终端' }).getByRole('img', { name: '客户机系统：macOS' })` |
| `guest-system-picker-smoke.mjs` | 等待 `getByRole('heading', { name: /^(先启动 Cage 环境\|Claude Cage)$/ })` |
| `initialization-smoke.mjs` | 等待 `getByRole('region', { name: '环境安装进度' }).getByText('25 MiB / 100 MiB', { exact: true })` |
| `local-config-import-smoke.mjs` | `AssertionError [ERR_ASSERTION]（脚本第 46 行）` |
| `log-audit-smoke.mjs` | 等待 `getByText('Claude Desktop 的文件服务已经退出，请重新打开应用')` |
| `manual-region-smoke.mjs` | 等待 `getByRole('button', { name: '现在检测', exact: true }).first()` |
| `password-vault-smoke.mjs` | 等待 `getByRole('alert').filter({ hasText: '密码不正确' })` |
| `private-browser-paste-smoke.mjs` | `AssertionError [ERR_ASSERTION]（脚本第 61 行）` |
| `project-file-cache-smoke.mjs` | `AssertionError [ERR_ASSERTION]: macos shows the refresh button` |
| `proxy-chain-ui-smoke.mjs` | `AssertionError [ERR_ASSERTION]: floating dropdown must not move the following exit` |
| `proxy-exit-policy-smoke.mjs` | 等待 `element to be visible, enabled and editable` |
| `proxy-manual-ssh-smoke.mjs` | `AssertionError [ERR_ASSERTION]（脚本第 60 行）` |
| `proxy-yaml-drop-smoke.mjs` | 等待 `getByRole('button', { name: '本地 YAML', exact: true })` |
| `recovery-ui-smoke.mjs` | `AssertionError [ERR_ASSERTION]: The input did not match the regular expression /点击后将自动停止环境、安装并重启/. Input:` |
| `safety-checklist-smoke.mjs` | 等待 `element to be visible, enabled and editable` |
| `session-actions-smoke.mjs` | 等待 `getByRole('group', { name: '另一段会话 的操作' }).getByRole('button', { name: '归档', exact: true })` |
| `setup-progress-smoke.mjs` | 等待 `getByRole('region', { name: '环境安装进度' }).getByText(/虚拟机连接（VMConnect）窗口/)` |
| `sidebar-session-layout-smoke.mjs` | `AssertionError [ERR_ASSERTION]: session row too short: 这是一段用于检查窄侧栏省略显示的很长很长的会话标题` |
| `start-without-host-git-smoke.mjs` | 等待 `getByRole('button', { name: '进入工作台', exact: true })` |
| `storage-recovery-smoke.mjs` | 等待 `getByRole('button', { name: '恢复环境存储', exact: true })` |
| `tailscale-smoke.mjs` | `AssertionError [ERR_ASSERTION]（脚本第 36 行）` |
| `terminal-image-paste-smoke.mjs` | `page.waitForFunction: Timeout 30000ms exceeded.` |
| `terminal-newline-smoke.mjs` | `AssertionError [ERR_ASSERTION]（脚本第 42 行）` |
| `terminal-startup-smoke.mjs` | 等待 `getByText('正在准备 Claude 会话…', { exact: true })` |
| `theme-smoke.mjs` | 等待 `getByLabel('浅色主题背景', { exact: true })` |
| `workbench-smoke.mjs` | `page.waitForFunction: Timeout 30000ms exceeded.` |

### 4.2 两边都失败，但不是脚本坏了（4 个）

- `archive-contention-smoke.mjs`、`terminal-startup-live-smoke.mjs`：从标准输入读夹具，由 Go 测试驱动（见第 5 节）。
- `browser-chrome-smoke.mjs`：要先构建 `_output/bin/cage`。
- `browser-privacy-smoke.mjs`：要真实的浏览器控制器（`scripts/browser-privacy-fixture.mjs`），只能在客机里跑。

### 4.3 只在 fork 失败（10 个）

都是 fork 自己的改动引起的，在上游能通过。需要按 fork 现在的界面改脚本，或者修界面。

| 脚本 | 原因 |
|---|---|
| `browser-download-panel-smoke.mjs` | **界面 bug**：`34318f19`（界面只写名称和状态）把空列表的说明句换成了"暂无下载"，和上面的标题重复，空列表里显示两遍"暂无下载"（`ui/src/browser-downloads.jsx:355-356`）。应删掉那个 `<p>` |
| `icon-hints-smoke.mjs` | `aee31c91` 让图标提示只在键盘焦点（`:focus-visible`）时显示，脚本用 `element.focus()` 模拟键盘焦点，不算 `:focus-visible`。脚本应改用 Tab 键 |
| `about-settings-smoke.mjs` | 等"优先通过主机网络与系统代理"，fork 已删掉这句说明 |
| `browser-vertical-tabs-smoke.mjs` | 等"浏览器设置页跟随 Cage 当前的字体与配色。"，已删 |
| `exit-policy-region-smoke.mjs` | 等"中国大陆出口禁止放行；静态 IP 和不限地区也必须确认出口国家。"，已删 |
| `mobile-pair-settings-smoke.mjs` | 等"远程待机已请求主机保持运行……"，已删 |
| `remote-workbench-smoke.mjs` | 等"账户资料保存在当前云端主机"，已删 |
| `account-remember-smoke.mjs` | 改名后脚本等的标题"注册账户"与界面不一致 |
| `initialization-entry-wait-smoke.mjs` | 改名后脚本等的标题"Cage lite 已准备就绪"与界面不一致 |
| `initialization-polling-smoke.mjs` | 改名后脚本等的标题"为 Cage lite 准备独立环境"与界面不一致 |

fork 自带的两个工具不是通过/失败型测试：`layout-shift-scan.mjs` 要扫多页、中英文两档，超过本次每个脚本 180 秒的上限；`preview-workbench.mjs` 是预览服务器，固定连 5198 端口，不读 `CAGE_UI_URL`。

### 4.4 只在上游新版失败（1 个）

`resources-smoke.mjs`：在 fork 上通过，在上游 `66b9ddfd` 上等不到"已保存，下次启动环境时生效。"。下次合入上游时留意。

### 4.5 需要真实环境、本次没有运行

不用模拟后端，要构建产物、真实服务、客机或云端主机，按设计是手动验证工具：

- `ui/scripts/`：`account-recovery-live-smoke`、`account-remember-live-smoke`、`audio-installed-smoke`、`audio-smoke`、`browser-files-cdp-smoke`、`browser-settings-review`、`browser-settings-smoke`、`caps-lock-smoke`、`diagnostics-smoke`、`image-input-smoke`、`installation-preview`、`mobile-browser-latency-smoke`、`mobile-browser-smoke`、`mobile-cli-live-smoke`、`mobile-inline-select-smoke`、`mobile-transcript-smoke`、`packaged-startup-smoke`、`proxy-chain-smoke`、`proxy-smoke`、`remote-hosts-smoke`、`remote-input-smoke`、`shared-cli-live-smoke`、`ssh-config-import-smoke`、`subscription-import-smoke`。
- `scripts/`：`agent-cli-smoke`（macOS CI 会跑）、`agent-client-smoke`、`agent-native-browser-smoke`、`browser-*-smoke`（客机内）、`control-upgrade-smoke`、`favicon-extension-smoke`、`vault-controller-smoke`、`vault-service-smoke`，以及 `desktop-clipboard-smoke.py`、`desktop-ime-smoke.py`、`mobile-cli-supervisor-smoke.py`、`shared-clipboard-guest-smoke.py`（客机内）。

## 5. 查过但不算死代码

- `internal/managedproxy/store.go` 的 `WindowsSocket`/`legacyWindowsSocket`、`internal/host/stop_event_unix.go` 的 `RequestInstanceStop`：只在 Windows 用，Linux/macOS 上的报告是平台差异。
- `ui/src/backend-message.mjs` 的 `backendMessageKeys`：导出给 `backend-message.test.mjs` 核对英文翻译是否齐全，是测试接口。
- `desktop/windows/Authenticode.cs:88` 的 `UiContext`：Win32 `WINTRUST_DATA` 结构的字段，要保留以维持内存布局。
- `desktop/NativeInputTests.swift`、`scripts/` 下的维护者工具（`update-exit-country-list.mjs`、`control-upgrade-smoke.mjs`、`test-macos-install.mjs`、`deploy/cloud/pack-kit.sh` 等）：手动运行，需要 macOS、真实构建或云端主机，不是死代码。
- `ui/scripts/archive-contention-smoke.mjs`、`terminal-startup-live-smoke.mjs`：单独运行会因为没有标准输入而报错，实际由 `internal/workbench/session_metadata_test.go`、`cli_channel_test.go` 驱动。

## 6. 复查方法

```sh
# Go（在仓库根目录；缺 UI 构建产物时先放一个被忽略的占位文件）
mkdir -p internal/workbench/web && echo placeholder > internal/workbench/web/index.html
TAGS=yq_noxml,yq_nolua,yq_noini,yq_nohcl,yq_notoml,yq_noprops,yq_nocsv,yq_nouri,yq_nobase64,yq_nosh,yq_noshell
for os in windows linux; do GOOS=$os deadcode -test -tags $TAGS ./...; GOOS=$os staticcheck -checks U1000 -tags $TAGS ./...; done

# 某个函数何时失去最后一个调用方
git log -S'queryExitIPService(' --oneline -- internal

# 单个冒烟脚本（先在 ui/ 下 npm run dev 或 vite --port 5210）
cd ui
CAGE_PLAYWRIGHT_MODULE=<playwright-core 目录> CAGE_TEST_BROWSER_PATH=<Chrome 路径> \
CAGE_UI_URL=http://127.0.0.1:5210 node scripts/account-smoke.mjs
```

对比上游时，用 `git worktree add --detach <目录> origin/main` 另开一份，`npm ci` 后起第二个 Vite，用同样的命令跑上游那份脚本。
