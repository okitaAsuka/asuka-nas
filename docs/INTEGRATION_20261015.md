# 2026-10-15 集成检查点

本分支是父任务授权的唯一集成写入分支 `codex/nas-integrated-20261015`，不合并 main、不部署。

## 固定输入与文件所有权

- 功能 lane：`a440497ace6da2328625f4fcc8e91b2dcb7264cb`，显式 HTTPS 管理、独立人类登录、会话/CSRF/有界节流与可信证书校验。此前 [CI](https://github.com/okitaAsuka/asuka-nas/actions/runs/38687389326) / [image 包](https://github.com/okitaAsuka/asuka-nas/actions/runs/38687389336) 通过。
- 架构 lane：`1dd1c33e2db67dc8346b2c7616b1da15a4bb4d2d`，共同 manifest 绑定、服务边界和测试 fixture 整理，见 ARCHITECTURE_20261015.md。已无冲突合并为 `04adcc12f41ed8c7367fe3730d6d34441595de09`。
- UI lane：父任务确认 owner 已停止，冻结 `0f191d539a793c0c7e6cd3285827abca92407a6b`，无冲突合并为 `2437d56f35804f5726d7a9c8e15efa94a453b72f`。临时验证分支 `4cf76bf30988cea48b1fdda2ec6cfa2bb3a7edda` 仅选用 auth fixture 的流程，不整体合入。
- 本次集成拥有共享文档、公开 package 模板/验证、Makefile/CI 和唯一 `ui_authenticated_browser_test.go` / `authenticated_browser.py`。父任务另授权移动 hero 的有限调整与相应 UI 测试；没有数据库 schema、实际凭据、NAS/Caddy/服务变更。

## 当前新增验证

`make verify-auth` 只允许 `CI=true` 且显式 `NAS_AUTH_DISPOSABLE_TRUST=1` 的一次性 Linux CI 用户，normal CI 必须执行此门，不能以 mock 会话测试替代。使用真实 Chromium、生产 TLS listener、真实 SQLite、临时媒体与回环 fake qB/Emby；不会发送真实标题/路径到上游。CA/私钥均来自测试临时目录，不进入镜像包；NSS CLI由CI预装，脚本不下载工具。

浏览器先证明未受信 CA 被拒绝，再通过 `certutil` 临时加入唯一命名的测试 CA，使用正常证书验证完成测试，最后关闭浏览器并移除本次 CA；不改变 HOME，不替换信任数据库，不删除已有条目，不使用 `ignore_https_errors` 或证书忽略参数。该方法遵循 [Chromium Linux certificate management](https://chromium.googlesource.com/chromium/src/+/main/docs/linux/cert_management.md)。不得在保存的开发/NAS环境启用此一次性测试信任开关。

复用 UI owner 的实际表单登录/退出与失效恢复用例，删除中间 `https_auth_browser.py` 和其平行 Go harness；未再把 auth helper 接入 `TestFrontendWorkflowBrowserLoop`，避免同一fixture执行两次。现有 `make verify-auth` 单独调用唯一 fixture，原七组 `make verify-ui` 门保持。覆盖匿名无任务/nonce、错误与成功登录、cookie 属性、服务器使会话到期后的确认401、清空预览批准/保留两视频整包草稿、重新登录不重发、重新预览并人工确认、退出撤销。随后原冻结意图独立执行：两视频+字幕硬链接、一个视频发现不能整包完成、全视频发现后就绪、7天到期只stop且源保留。验证一份comparison/一次下载，执行前后intent/digest不变。

静态白名单仍为 `/`、`/app.js`、`/style.css`；UI无新增资源，严格CSP无需放宽。真实浏览器门另断言无CSP违规、无外部请求、无持久存储秘密。mock session脚本继续明确标注仅UI合同测试。

父视觉审查要求移动首屏优先待办：650px以下 hero 缩为短标题/操作与48px原创景物缩略图，隐藏重复宣传文案；保留DOM与键盘顺序、桌面设计和有限动画策略。390×844实际工作台与HTTPS登录后总览都断言待命名卡完整进入首屏；新增 `asuka-workflow-real-overview-mobile-first-screen.png` 及 `asuka-workflow-auth-real-overview-mobile-first-screen.png`，均随最终CI artifact归档。

公开包仍是八文件白名单；management/events/learning 默认关闭，凭据模板仅为空 env 引用。新增打包门禁拒绝启用 management、加入私密 management 字段、填写 verifier/密码、未知或重复 env 引用。默认 Compose 去除显式 `-listen`，缺省仍回环；只有部署者明确启用 management 时使用其单一配置地址。证书、私钥、verifier/密码必须在包外提供；没有新增实际网络映射或 TLS 挂载。

## 验收边界

前UI检查点 `b4dbf7a522fb3b133833a6dc48e3633143122b3a` 的 [CI](https://github.com/okitaAsuka/asuka-nas/actions/runs/38689313730) / [package](https://github.com/okitaAsuka/asuka-nas/actions/runs/38689313743) 通过：Go race CLI 5.154s、领域123.534s，首次真实可信TLS browser23.750s，七组原生产browser70.631s、两组mock布局；包白名单/checksum/network-none配置校验通过。它仅是前UI检查点，不能替代最终整合SHA。

本地包边界12项、preflight5项、标准库access race（22.062s）通过。本环境完整Go依赖获取被下载策略阻止，未绕过；最终重跑Go/真实认证/七组生产browser/独立包装的结果与artifact由交付回复绑定精确SHA。没有将UI mock或CI包装称为实际部署。

仍待后续迭代：运行时服务 URL/凭据管理（当前文件/env）、明确配置的元数据 adapter、通知中的可公开标题/链接策略、按订阅/任务做种策略、specials/混季批次。真实极空间型号后缀/固件、架构、GUI host/bridge/只读挂载、TLS 客户端信任、同设备硬链接和 qB/Emby 版本均未实机验收。已有 Caddy 保持不动，agent 仓库未改。
