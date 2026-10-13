# 本地中文管理界面

CLI 默认只校验配置。显式启动：

```sh
asuka-nas -serve -config /absolute/path/config.json
# 浏览器打开 http://127.0.0.1:8787
# 可选 -listen 127.0.0.1:其它端口 或 [::1]:端口
```

`-serve` 同时运行 RSS/job 循环；`-run` 仅运行 daemon，无管理端口。两者与 rules CLI 使用同一实例锁，锁先于可写 SQLite 打开/迁移。仅允许 IP 字面量回环地址，禁止 `0.0.0.0`、`::`、LAN IP、域名。纯本机单用户是明确的信任边界；管理面目前没有用户登录；NAS另有默认关闭的专用回环认证只读事件服务，凭据不复用管理nonce，见 NOTIFICATIONS.md。不要通过反向代理、SSH 隧道给其他人或 Docker 端口映射暴露管理面。网络部署需另行实现身份认证、TLS 和权限，不是“内网即安全”。

## 操作

- **订阅**：首次从配置的 subscriptions 导入 ID 对应档案并启用。页面新增订阅只能引用已有档案；秘密 RSS URL 不返回浏览器。改 URL/授权来源需要改本地配置并重启。启停、显示名与版本持久化；已有资源的订阅禁止换档案。停用保留所有历史和任务，做种到期停止仍执行。重试抓取解除该来源的退避，下个轮询执行。
- **规则**：可从用户明确选择的媒体相对系列目录/NFO提取局部候选，再确认系列稳定 ID、集序、订阅/别名、明确季号及目录；需要时指定有限相对/总集范围。预览不写库；保存生成不可变草稿，启用是单独操作。已有版本载入后创建新 revision，不编辑历史。停用规则继续阻止已知身份退回猜测。一次方案确认不授予规则自动执行权限。
- **任务**：列表每页100条，按任务ID稳定游标；筛选只针对本页。详情显示来源证据（最多100条）、阶段、做种期限、计划、指纹、逐文件进度和 Emby 发现。RSS 标题以纯文本渲染。Emby 刷新成功不等于就绪。
- **预览/确认**：只给完整单视频清单人工预览，字幕随视频冻结；合集使用明确有界规则。确认绑定 job version、plan version、digest；指纹/路径/冲突在执行前再次验证。部分执行后的计划不能重新规划或移动历史文件。
- **重试**：只解除退避，不重置下载、文件、规则或计划。临时权限/Emby问题纠正后可安全恢复。已冻结证据冲突、legacy_evidence_unresolved、特殊篇/不支持清单仍需人工诊断；本版没有绕过安全门的“强制完成”。
- **事件/健康**：任务/来源分流游标，规则审计最近100条。规则列表最多500版本。NAS认证只读事件与agent固定通知已实现但未真实连接；目录/NFO局部候选已实现，元数据与模型未接入；页面明确标识。数据库健康不是 NAS 挂载或外部服务健康证明。

## API（当前实现 /api，原 /v1 是未来 agent 契约）

所有响应 `no-store`，无 CORS。GET `/api/session` 返回进程内 CSRF nonce；POST 必须精确 Host、Origin、`Content-Type: application/json` 和 `X-CSRF-Nonce`。拒绝跨站 fetch；CSP 禁止内联脚本/框架嵌入。nonce 不持久化、不充当部署鉴权 token。请求体64KiB，写操作最多1个在途，多余429；领域错误409（固定码，原始异常不出站）。读列表有上限；写操作用旧版本拒绝过期操作。

| 方法/路径 | 作用 |
|---|---|
| GET /api/health | 数据库与未接入能力状态 |
| GET/POST /api/subscriptions | 来源引用列表；新增(version=0)或CAS编辑 |
| POST /api/source-retry | id+version，解除来源退避并审计 |
| GET /api/jobs?after=id | 每页100；next为空则结束 |
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

管理状态是领域 schema5 之外的 additive 控制扩展 v2（control_meta、control_profiles、managed_subscriptions、control_events）；独立迁移不重写历史 Plan。升级前备份整个关闭后的 SQLite；不要降级到不理解持久订阅启停的旧二进制。缺失的来源档案显示不可用并跳过抓取，已有任务仍按原计划执行。

## 验证

`make verify` 做 Go1.26.9 格式/vet/race/build。`make verify-ui` 需要 Python3.12、tests/requirements.txt 与 Chromium（CHROMIUM_PATH可指定；否则使用系统或Playwright浏览器），实际启动本地fake HTTP服务、临时SQLite/文件和Chromium。只验证本地产品链；不访问真实RSS/qB/Emby、不调用模型、不发送Discord。

浏览器测试覆盖订阅添加→RSS→qB→指纹确认→实际硬链接→Emby发现→事件；规则启用后下一集自动；SQLite重开后的合集部分入库恢复；重复点击、XSS、导航晚到响应、错误恢复、手机宽度。截图仅临时内部检查，不随Library源码包交付。

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
