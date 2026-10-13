# 单任务命名辅助合同（冻结 v1 增补，2026-10-13）

两仓基础基线：NAS e4bdde7e53ffc397580526e6b78314c52e8fc6cd；agent 5e2d572293ed0074bdc5280caffe10edf18eafb0。首版后端提交为NAS 3f0b9333be3b298bd9f48f51a7fd5b7bd5445b04、agent f55e9f777b1d51098bc77a3f6561b62f8c8c2de4；本增补覆盖首版的“严格job_version过期”和“仅人工入队”行为。此文供独立前端接入，不修改web/包装/部署。

## 本次精确 DTO 差异

管理请求及NamingResult形状保持不变；NamingSnapshot增加三个可选字段，protocol仍为1：

- `semantic_digest`：64位SHA256，绑定任务ID、完整清单指纹摘要、上下文/标题、parser/规则epoch、当时的人工方案摘要。排除job_version、hints、trigger和请求标识；hints和触发来源仍由完整`digest`绑定。自动去重不因补充方式或做种进展变化。
- `plan_digest`：快照采集时已有的未批准人工方案摘要；无方案则省略。改变人工计划、批准或开始执行都会使原语义快照无效。
- `trigger`：`human`或`automatic`；只表示快照怎样入队，绝不表示审批来源。

Plan另增加只由NAS设置的可选`naming_review:true`，标记专用naming-preview/confirm授权；普通batch-preview省略/false。前端无需回传它，两个批准接口的请求形状不变。AssistanceRef.result_digest为空仅表示尚无模型返回，不能当作模型输出证据。

`job_version`保留为采集时审计值，不能将其与当前job版本不同解释为建议stale。`NamingReview.state`是当前服务校验后的状态。旧保存快照未带新字段时仍支持无原人工计划的保守校验，不重写旧快照/结果。

## 触发、预算和失败边界

NAS新增显式配置 `learning.automatic_naming`（bool，默认false）。开启必须同时启用原learning回环服务；agent仍须单独启用默认关闭`nas_naming`并设置有界持久预算。本轮只在测试中opt-in，不修改真实配置或创建凭据。

NAS job循环先运行原下载/做种/规则/文件逻辑，规则优先。自动辅助只接收下载完成、needs_confirmation、没有人工计划/执行开始、属于支持命名问题的任务；全清单及隐私门禁仍检查。特殊篇、不支持格式、文件/目标安全失败、明确人工停用规则和冻结冲突不入队。每tick最多扫描/请求8项，持久扫描游标防止坏输入堵住后续任务；NAS请求总量最多10000、每任务最多3份请求。SQLite唯一索引保证每语义快照至多一份automatic请求。

同一逻辑清单/上下文/parser/规则/人工计划已经有任何请求时直接跳过，不重复哈希大文件或调用模型。下载文件只读仍是既有前提；外部改写会在模型check、结果提交、preview和执行指纹校验拒绝，重新采样由人显式retry。最新请求rejected或unresolved时，自动队列保持停止，连来源变化也不会自动重提；人补充facts或显式retry才可恢复。模型离线不参与做种或文件调度。

自动请求不填媒体目录、显示名或季号。无依据返回unresolved与逐文件不确定项；界面据此询问明确事实，不能填默认S01或猜元数据。人工可以直接编辑BatchOrganization并预览终审，复用原未解结果而无需第二次模型调用；也可用naming-request的hints和retry显式请求模型重新判断。

agent复用naslearning SQLite owner/总预算/attempts/ModelClient/事件；模型调用前落盘预算，结果落盘后再提交，NAS结果落盘后才ACK。重启calling变interrupted，需显式retry且不恢复额度；提交/ACK故障只重放同一结果。总调用1–1000，每项1–3次（每任务最多3请求，最多9次且仍受总额度），超时1–60秒，输出256–4096 tokens/32768字节。普通通知继续无模型。

## 人工管理 API

沿用本机Host/Origin/CSRF和单写门；所有人工写入口拒绝Authorization头，即使有nonce，服务身份不能终审。

- POST `/api/naming-request`：`{job_id,job_version,hints:{directory,display_name,season},retry:false}`。当前job_version只用于入口CAS；重复相同命名事实/hints返回同项，做种进展不增加请求额度。hints可空。不同facts/真正旧语义须显式retry；拒绝/未解可显式重试；活跃请求补充不同hints也允许retry，并原子将旧项标rejected、清除其preview、追加naming_superseded审计，防止旧无资料请求继续推理/批准。最多3份请求。
- GET `/api/naming?job_id=...`：`{items:[{id,version,state,snapshot,result?,result_digest,preview?}]}`。状态requested/suggested/unresolved/rejected/approved；真正命名语义变化展示stale。正文只在本机审核面，普通事件只有ID/固定码。
- POST `/api/naming-preview`：`{id,version,organization?}`。默认从完整建议生成batch；可传明确人工修改的BatchOrganization，包括在unresolved时补事实。验证原请求的语义和完整指纹，再以当前job版本生成并保存最新preview `{job_version,plan,digest}`。模型结果及来源保持不可变。
- POST `/api/naming-confirm`：`{id,version,digest}`。只批准已保存的当前preview，执行仍走原batch/逐文件worker；批准/预约/不可变人工对比/审计共用事务。批准ACK丢失可用原version/digest重复。
- POST `/api/naming-reject`：`{id,version}`。保留依据，禁止自动重提。

语义判定忽略seed/stop状态、attempts/退避、临时ErrorCode及其导致的job版本变化。来源上下文、完整清单内容/身份、parser/规则epoch、人工计划改变或批准/执行开始仍拒绝；提交结果及保存preview在SQLite事务内再次核对这些逻辑不变量。preview生成/批准用当前必要job版本CAS：若停止做种发生在preview之后，重新preview即可复用同一请求/结果，不需请求新快照或再次付费推理。

错误码沿用：naming_stale / naming_invalid / naming_not_found / naming_retry_required / naming_limit / naming_rules_available / naming_unresolved / naming_preview_required / naming_replay_conflict；文件/preview冲突、指纹及CAS沿用batch码。配置缺服务返回naming_automatic_service_required；自动队列内部失败只发布固定naming_automatic_unavailable，不泄漏原文。

## 服务协议与隐私

复用默认关闭learning专用回环端口和独立服务Bearer；新增GET `/v1/naming/requests?after=N`、POST `/v1/naming/check`、POST `/v1/naming/results`。没有服务批准、任意任务查询、URL获取、文件执行、脚本或工具接口。

request页：`{protocol:1,items:[{seq,snapshot}],next_cursor}`，最多8。snapshot字段为 `{semantic_digest?,plan_digest?,trigger?,protocol,id,digest,job_id,job_version,context_version,parser_version,rule_epoch,manifest_digest,hints,contexts:[{subscription,title}],files:[{id,name,kind,season,episode}]}`。name只有basename，kind为video/subtitle/auxiliary。私有完整源路径/指纹列表只在NAS；manifest_digest是此完整绑定列表的SHA256。服务不见源目录、绝对路径、GUID、秘密URL或凭据。

完整digest按两仓NamingSnapshot DTO字段顺序，将digest设为空字符串后编码JSON再SHA256；新三个可选字段位于protocol之前，空值omitempty。semantic_digest计算另将id/digest/semantic_digest/trigger清空，job_version置0，hints置空，不改其余命名事实。两端固定DTO有实际跨仓stub验证，前端无需重算摘要。

check输入 `{protocol:1,id,digest}`，输出 `{protocol:1,ok:true}`；过期或被替代409 naming_stale。模型输出/结果提交同有限DTO：`{protocol:1,id,digest,status:"suggestion"|"unresolved",directory,display_name,files:[{file_id,season,episode,target,basis,uncertainties:[]}],explanation,uncertainties:[]}`。完整清单恰好一次，辅助文件无目标，字幕继承同stem视频；目标由NAS算法和batch重新验证。目录/显示名只取人工hints，季号只用明确hints或SxxExx证据；否则unresolved，不猜特殊篇。

NAS先持久不可变结果，再ACK `{protocol:1,id,state}`。同结果重放返回原收据，包括批准/拒绝后；不同结果409 naming_replay_conflict。服务feed只包含人工授权或明确opt-in自动入队的隔离任务快照。

## 前端冻结接入与来源

1. 默认可人工“请求命名建议”；opt-in已启用时直接展示自动requested/未解/待审状态，无需逐任务点击启动。状态来自naming列表，不能用snapshot.job_version与job版本差异关闭审核。
2. 缺资料时显示逐文件不确定项，询问媒体目录、显示名和有依据的季号；不预填猜测。可编辑既有BatchOrganization，POST naming-preview。此路不用新请求/新模型调用。
3. 人工编辑任意字段或切换任务时清除本地确认态；只有当前成功preview digest可终审。收到stale_plan/stale_version时保留草稿并重新preview，避免用retry重新推理。真正naming_stale才需要明确的新快照。
4. 请求version从1到2结果，批准/拒绝加1；preview不改version，以最新持久digest裁决。原批准version/digest重放幂等。
5. 审核原建议用naming-preview/confirm。用户拒绝建议、建议过期或模型离线时，普通batch-preview/confirm是独立人工出口：以全新人工意图、当前任务/完整指纹/CAS授权，不依赖原建议状态、不需新模型请求。专用preview的naming_review=true与普通人工preview的false/省略不能混用。旧单文件人工Propose/Confirm仍支持独立修订。所有路径永久保留辅助历史来源，原建议正文/状态/审计不会被人工fallback改写。

NAS共用batchPlan/reservePlan/recordComparison/硬链接/Emby，不引入文件执行器。Plan/不可变Comparison保留assistance引用，origin=model_assisted_human，trigger不能把它变成human；有命名辅助请求历史的任务保守标model_assisted_human并排除纯人工learning；优先引用已有模型结果，无结果时引用原请求并明确空result_digest，不伪造模型正文。如何将人工修改模型建议用于学习留给父后续设计，本轮不扩展。

避免每集重复终审：沿用明确人工可复用NamingRule（订阅/别名/媒体身份/集序/目录/季号及有界episodes或absolute）；POST `/api/rule-preview {rule,title,source}` 试算，POST `/api/rules` 保存不可变草稿，POST `/api/rule-state {id,revision,enabled:true,epoch}` 独立启用，范围内未来任务优先自动匹配。不能从一次模型结果静默推出未来范围。纯人工learning候选的proposal-preview/approve也保留。

NAS仍是未部署领域schema8，仅增加自动扫描表/唯一索引，不改旧任务/人工证据；agent保护库schema1及原learning身份不变。nas_naming StorePath与learning分离，Mode由gateway固定naming；CLI `gateway nas-naming status|resume|retry ID`需停gateway并受owner锁保护。不接TVDB、搜索、真实模型凭据或部署；packaging/前端/最终CI由父整合。


## 最终成本预检增补

NAS automatic请求无可信目录/显示名来源，不能花一次模型调用判断已知必缺facts。agent在NAS check后、reserveModel前确定性生成完整合法unresolved，列缺目录/显示名及待核对季号；不进calling，不增加calls/attempts。人补齐facts再明确请求新建议，或直接人工填表终审，不猜资料、不放松NAS校验。当前界面不把该预检结果假称模型输出；来源仍保守保留辅助历史，不能洗成pure human。

原12场景跨仓证据中automatic/unresolved的calls1现为calls0；其它完整facts场景仍calls1。最新agent相关race、全CI及两仓smoke由agent自身CI固定公开NAS SHA执行，历史12场景通过不直接代替成本变化复验。


跨仓CI权限说明：NAS仓的默认GitHub CI身份无法读agent仓（checkout返回Repository not found），没有扩权或创建token。改由agent自身CI读取公开NAS固定SHA，运行naslearning相关race与12场景smoke；NAS完整Go/UI门和独立package门保留。失败、权限阻塞或未运行项均不记为通过，最终结果由交付回复绑定。
