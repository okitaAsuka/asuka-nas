# 当前接口与访问边界 / 2026-10-13

本机管理 `/api`、只读事件 `/v1/events`、有限学习/命名回传 `/v1/learning/*` 与 `/v1/naming/*` 均已实现。它们有不同监听与身份；没有 LAN 管理鉴权，没有远程文件执行/批准 API。以下是当前功能，早期仅内部库/未来 HTTP 的阶段契约不再描述当前状态。

## 本机管理

`-serve` 在显式 IP 回环监听，默认127.0.0.1:8787；同时启动daemon。管理面是本机单用户，没有登录身份系统。GET `/api/session` 返回进程内 CSRF nonce；POST 必须精确Host/Origin、application/json和X-CSRF-Nonce；没有CORS。服务Bearer不能请求/批准命名或规则建议。64KiB有界请求、单在途写门、CAS/完整快照与指纹由各领域接口再验证。秘密URL、GUID和凭据不返回UI；标题与建议文本安全渲染。

已实现路由：

| 路由 | 行为与合同 |
| --- | --- |
| GET /api/health | SQLite可读、能力supported/configured_enabled、runtime未知；开关不证明消费者/模型在线 |
| /api/subscriptions、source-retry | 可用来源引用、订阅启停/CAS及抓取重试；不向浏览器返回秘密URL |
| /api/jobs、jobs/id、job-action | 下载/入库/刷新/发现/做种独立状态、冻结方案与安全恢复 |
| /api/rules、rule-preview、rule-state、epoch、audit | 人工不可变版本规则；预览、保存草稿、单独启用 |
| /api/library-candidates、candidate-rule-preview、candidate-rule | 用户明确所选目录/NFO的有界局部候选，非外部元数据 |
| /api/batch-preview、batch-confirm、comparisons | 全任务人工表、完整清单指纹与摘要绑定、不可变人工意图和实际执行投影；[批量DTO](BATCH_FRONTEND_HANDOFF.md) |
| /api/proposals、proposal、proposal-samples、proposal-audit、proposal-revise、proposal-preview、proposal-approve、proposal-reject | agent规则候选及明确人工有限范围/历史试算/终审；[规则建议合同](RULE_PROPOSALS_FRONTEND_HANDOFF.md) |
| /api/naming、naming-request、naming-preview、naming-confirm、naming-reject | 单任务建议状态/明确facts/完整方案/人工终审与拒绝；[命名合同](NAMING_ASSISTANCE_HANDOFF.md) |
| /api/events、source-events | 任务与来源固定码分页游标，无标题/URL/异常正文 |

命名snapshot的semantic_digest绑定完整清单、上下文、parser/规则epoch与人工计划；job_version仅采集审计，做种等正交变化不废弃建议。preview以当前任务CAS重建完整方案，编辑或旧preview摘要不能批准。snapshot.trigger=human/automatic只表示入队来源，不是授权来源；Plan.naming_review由NAS区分建议审核与独立人工batch。拒绝/过期/离线可直接人工batch，不需要新模型请求；辅助历史仍保留model_assisted_human且不能进入纯人工学习。

## 专用只读事件服务

`events.enabled`默认false；独立回环监听默认127.0.0.1:8788。Authorization使用部署者已有环境变量值，不复用管理nonce；精确Host，拒绝Origin/Sec-Fetch-Site，无CORS。

`GET /v1/events?after=123` 固定最多100条：

```json
{"events":[{"id":124,"schema_version":1,"job_id":"opaque-job-id","kind":"discovered","plan_version":1,"file_index":0}],"next_cursor":124}
```

after必填非负int64；拒绝重复/未知参数，无limit；空页cursor保持。plan/file可省略。响应和事件无标题、URL、tracker、绝对源路径、凭据或原始异常。只读服务不能批准/重规划任务。配套gateway持久重放、固定Discord通知、故障恢复已实现，真实Discord外发仍未验；见[NOTIFICATIONS.md](NOTIFICATIONS.md)。

## 专用学习 / 命名服务

`learning.enabled`默认false；独立回环监听默认127.0.0.1:8789，使用与events分开的已有服务身份。只能读取有界学习样本/明确授权或opt-in命名快照、check当前依据、提交有限候选结果；服务身份永远不能人工终审或执行文件。

学习API见[RULE_PROPOSALS_FRONTEND_HANDOFF.md](RULE_PROPOSALS_FRONTEND_HANDOFF.md)。命名API为GET `/v1/naming/requests?after=N`（每页最多8），POST `/v1/naming/check` 与 `/v1/naming/results`。NAS先持久不可变结果再ACK；同结果重放返回收据，不同结果拒绝。result仅含有限目录/显示名/逐文件季集与依据/不确定项，没有脚本、工具或规则自动启用字段。

`learning.automatic_naming`默认false，开启必须同时启用learning服务；agent的独立nas_naming默认关闭、独立SQLite owner与有界持久预算。规则优先，支持的待确认任务每语义快照只自动入队一次；拒绝/未解不会自动再次请求，离线不影响做种。无明确目录/显示名/季号返回unresolved。NAS schema8/parser v4，agent保护schema1；仅未部署开发库迁移，不授权生产升级。

## 尚未实现 / 未验

没有 `/v1/jobs/*`、远程preview/confirmations/批准或通用agent工具接口；没有TVDB/在线元数据搜索；没有LAN身份鉴权/TLS入口。导入离线包不会使管理页可直接LAN访问。已有授权SSH同端口回环隧道只是一种访问候选，真实NAS/SSH/上游服务与模型均未部署验收；见[PACKAGING.md](PACKAGING.md)。
