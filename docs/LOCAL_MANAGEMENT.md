# 本地中文管理界面

CLI 默认只校验配置。显式启动：

```sh
asuka-nas -serve -config /absolute/path/config.json
# 浏览器打开 http://127.0.0.1:8787
# 可选 -listen 127.0.0.1:其它端口 或 [::1]:端口
```

`-serve` 同时运行 RSS/job 循环；`-run` 仅运行 daemon，无管理端口。两者与 rules CLI 使用同一实例锁，锁先于可写 SQLite 打开/迁移。**默认模式**仅允许 IP 字面量回环地址，禁止 `0.0.0.0`、`::`、LAN IP、域名；它是无登录的本机单用户边界，不能通过反向代理或 Docker 映射暴露。已有授权SSH的同端口回环隧道仍可选，真实NAS/SSH未验。

**显式 HTTPS 模式**已实现：`management.enabled:true` 与 `-serve` 同时满足、部署者提供可信证书/私钥及独立口令 verifier 时，才绑定配置中的明确地址。不能同时传 `-listen`；不复用 event/learning bearer，不信任 forwarded headers，不接受明文 HTTP。完整配置、session/login/logout、cookie/CSRF、错误及 UI hooks 见 [MANAGEMENT_ACCESS_CONTRACT.md](MANAGEMENT_ACCESS_CONTRACT.md)，部署前提见 [PACKAGING.md](PACKAGING.md)。登录/退出UI已整合；401清空预览批准、在内存保留安全草稿，重新登录读取当前事实并重新预览，不自动重发POST。默认关闭的事件/learning服务仍只回环，不随管理模式扩大，见 NOTIFICATIONS.md。

## 操作

- **订阅**：首次从配置的 subscriptions 导入 ID 对应档案并启用。页面新增订阅只能引用已有档案；秘密 RSS URL 不返回浏览器。改 URL/授权来源需要改本地配置并重启。启停、显示名与版本持久化；已有资源的订阅禁止换档案。停用保留所有历史和任务，做种到期停止仍执行。重试抓取解除该来源的退避，下个轮询执行。
- **规则**：可从用户明确选择的媒体相对系列目录/NFO提取局部候选，再确认系列稳定 ID、集序、订阅/别名、明确季号及目录；需要时指定有限相对/总集范围。预览不写库；保存生成不可变草稿，启用是单独操作。已有版本载入后创建新 revision，不编辑历史。停用规则继续阻止已知身份退回猜测。一次方案确认不授予规则自动执行权限。
- **任务**：列表每页100条，按任务ID稳定游标；筛选只针对本页。详情显示来源证据（最多100条）、阶段、做种期限、计划、指纹、逐文件进度和 Emby 发现。RSS 标题以纯文本渲染。Emby 刷新成功不等于就绪。
- **预览/确认**：支持完整单视频人工预览，以及完整合集的批量人工表；关联字幕随视频冻结。确认绑定 job version、plan version、digest；指纹/路径/冲突在执行前再次验证。部分执行后的计划不能重新规划或移动历史文件。
- **重试**：只解除退避，不重置下载、文件、规则或计划。临时权限/Emby问题纠正后可安全恢复。已冻结证据冲突、legacy_evidence_unresolved、特殊篇/不支持清单仍需人工诊断；本版没有绕过安全门的“强制完成”。
- **事件/健康**：任务/来源分流游标，规则审计最近100条。规则列表最多500版本。认证事件通知、agent规则候选和命名辅助均已实现但真实外部服务未验；健康页分别显示支持、配置开启/关闭及未知运行状态，不从开关推断连接。TVDB/在线元数据搜索未实现。数据库健康不是 NAS 挂载或外部服务健康证明。

## API（本机 /api 与独立只读/有限结果回传 /v1）

所有响应 `no-store`，无 CORS。默认回环模式 GET `/api/session` 保留进程内 CSRF nonce，并增加 mode/authenticated。HTTPS 模式匿名 session 只返回未登录状态；登录后 csrf 独立绑定有限会话，其他 API 无会话即401。业务 POST 必须精确 Host、Origin、`Content-Type: application/json` 和当前 `X-CSRF-Nonce`。拒绝跨站 fetch；CSP 禁止内联脚本/框架嵌入。业务请求体64KiB、写操作最多1个在途；login/logout 为4KiB、登录另有有界节流。领域错误409、鉴权错误见管理合同，均不输出原始异常。退出/401/重启后不得自动重放业务 POST；已经批准的冻结意图仍独立恢复。

| 方法/路径 | 作用 |
|---|---|
| GET /api/session | 默认回环 nonce，或 HTTPS 模式认证状态/当前会话 nonce/绝对到期时间 |
| POST /api/login、/api/logout | 仅显式 HTTPS 模式；独立人类身份、会话建立/撤销，精确合同见上文链接 |
| GET /api/health | 数据库、能力支持/配置、未知运行状态；不做外部探测 |
| GET/POST /api/subscriptions | 来源引用列表；新增(version=0)或CAS编辑 |
| POST /api/source-retry | id+version，解除来源退避并审计 |
| GET /api/workflow | 有界只读状态计数及明确scope；不探测上游 |
| GET /api/jobs?after=id&filter=all&q=... | 按ID分页100；安全摘要/ID字面搜索与阶段筛选；scope最多覆盖前10000个任务，next为空则结束 |
| GET /api/jobs/id | 显式公开DTO，无magnet/URL/GUID/凭据 |
| POST /api/job-action | id+version+action；retry/replan/preview/confirm |
| GET/POST /api/rules | 查询版本/追加草稿 |
| POST /api/rule-preview | rule+title+source，确定性解析解释 |
| POST /api/library-candidates | directory，明确选择系列相对目录；只读局部证据/冲突/未知/digest |
| POST /api/candidate-rule-preview | directory+digest+rule+title+source，重扫校验后纯预览 |
| POST /api/candidate-rule | directory+digest+rule，重扫校验后追加人工草稿；不启用 |
| GET /api/epoch | 规则控制版本 |
| POST /api/rule-state | id+revision+enabled+epoch |
| GET /api/audit | 最近100条规则审计 |
| GET /api/events?after=n | 任务事件最多100条，按ID重放 |
| GET /api/source-events?after=n | 独立来源事件游标 |

`job-action.preview` 额外 source/target；confirm 额外 plan_version/digest。UI每次操作保存时禁用写按钮；导航和预览的响应都校验 generation，旧响应不能覆盖新页面。服务器版本校验提供第二层保护。

管理状态是领域 schema8 之外的 additive 控制扩展 v2（control_meta、control_profiles、managed_subscriptions、control_events）；独立迁移不重写历史 Plan。升级前备份整个关闭后的 SQLite；不要降级到不理解持久订阅启停的旧二进制。缺失的来源档案显示不可用并跳过抓取，已有任务仍按原计划执行。

## 验证

`make verify` 做 Go1.26.9 格式/vet/race/build。`make verify-ui` 需要 Python3.12、tests/requirements.txt 与 Chromium（CHROMIUM_PATH可指定；否则使用系统或Playwright浏览器），实际启动本地fake HTTP服务、临时SQLite/文件和Chromium。只验证本地产品链；不访问真实RSS/qB/Emby、不调用模型、不发送Discord。

浏览器测试覆盖订阅添加→RSS→qB→指纹确认→实际硬链接→Emby发现→事件；规则启用后下一集自动；SQLite重开后的合集部分入库恢复；重复点击、XSS、导航晚到响应、错误恢复、手机宽度。工作台及命名截图随 CI artifact 交付，具体范围与文件名见 FRONTEND_WORKFLOW_EVIDENCE.md。

## 定向修复：订阅完整性与预览归属

订阅 GET 现在支持 `?after=id`，每页100条，返回 `next` 与 `total`；界面可翻页，规则作用域选项遍历各页。调度使用独立的完整启用集合，与页面大小无关。当前配置档案保存在 control_profiles（只含ID），每次初始化事务性替换该集合；历史订阅、启停状态、resources 和 jobs 保留。档案移除后不抓取，仍能查看/停用历史项；恢复档案后按保存的启停状态处理。

初始化、创建、启用和修改档案引用统一限制“当前档案中可用且启用”最多100条；历史/禁用项不占此容量。历史总量最多10000条。超限返回 subscription_limit 或 subscription_history_limit，整笔事务回滚（含档案集合、版本及审计），不静默截断、不自动删证据。控制schema1在同一事务升级2；旧版程序拒绝未知控制schema，不建议降级。

规则预览绑定完整的规则、revision、RSS示例和源文件示例快照。手动输入/change/reset、程序化载入规则都使本地预览代数失效；导航由页面generation失效。响应除了代数还核对完整输入快照，无input事件的程序赋值也不能接受旧success/error。保存草稿只有在当前快照已有成功预览时可用，点击时再核对快照，并提交审核过的版本内容。启用仍是针对持久不可变revision的独立epoch校验；文件确认仍绑定后端plan version/digest，未改变执行授权。

## 已有目录候选与待映射恢复

任务 needs_confirmation 且未开始时可进入“补充可复用映射规则”，主动选择来源线索，仅带入订阅/标题和清单中的视频示例，不暴露URL/GUID。别名不自动生成。选相对系列目录→读取候选→核对逐项证据/冲突/未知→选入草稿→明确本次篇章季号、别名和集序→预览→保存草稿→单独启用。观察到的前几季不能推断下一篇章季号，也不会自动变成绝对集数范围。

候选限额、NFO字段和摘要契约见 [NAMING.md](NAMING.md)。候选扫描是有CSRF保护的POST，沿用单个在途槽；取消、输入更换和导航用AbortController及独立generation+完整目录快照丢弃旧success/error，即使传输忽略取消也不能覆盖。双击不提交两次。选择/扫描变化使已审核预览失效；预览和保存均重扫，对目录变化返回固定错误码，不保存部分结果。

| 状态/错误 | 实际可用操作 |
| --- | --- |
| 无计划待映射 / 未知篇章 | 补明确规则并启用，下次job循环自动重检；不需重新qB add |
| stale_plan，尚未开始 | 规则确认启用后回到任务“重新规划”；重试不改旧计划 |
| permission_denied / 临时上游失败 | 修正挂载/权限/服务后安全重试解除退避，恢复冻结计划 |
| candidate_stale / library_changed | 重新读取、选择、预览；草稿未保存 |
| library_limit | 选择更小系列目录，服务不返回部分候选 |
| library_cancelled | 已取消或用户态超时，可重新发起明确读取 |
| library_unsafe_path / library_unavailable | 核对配置根、相对路径、符号链接、权限；不跟随外部路径 |
| late_mapping_conflict / legacy_evidence_unresolved | 核对冻结来源/原始证据；本版无强制批准或历史迁移，详情不提供无效重试 |
| 特殊篇 / 不支持编号或清单 / 字幕不明 | 人工诊断完整来源清单；本版不支持强制入库，不循环点重试 |

所有阻塞都保留源文件、不覆盖目标；做种期限独立，不因为目录候选、规则修改或通知失败而延长。


## 完整任务批量整理与命名辅助（已整合）

本机`POST /api/batch-preview`仅返回全清单人工方案；`batch-confirm`绑定job/plan版本、digest与全清单指纹，在同一事务保存批准与不可变对比记录。逐行明确集号、有界offset提取、显式确认表序后连续编号；不推断现实季号。`GET /api/comparisons?id=...&after=...`按job分页查看原始意图及实际文件状态。未开始方案可修改，已开始只恢复原冻结方案。预览取消不生成样本，全部Emby确认才整包完成。原下载文件不rename/删除/覆盖，做种独立。

批量DTO见BATCH_FRONTEND_HANDOFF.md；规则学习候选已实现，见RULE_PROPOSALS_FRONTEND_HANDOFF.md。任何高置信度不能绕过人工终审。

命名辅助入口：GET /api/naming?job_id=...；POST /api/naming-request（job_id/job_version/hints/retry）、naming-preview（id/version/可选organization）、naming-confirm（id/version/digest）、naming-reject（id/version）。完整合同见NAMING_ASSISTANCE_HANDOFF.md。requested仅表示请求已保存，不证明agent在线/模型运行。

learning.automatic_naming默认false。显式开启后支持的待确认任务自动入队；界面区分automatic/human入口，缺目录、显示名或明确季号列unresolved。人可直接填BatchOrganization，经naming-preview/confirm批准，无需再次调用模型；也可明确补facts重试，最多3份请求。做种停止不使建议过期；若发生在preview后，重新完整preview即可复用结果。真正命名语义变化才stale。

拒绝、过期或离线后可直接普通batch-preview/confirm，不需要新模型请求。模型结果与拒绝状态不被人工fallback改写；所有辅助历史保留model_assisted_human来源并排除纯人工学习。已批准任务可显式复用已确认目录/名称/季号为规则草稿，别名/订阅/有限范围需人工核对，再独立预览/保存/启用。


## 工作台入口

默认日常总览；任务的搜索、阶段、游标和详情写入只读 URL fragment，刷新、浏览器历史及返回任务保留上下文。全部写操作仍走原CSRF/CAS和完整预览/批准路径。GET只读工作台最多投影10000任务，页面明确全量或有限范围；不能把第一页当全库。待审规则计数为持久pending版本，详情仍需重新核验。

接入页只显示档案数量、服务地址是否提供与未知连接状态；不回传配置URL/路径或秘密，不触发外部探测。RSS/服务URL在实际config.json，凭据通过既有环境变量注入。公开片段和部署步骤仅作手工说明，没有URL/凭据保存接口。完整证据见 [FRONTEND_WORKFLOW_EVIDENCE.md](FRONTEND_WORKFLOW_EVIDENCE.md)。
