# 工作台前端验收 / 2026-10-13

基线 `4dce6f1e05bf342a066724d7548a9996f4959860`，独立功能分支 `codex/nas-frontend-workflow-20261013`；仅此仓库 writer，不递归委派、不 merge/PR/部署。

## 页面与范围

- 默认日常总览：下载中/等待下载、待命名/待补事实、待审规则版本、入库后 Emby 待发现、失败/恢复，点击进入准确阶段。分类允许重叠，不伪造下载比例、速度、在线状态。
- 任务列表：已有命名目标文件名 → 下载文件名 → 来源标题的纯文本摘要；明确季集显示 SxxExx，其余“季集未知”。摘要最长180字符，隐藏链接与常见凭据字段；不返回hash/magnet/GUID/指纹、完整清单或配置。
- 搜索与筛选：API端按安全摘要/任务ID进行字面匹配、按ID分页100条；更换搜索/阶段回到第一页。筛选、游标和详情进入只读URL fragment，刷新/浏览器历史和返回任务保留上下文。
- 任务详情：直达命名辅助/人工完整批量、来源证据、文件方案、对比与审计、安全恢复。复用 BatchEditor、ReviewedRequestGate 和原执行授权，不修改自动命名/学习协议或任何写入语义。
- 订阅/接入：现有来源引用选择、启停和重试依旧真实可用。RSS URL、qB/Emby地址、秘密环境变量由实际 config.json/运行环境管理；无虚假可保存表单。提供公开片段、GUI image.tar导入步骤、挂载/权限/真实发现检查清单。
- 已实现/配置已提供/连接未知分别表示；数据库可读不证明外部健康。配置概况只返回RSS档案数量与服务地址布尔值。

`GET /api/workflow` 与 `GET /api/jobs` 使用同一事务读投影，最多按ID读取10000个任务；同时返回 `scope.total/scanned/limit/truncated`。超过上限时 UI 明确总量、所覆盖的前10000任务以及搜索/筛选也限于此范围，不能称全库统计。待审规则数量为全库持久 pending revision 数；进入现有详情重新核验过期状态，工作台不写状态。超过10000任务的更广检索不在本里程碑。

## 可复验入口

`make verify`（真实 Go1.26.9），`make verify-ui`（全部既有规则、批量、命名和新增工作台门），`make package-test`，Offline package CI。

新增 API 测试：106任务跨页与字面搜索、不同阶段、只读不生成事件、非法筛选/超长参数、摘要凭据过滤；10001任务限额/诚实范围；真实成功人工样本生成的待审候选计数且版本/状态不变。

新增 `TestFrontendWorkflowBrowserLoop` 启动生产 Management + 临时SQLite和文件，qB/Emby只为回环fake。实际 Chromium 检查106任务精确计数、未知/危险HTML纯文本摘要、21个已入库未发现任务、搜索/筛选/页2选择、刷新、命名深入口/证据/审计/恢复/返回/浏览器历史、旧成功与旧错误、连续刷新、加载失败和会话失败恢复、订阅双击一次CAS、缺档案不可保存、配置/缺失/未知区分、外部请求为0。

本机真实 Go1.26.9 已取得，但官方模块的 SQLite/libc 下载 Forbidden；未绕过。系统自带 Chromium 可用：已有模拟DTO的命名与规则布局/交互门本机通过，它们**不能代替**生产API业务验收。生产完整验证及镜像包由已授权 GitHub CI 运行，最终结果与精确提交以交付回复绑定；失败/未运行不计通过。

## 截图

CI `nas-ui-<完整SHA>` artifact 归档 `/tmp/nas-ui.log`、既有命名截图和以下生产工作台截图（仅临时fixture，不含用户媒体/凭据）：

- `asuka-workflow-real-overview-desktop.png` / `asuka-workflow-real-overview-mobile.png`
- `asuka-workflow-real-tasks-desktop.png` / `asuka-workflow-real-tasks-mobile.png`
- `asuka-workflow-real-setup-desktop.png` / `asuka-workflow-real-setup-mobile.png`
- `asuka-workflow-real-empty-desktop.png` / `asuka-workflow-real-load-error.png`

桌面1440px与手机390px；截图须下载实际查看后才能完成视觉验收。日志以 `PASS WORKFLOW production Management + SQLite` 区分生产API证据和 `PASS MOCK ONLY`。保留期14天。

真实NAS、GUI启动、RSS/qB/Emby/模型/通知账号与上游兼容性仍未验。LAN登录、TVDB在线搜索、本页秘密/URL写接口未实现。Caddy、网络、鉴权、文件执行和凭据均未修改。
