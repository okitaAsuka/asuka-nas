# 单任务命名辅助 UI 候选 / 2026-10-13

## 版本与写入边界

基线 `3f0b9333be3b298bd9f48f51a7fd5b7bd5445b04`，分支 `codex/nas-naming-ui-20261013`；旧 UI 整合基线 `e4bdde7e53ffc397580526e6b78314c52e8fc6cd` 为祖先。精确候选 SHA 在最终交付回复，避免自身提交循环引用。

已读取 checkout 的 AGENTS.md、docs/DESIGN.md、docs/STATUS.md、NAMING_ASSISTANCE_HANDOFF/EVIDENCE 及 BatchEditor、ReviewedRequestGate、ProposalReview、规则预览/保存/启用流程。checkout 无 `.agents/skills`，`/workspace/.agents` 为空；采用 dot-task-routing / dot-toolbox 约定，未递归委派。

仅修改 `internal/nas/web/app.js`、`style.css`；新增两份独立 Python 浏览器测试、一份 `*_browser_test.go` 夹具及本文。未改业务 Go、旧测试、Makefile、CI、打包、README、STATUS、asuka-agent。未合并 main、创建 PR、部署、连接真实 NAS/qB/Emby/模型，未创建凭据或更改权限。

## 行为

- 下载完成任务详情显示 agent 辅助状态；人工目录/显示名/季号可空，季号无默认。requested 仅表示已保存请求、等待结果，明确 NAS 没有 agent 在线/调用进度信息；不显示模型运行中或百分比。
- 当前 requested 每5秒轮询本机命名列表，离开页面即停止；手动重读可用。结果/来源说明用安全 textContent；完整清单含视频、关联字幕、辅助文件，各列原名/目标、季集、依据和不确定项；历史结果可展开。
- suggested 和 unresolved 复用 BatchEditor。载入建议先让生产 naming-preview 返回带私有 source 绑定的完整 batch，再只填表；不猜 basename 映射、不在客户端解码 opaque ID、不把载入视为批准。用户重新完整预览并勾选明确批准后，才 POST naming-confirm 当前 digest。
- 编辑、静默 DOM 赋值、reset、取消、导航使本地审核态失效；确认发送已审核请求快照。双击由既有单写门拦截。409 命名/版本冲突重读任务与命名状态，保留草稿，关闭旧批准态。新结果到达也保留已填写事实和人工表。
- unresolved 明确列出缺依据，支持补充后显式 retry（每任务最多3份），也支持直接人工填表经 naming-preview/confirm 审核。服务无结果时保留普通人工 batch 流程。
- 拒绝必须明确勾选；不自动重新申请，也不恢复更早结果的批准入口。模型结果接触后的比较记录显示 `model_assisted_human`，说明不进入纯人工学习；界面和后端都保留模型来源。
- 仅 approved 的已保存完整 preview 提供“复用已确认事实为规则草稿”。带入已确认目录、显示名、季号和示例 source；不带入模型猜测的别名/未来范围。复用规则必须人工填写订阅来源、完整标题别名及有限集数范围才允许预览；沿用 rule-preview → 保存不可变草稿 → 单独启用，无静默规则写入。

## 已实际验证

| 验证 | 结果与适用范围 |
| --- | --- |
| `node --check internal/nas/web/app.js` | PASS，JS 语法 |
| 两份新增 Python 的 AST / 编译检查 | PASS，语法；不是业务运行 |
| `python3 -B tests/naming_assistance_layout_browser.py` | PASS，真实系统 Chromium + mock DTO/API，仅 UI/视觉/请求归属 |
| `python3 -B tests/proposal_layout_browser.py` | PASS，既有 mock 规则审核组件回归 |
| `python3 -B -m unittest discover -s tests -p 'test_preflight.py' -v` | PASS，既有5项 Python 预检安全测试 |
| `gofmt` 新浏览器 Go / 全仓格式检查 | PASS |
| `git diff --check` | PASS |

新 mock Chromium 覆盖：请求双击只有一写、requested 真实含义、收到结果保留人工草稿、完整4文件表、安全 HTML 文本、键盘载入与批准勾选、默认载入不批准、静默修改拦旧 confirm、延迟成功/失败在编辑/静默赋值/取消/reset/导航后丢弃、批准双击、已确认规则事实复用且不写规则、缺别名/有限范围不发 rule-preview、unresolved 补充 retry、显式拒绝后5.2秒不再发请求、拒绝后关闭批量批准。390px `document.documentElement.scrollWidth <= innerWidth` 通过。

已用 view_image 实际目检桌面与390px全页及审核区域截图：蓝色流程边界、事实表单、黄底不确定项、逐文件表和移动端卡片、折行路径、批准区均可读，未见横向溢出。截图是 **mock视觉证据**，不是 NAS 文件执行证据：

- `/tmp/asuka-naming-mock-requested.png`
- `/tmp/asuka-naming-mock-desktop.png` / `asuka-naming-mock-mobile.png`
- `/tmp/asuka-naming-mock-review-desktop.png` / `asuka-naming-mock-review-mobile.png`

脚本每次复验重生成这些截图。截图未混入生产静态资产。

## 真实业务入口与阻塞（未通过）

父整合 writer 可添加独立入口，**本分支没有改 Makefile**：

```sh
python3 -B tests/naming_assistance_layout_browser.py
NAS_BROWSER_TEST=1 go test -race -count=1 ./internal/nas -run '^TestNamingAssistanceBrowserLoop$' -v
```

Go 测试设计使用真实 Chromium、生产 Management 的 Host/Origin/CSRF 写门、生产 LearningHandler 服务鉴权/请求队列/结果接收、临时 SQLite、实际硬链接及 fake qB/Emby。fixture只提供受控有限模型结果与推进时钟；浏览器负责真实管理 API 请求/预览/批准，不把业务写路由替换成 mock。

场景：primary 两视频+同stem字幕+辅助文件（实际3硬链接）；unresolved补充后新请求；unresolved直接人工填写经 naming-preview；拒绝后不自动请求并 SQLite reopen；服务503时普通人工 batch。夹具断言当前旧 digest 被拒、预览不写媒体/计划/比较、批准保持模型来源、逐项 os.SameFile、全部 fakeEmby 发现才完成。浏览器还检查延迟生产 preview 成功/失败、静默编辑、取消/reset/导航、双击、安全文本、键盘和390px。

**这些生产业务测试本轮未运行至编译或浏览器阶段，不能称通过。** 默认 `/usr/bin/go` 为其它程序；已下载官方 Go1.26.9 linux/amd64 并核验 SHA256 `42d158b4d8f7b61ac0a830567c940a86098fb7aac52e467a5ebec03ef5cc2f8d`，实际 `go version go1.26.9 linux/amd64`。工具链在 `/workspace/nas-toolchain/go`，GOPATH/GOCACHE 均在该可写目录，无 go.mod 降级或替代依赖。

`go mod download` 的 modernc.org/sqlite v1.50.1 和 modernc.org/libc v1.72.3 从官方 proxy 的 storage.googleapis.com 下载均返回 **Forbidden**。`make verify` 通过格式检查后在 go vet 的依赖加载停止；上述独立 Go 浏览器命令同样 `[setup failed]`。未换代理/绕过限制、禁用 sumdb、伪造缓存或替换 SQLite。完整 vet/race/build、真实 API+SQLite 浏览器闭环、旧生产浏览器套件和最新后端联调都未验；父须在已授权且依赖可用环境实际运行。

## 后端交接

1. 本轮仅绑定已发布3f0b933管理合同，尚未收到后续 backendSHA。命名预览不以旧 snapshot.job_version 判断成功，留给服务端语义快照校验；请求仍按 API 发送当前任务版本。父须对新语义快照/默认关闭自动入队实现作最终联调。
2. 当前列表没有 agent 在线、calling/failed/interrupted、累计预算或自动/人工请求入口标识字段；UI不猜这些信息，配置 UI 本轮未实现。如要区分自动入口或展示真实调用状态，需后端提供明确 DTO。
3. **已拒绝或过期且曾收到结果的批量人工退路仍有后端限制**：BatchPlan 保留 assistance；batch-confirm 要匹配命名 preview，而 naming-preview 仅接受 suggested/unresolved。UI保留人工表并关闭该状态的批量批准，要求明确申请当前快照，不洗掉模型来源。unresolved 状态及 requested尚未接触模型时的人工退路已接入；拒绝/过期后的无新请求人工完整批准路径需后端 owner 决定并补合同。旧单文件独立人工修订仍由后端保留模型来源。

候选提交推送后停止写入。最终整合 Makefile、业务依赖复验、最新后端/包装合并和验收属于父任务。
