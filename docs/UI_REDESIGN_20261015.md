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

## 会话接入协调

parent 已告知 feature lane 正实现 opt-in authenticated HTTPS management，独占 `management.go`、`config.go`、`cmd/asuka-nas/main.go` 和访问模块。UI 不修改这些文件。最终 `/api/session` / login / logout 精确 DTO 到达前不猜 schema 或显示伪登录控件。会话失效应锁定业务区域、保留内存中的非秘密工作，恢复后从新鲜服务状态重建表单；不保存密码，不自动重放后果性请求，旧预览和批准勾选失效。此项最终接入状态以交付回复及本文后续记录为准。
