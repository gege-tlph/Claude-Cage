# 贡献指南

Cage lite 是 [Claude Cage](https://github.com/Lee-xeo/Claude-Cage) 的独立分支，目前只发布 Windows 版，界面只维护简体中文和英文。

- **插件**：插件市场沿用上游的 [cage-plugins](https://github.com/Lee-xeo/cage-plugins)，请到那里提交，开发规范见 [docs/plugins.md](../docs/plugins.md)。
- **Cage lite 本体**：Fork 本仓库，从最新的 `main` 创建分支，然后提交 PR。改动较大时，可以先在 Issue 里选“方案贡献”说明方案。
- **上游也有的问题**：如果问题在上游 Claude Cage 里同样存在，也可以直接反馈给上游。

## 提交 PR

1. 从最新的 `main` 创建分支，分支名用 `fix/…`、`feat/…`、`chore/…` 或 `docs/…`。
2. 一个 PR 只做一件事，不要混入无关改动或本地临时文件。
3. 运行与改动相关的检查：
   - Go：`go test ./...`。Windows 上有一部分测试会因环境限制失败，请在 PR 里说明哪些失败与本次改动无关；
   - 界面：在 `ui/` 下运行 `npm test` 和 `npm run lint`；
   - 涉及安装、更新或打包时，在 Windows 上实际覆盖安装一次。
4. 按 [PR 模板](PULL_REQUEST_TEMPLATE.md) 写明用户可见的变化、验证结果和尚未验证的范围。
5. 新增的界面文字只写名称和状态，并同时提供简体中文和英文。

## 敏感信息

代码、配置、日志和截图里不要出现密码、令牌、订阅链接、出口 IP 或本机路径。
