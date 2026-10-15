# 前端 lane：番剧工作台重设计 / 2026-10-15

## 版本、所有权与集成

起点严格为 `f19f1d275c1f755aca8c4024243fe089fb810208`（`codex/nas-frontend-workflow-20261013`），已从 origin fetch 并核对。环境初始 `work` 分支为旧初始化提交 `ba619cf`，工作区干净，无需覆盖或保管用户修改。实现分支 `codex/nas-ui-redesign-20261015`，首个实现检查点 `8a74b2aaaad751e2f0885b141224b568b574dd01`。最终交付 SHA 由提交后的回复绑定。

本 lane 独占 `internal/nas/web/{index.html,app.js,style.css}`、UI 浏览器测试及本文。没有修改后端、数据库、不变量、Makefile、共享工作流、依赖、顶层 README、共享 STATUS 或 asuka-agent。AGENTS.md 要求的状态/契约说明在本 lane 文档提供，由 parent 最终整合到共享 STATUS。仓库和 `/workspace/.agents` 未发现其他可读 skill 指引。

NAS、Caddy、网络/安全设置和真实上游均未触碰。截图中的数据仅为 bounded fixtures。CI、浏览器 fixture、离线包装均不是实际部署。

## 设计与架构

使用暖灰纸面、朱砂主操作色、深蓝导航；原创静态 SVG 日落山景提供番剧收藏氛围。无外部图片/字体、前端包、额外 HTTP 资源、画布、持续动画、视差或背景模糊；未新增计时轮询。原命名建议 requested 状态的 5 秒读取机制保留。装饰隐藏于辅助技术，业务状态不使用装饰性在线标记。

继续使用原生 ES module。`Management.ServeHTTP` 基线只接受 `/`、`/app.js`、`/style.css`，跨文件拆分模块需要后端静态路由变更，故此次在单文件内分层，避免无理由引入框架或改变共享路由：

- 共享安全 DOM / UI：`el`、`panel`、`sectionLead`、`loadingState`、`table`/`row`、`field`/`select`、`badge`。
- 页面路由和读取：原 hash 路由保留查询、筛选、分页、任务和详情分区；读取所有权继续受 `generation` 控制。
- 写入与审核：原 `mutate`、单写门、`ReviewedRequestGate`、`BatchEditor` 和命名/规则审核门保持独立，不用表现组件推导授权。
- CSS 单一 `:root` 定义色彩、语义状态、间距、圆角、焦点；页面、任务卡和专业审核组件复用这些变量。

后续如需物理拆分，可先由后端 owner 将严格静态白名单扩展为列明的同源 JS 模块，再将纯 DOM 组件与只读页面抽取；不先迁移框架、不改变请求 DTO 或审核门。

## 已实现的体验变化

| 工作流 | 变化及真实语义 |
| --- | --- |
| 总览 | 五个 API 计数入口；保留范围/截断/分类重叠说明。说明性流程图明确不是任务进度；就绪数只来自 API。 |
| 任务列表 | 搜索/阶段/分页集中，命名/恢复入口明确；仅为对应已完成状态标绿，不因媒体就绪将所有状态染绿。 |
| 返回/历史 | 查询参数及当前页仍保留；从详情返回，将键盘焦点还给选中的任务按钮；下一页落点为结果计数。 |
| 任务详情 | 状态摘要、分区按钮、来源证据、文件方案、对比审计保持独立。未提供数据的分区仍禁用。 |
| 订阅 | 同一 API/CAS 与双击门；编辑后聚焦名称，新建后聚焦 ID。URL/凭据不进入表单。 |
| 映射 | 媒体身份、相对集范围、总集→相对集分为语义 fieldset。原输入名和 payload 不变，预览/保存/启用仍为独立步骤。 |
| 人工批量 / 命名辅助 | 保留全量文件/旁车对比、排序确认、独立人工入口、建议原始依据与不确定项、人工终审。统一表格和移动卡片。 |
| 规则审核 | 原始人工记录→可编辑新版本→有限试算→终审的门禁完整保留；移动卡片仍提供字段标题。 |
| 事件 / 接入 | 事件不等于 Discord 送达；配置不等于连接；数据库可读只用对应标签呈现，未探测继续显示未知。 |
| 页面状态 | 本页加载、读取时间、读取失败、离开期间可能陈旧明确可见。回到页面不自动刷新、丢草稿或触发上游。 |

## 可访问性与性能

- 跳到正文链接只聚焦 heading，不覆盖业务 hash 路由。
- 导航更新文档标题与 `aria-current`；表单有 label，表格有 thead 和 scope，移动卡片保留列标题的辅助技术可见性。
- 键盘焦点 3px 明确轮廓；全局错误聚焦可见 alert，加载和读取状态使用 live region；状态不只靠颜色。批量表编辑时清除上一轮字段错误，仍需重新完整预览。
- 主要文字配色对比度核算：次要文字 5.13:1，主按钮 5.55:1，导航 9.06:1，当前导航 6.41:1，警示 5.79:1，成功状态 5.42:1。此为核心色对计算，不宣称完整 WCAG 认证。
- 桌面侧栏、平板响应布局、移动横向导航；表格在窄屏按字段卡片阅读，不依赖整个页面横向滚动。
- 唯一页面入场动画 240ms、执行一次；`prefers-reduced-motion: reduce` 移除全部动画/过渡。无持续运行 JS 视觉效果。
- 只读页面不发额外探测。资源数量仍为 3，不新增依赖或静态资源请求。

## 验证记录（持续补充）

本地 Go 1.26.9 工具链可运行，但 SQLite/libc 模块下载被代理拒绝；`make verify` 在 `go vet` 的依赖读取阶段失败。没有将该失败计为通过。既有 GitHub CI 执行真实 Go 后端与 Chromium fixture 门。

已在本地真实 Chromium 通过：

- `python3 -B tests/proposal_layout_browser.py`：模拟 DTO；桌面/390px、完整试算/覆盖分页、范围二次确认、静默更改、取消、旧响应、确切批准 payload。
- `python3 -B tests/naming_assistance_layout_browser.py`：模拟 DTO；请求/建议/编辑/双击、静默变更、取消/reset/切页、文本注入防护、键盘、390px、规则草稿。
- `node --check internal/nas/web/app.js`、`python3 -m py_compile tests/frontend_workflow_browser.py`、`git diff --check`。

生产 API fixture 验证由现有 `TestFrontendWorkflowBrowserLoop` 启动真实 `Management` / SQLite 和 fake qB/Emby；浏览器脚本新增 skip link/标题、焦点、读取/失败/陈旧反馈、reduced motion、320/390/768/1024 排版断言。没有将模拟 UI 服务器当作生产后端。

截图沿用 `/tmp/asuka-workflow-*.png` 与 `/tmp/asuka-naming-*.png`，由既有 CI artifact 按提交 SHA 归档；生产总览/任务桌面与移动四图另有既有日志 SHA256/base64 证据入口。新增生产截图包含加载、减少动态效果、订阅、映射、移动事件等状态。

## 已实现的认证 UI 与契约边界

按 feature lane 已冻结提交 `6b18a6ecf9ef6914b29d0fd1e555bd8e4b650853` 的 `docs/MANAGEMENT_ACCESS_CONTRACT.md` 实现；只读 fetch 该分支，没有 merge/cherry-pick 后端。

- 首先 GET session；旧 `{csrf}` DTO 与 `local_single_user` 兼容，不出现无效 login/logout 控件。
- `authenticated_https` 未登录显示独立登录界面；只发送 `{username,password}` JSON，无 Authorization、无 login CSRF nonce。登录后 session nonce 只在 JS 内存。
- 401 `authentication_required` 统一递增页面/会话代数、取消在途请求、清空工作区 DOM 和旧 nonce。所有业务请求暂停，旧会话的迟到成功/401 无权改变新会话。
- 只对明确注册的批量、人工命名事实、规则草稿、订阅、单文件及局部候选目录保留内存输入；不保存 DOM、密码、nonce、preview digest、服务器版本或批准/排序/范围声明勾选到存储。
- 登录成功重新 GET 当前业务事实。批量按完整 source 身份恢复行序/集号；manifest 或辅助通道变化时只在只读区保留草稿，不按行索引套入其他文件。订阅编辑从最新列表重取版本和身份；草稿不是自动写入。
- 旧预览、确认/范围/排序勾选全部失效，必须重新核对和预览；绝不自动重发确认、批准、请求命名或其他 POST。已被服务器接受的先前请求仍应先读持久状态，不承诺撤销。
- logout 带当前 CSRF；成功或明确 401 后清空草稿并显示登录。网络失败不谎称已退出。忙碌写操作期间禁用退出，避免误解正在保存的效果已取消。
- 密码尝试后立即清空；关闭/刷新页面不保留草稿；没有 localStorage/sessionStorage。认证失败不区分用户名存在与否。429 遵守 Retry-After、禁用提交，时间到后仍需用户主动登录，无自动登录循环。
- 健康页和侧栏根据 session 返回的模式显示身份边界；登录成功不表示上游连接成功。

新增独占文件 `tests/session_layout_browser.py`，由 `tests/frontend_workflow_browser.py` 在生产工作台测试之后调用，现有 Makefile/CI 不变。独立 stdout 标明 `MOCK SESSION CONTRACT ONLY`，截图带 `session-mock` 名称。该测试验证真实 Chromium + synthetic HTTP DTO 的 UI 行为，**不验证 TLS、认证服务、cookie 安全属性、密码 verifier 或真实 NAS**；这些仍归 feature lane / parent 最终整合验收。

本地已通过登录/限流/错误、过期确认不重放、fresh-version 预览、批量行序/人工事实恢复、manifest 变化只读保留、订阅当前 CAS、规则重预览门、刷新中的过期恢复、退出丢弃、迟到旧 401、桌面/390px/reduced-motion。截图 `/tmp/asuka-workflow-session-mock-{desktop,mobile,expired}.png` 自动归入既有 CI artifact。

## 已确认 CI 检查点

`59c505602190028fe17889ea3e22bfba955745a1`（认证 UI 接入之前）已通过完整 [CI 38686583766](https://github.com/okitaAsuka/asuka-nas/actions/runs/38686583766)：`make verify`、两组模拟布局门及七组实际 Management/SQLite 浏览器门全部成功。首个检查点的总览门发现刷新 CSS 图标进入可访问名称，已用明确 aria-label 修复，原精确名称断言保持。

最终认证 UI 候选的 SHA、完整 CI 和 screenshot artifact 由交付回复绑定。实际认证后端尚未并入本分支，因此 UI 接入真实 HTTPS 后端仍是明确的集成验收项。真实 NAS/GUI、Caddy、服务凭据与上游连接不在本 lane 验证范围。
