# 2026-10-15 集成检查点

本分支是父任务授权的唯一集成写入分支 `codex/nas-integrated-20261015`，不合并 main、不部署。

## 固定输入与文件所有权

- 功能 lane：`a440497ace6da2328625f4fcc8e91b2dcb7264cb`，显式 HTTPS 管理、独立人类登录、会话/CSRF/有界节流与可信证书校验。此前 exact-SHA CI 和 image 包通过，见 FEATURE_CLOSURE_EVIDENCE.md。
- 架构 lane：`1dd1c33e2db67dc8346b2c7616b1da15a4bb4d2d`，共同 manifest 绑定、服务边界和测试 fixture 整理，见 ARCHITECTURE_20261015.md。已无冲突合并为 `04adcc12f41ed8c7367fe3730d6d34441595de09`。
- UI lane：仍等待父任务提供冻结 SHA；未合并中间版本，未修改其 assets 或浏览器测试。
- 本次集成拥有 README、STATUS、LOCAL_MANAGEMENT、PACKAGING、RUNNING、公开 package 模板/验证、Makefile/CI，以及独立 `management_https_browser_test.go` / `https_auth_browser.py`。没有数据库 schema、实际凭据、NAS/Caddy/服务变更。

## 当前新增验证

`make verify-auth` 只允许显式 `NAS_AUTH_DISPOSABLE_TRUST=1` 的一次性 Linux CI 用户。使用真实 Chromium、生产 TLS listener、真实 SQLite、临时媒体与回环 fake qB/Emby；不会发送真实标题/路径到上游。CA/私钥均来自测试临时目录，不进入镜像包。

浏览器先证明未受信 CA 被拒绝，再通过 `certutil` 临时加入唯一命名的测试 CA，使用正常证书验证完成测试，最后关闭浏览器并移除本次 CA；不改变 HOME，不替换信任数据库，不删除已有条目，不使用 `ignore_https_errors` 或证书忽略参数。该方法遵循 [Chromium Linux certificate management](https://chromium.googlesource.com/chromium/src/+/main/docs/linux/cert_management.md)。不得在保存的开发/NAS环境启用此一次性测试信任开关。

当前 harness 用真实同源浏览器 fetch 覆盖登录/退出 API，再操作现有批量整理 UI。覆盖匿名无任务/nonce、cookie 属性、会话失效确认 401、重新登录不重发确认、重新预览后人工确认、退出撤销会话、已批准意图独立执行、逐文件 Emby 发现、源保留硬链接与7天停止。登录/退出的最终表单交互须在冻结 UI 合并后补齐；API 与 mock 响应均未替代实际后端。

公开包仍是八文件白名单；management/events/learning 默认关闭，凭据模板仅为空 env 引用。新增打包门禁拒绝启用 management、加入私密 management 字段、填写 verifier/密码、未知或重复 env 引用。默认 Compose 去除显式 `-listen`，缺省仍回环；只有部署者明确启用 management 时使用其单一配置地址。证书、私钥、verifier/密码必须在包外提供；没有新增实际网络映射或 TLS 挂载。

## 验收边界

本地包边界测试 12 项、preflight 5 项通过；Go 格式检查与标准库 access 测试可在已校验 Go 1.26.9 运行。本环境完整 Go 依赖获取被下载策略阻止，未绕过；集成后的完整 race/browser/package 结论以本分支 exact-SHA GitHub Actions 为准。

等待冻结 UI：合并后核对静态资源白名单/CSP、匿名启动、登录/退出、401 清理、旧预览/写请求不自动重放、离线回环兼容，以及全量原有门禁。当前检查点不声称最终 UI 集成或真实 NAS 部署完成。

仍待后续迭代：运行时服务 URL/凭据管理（当前文件/env）、明确配置的元数据 adapter、通知中的可公开标题/链接策略、按订阅/任务做种策略、specials/混季批次。真实极空间型号后缀/固件、架构、GUI host/bridge/只读挂载、TLS 客户端信任、同设备硬链接和 qB/Emby 版本均未实机验收。已有 Caddy 保持不动，agent 仓库未改。
