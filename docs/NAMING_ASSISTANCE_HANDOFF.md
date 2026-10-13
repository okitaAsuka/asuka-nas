# 单任务命名辅助合同（冻结 v1，2026-10-13）

两仓基线：NAS e4bdde7e53ffc397580526e6b78314c52e8fc6cd；agent 5e2d572293ed0074bdc5280caffe10edf18eafb0。此文供独立前端接入，不修改 web 或部署。

## 人工管理 API

沿用本机 Host/Origin/CSRF 和单写门；所有人工写入口拒绝任何 Authorization 头（即使有 nonce）。

- POST `/api/naming-request`：`{job_id,job_version,hints:{directory,display_name,season},retry:false}`。hints 可空；是明确人工事实，不是模型猜测。只接受下载完成且待确认、未开始任务，最多32个文件/16个安全标题。规则已明确可解的任务拒绝辅助。重复同快照请求返回同项。每任务最多3份请求；重新申请必须 `retry:true` 且旧项已拒绝/过期/未解。不会自动反复建议。
- GET `/api/naming?job_id=...`：返回该任务历史 `{items:[{id,version,state,snapshot,result?,result_digest,preview?}]}`。状态为 requested/suggested/unresolved/rejected/approved；动态过期项展示 stale。正文只在此本机审核面出现，日志/事件只有ID与固定码。
- POST `/api/naming-preview`：`{id,version,organization?}`。默认把完整建议转为 batch；可传明确人工修改后的 BatchOrganization。保存最新成功 preview `{job_version,plan,digest}`；重新生成/修改会使旧 digest 失效。
- POST `/api/naming-confirm`：`{id,version,digest}`。只批准已保存且仍匹配任务、完整清单指纹、上下文、parser/rule epoch、结果和当前人工意图的 preview。返回 `{ok:true}`；ACK丢失可重复同请求。文件执行与Emby发现由既有 job worker恢复，做种独立。
- POST `/api/naming-reject`：`{id,version}`。人工拒绝后不会自动再请求模型。

错误码：naming_stale / naming_invalid / naming_not_found / naming_retry_required / naming_limit / naming_rules_available / naming_unresolved / naming_preview_required / naming_replay_conflict；文件冲突/指纹/版本沿用 batch 码。失败不执行文件。

## 服务协议

复用默认关闭的专用 learning 回环服务、独立服务 Bearer，新增 `/v1/naming/requests?after=N`、`/v1/naming/check`、`/v1/naming/results`。没有服务批准、任意任务查询、URL获取、文件执行或工具接口。

request 页：`{protocol:1,items:[{seq,snapshot}],next_cursor}`（最多8）。snapshot 包括 `{protocol,id,digest,job_id,job_version,context_version,parser_version,rule_epoch,manifest_digest,hints,contexts:[{subscription,title}],files:[{id,name,kind,season,episode}]}`。`digest`是将snapshot的digest设为空字符串后按列出的字段顺序编码JSON得到的SHA256；`manifest_digest`是NAS私有完整绑定清单的SHA256，不提供文件内容或源目录。文件id为不可逆标识；name只有basename，kind为video/subtitle/auxiliary。摘要额外绑定NAS私有源路径及全清单指纹，服务不见源目录、绝对路径、GUID、URL或凭据。未经人工请求的任务不会导出。

check 输入 `{protocol:1,id,digest}`，输出 `{protocol:1,ok:true}`；过期返回409 naming_stale。模型输出和结果提交同有限DTO：`{protocol:1,id,digest,status:"suggestion"|"unresolved",directory,display_name,files:[{file_id,season,episode,target,basis,uncertainties:[]}],explanation,uncertainties:[]}`。每个完整清单文件必须出现一次；辅助文件不生成目标，字幕继承同stem视频。NAS只信本地batch校验生成的目标。无法从人工 hints 或明确文件/标题证据确定季号时必须 unresolved，不猜特殊篇。

NAS先持久不可变结果，再ACK `{protocol:1,id,state}`。相同结果重放返回同ACK，包括人工批准/拒绝后；不同结果409 naming_replay_conflict。旧快照拒绝新提交。两仓重启不会重跑已持久结果；模型在途崩溃显示interrupted，必须显式人工重试且继续消耗累计预算。

## 共用与来源

NAS复用 batchPlan / reservePlan / recordComparison / 逐文件硬链接 / fake或真实Emby adapter；不引入另一文件执行器。Plan和不可变Comparison保留 assistance 引用，origin=`model_assisted_human`。任何在已收到模型结果的任务上通过普通 batch/manual 入口确认的记录同样带来源，拒绝结果也不能洗成纯人工证据；learning只接收origin=human。新学习规则仍须原有独立人工终审。

agent复用 naslearning SQLite owner、调用预算、显式retry、ModelClient与事件投影。独立 `nas_naming` 配置默认关闭，字段同 `nas_learning`，独立StorePath，Mode由gateway固定为naming。预算总调用1–1000、每项1–3次、超时1–60秒、输出256–4096 tokens（正文另有32768字节上限）；普通通知继续无模型。CLI `gateway nas-naming status|resume|retry ID`要求停止gateway，owner锁阻止双消费者；拒绝/提交成功不可重跑，NAS重新申请与agent失败重试都是显式行为。此版不接TVDB、搜索、真实模型凭据或部署。

## 前端冻结接入说明

1. 待确认任务提供“请求命名建议”，允许输入明确人工媒体目录/显示名/季号；不填时不会默认季号。请求按钮不是文件执行按钮。
2. 轮询本机 naming 列表展示 requested 状态；agent服务失联/未开启不影响下载和做种。agent错误详情在既有事件页或停机CLI，NAS保留requested，不伪称模型运行成功。
3. suggested 展示完整逐文件依据/不确定项与来源；unresolved保留待确认。支持以既有BatchOrganization编辑人工意图，再POST naming-preview。
4. 使用当前成功preview的digest终审，编辑任何字段或切换任务即清除本地确认态；保存/刷新不自动批准。收到409时保留草稿、重读任务/建议。
5. NamingReview version从1请求到2结果，批准/拒绝加1；preview不改变version，最新持久digest裁决。批准重复用原version和digest幂等。
6. 批量模型辅助任务统一走naming-preview/confirm；普通batch-confirm只有匹配同一已保存naming preview时可批准。旧单文件人工Propose/Confirm仍支持独立明确修订，但永久携带模型接触来源、排除learning。

做种停止等正交状态推进也会改变严格job_version，故旧请求会stale：需人显式retry申请当前快照，受每任务3份上限；不会静默复用旧版本或反复付费。适用于32文件以内、单一季号完整任务；特殊篇/不支持清单保留原待确认流程。NAS领域schema由7增至8，仅对此未部署开发库增表，不授权生产迁移。agent保护库schema仍为1；Mode加入identity隔离，已有learning身份不变。两队列不得共用StorePath，通知token不得复用。

可复用规则的明确人工出口（沿用已有API，不新增自动推广）：审核后可人工填写 `NamingRule`（订阅、别名、媒体身份/集序、目录、显示名、季号与明确episodes/absolute边界），调用 `POST /api/rule-preview {rule,title,source}` 试算，`POST /api/rules` 保存不可变草稿，最后 `POST /api/rule-state {id,revision,enabled:true,epoch}` 独立启用。以后范围内任务由规则优先自动处理。不能从单次模型方案静默推出未来范围；人工声明未来范围必须明确表达。这条路径与纯人工learning候选的proposal-preview/approve同时保留。
