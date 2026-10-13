# 当前状态 / 2026-10-13

当前行为以本节、最新合同与代码为准；后文是历史里程碑，不是当前未实现清单。NAS领域schema8、控制扩展v2、parser v4；均为未部署开发库迁移，不授权生产库升级。当前集成分支为 codex/nas-naming-ui-20261013，最终 SHA、CI 和包校验由交付回复绑定。

| 能力 | 当前状态 |
| --- | --- |
| RSS/.torrent → qB5.x 下载去重、到期仅stop | 已实现；fake HTTP，不删源；通知/模型不参与做种截止 |
| 人工命名规则、目录/NFO局部候选 | 已实现；明确订阅/别名/媒体身份/季号与有限范围，预览、保存草稿、单独启用 |
| 完整任务 batch 与不可变对比 | 已实现；逐文件预览/版本摘要绑定、实际硬链接、逐视频Emby发现、部分执行恢复 |
| agent归纳人工规则候选 | 已实现；持久样本/结果，人工有限范围试算和终审；范围内下一集自动映射 |
| 单任务命名建议 UI/API | 已实现；人工facts、完整逐文件依据与不确定项、编辑后naming-preview/confirm、人批/拒绝 |
| 默认关闭自动命名入队 | 已实现；learning.automatic_naming显式opt-in、规则优先、有界队列/预算、缺必需facts确定性unresolved且模型calls0、不自动重提拒绝/未解项 |
| 命名语义快照 | 已实现；做种等正交job版本变化不废弃建议；旧preview仍需当前CAS重预览，不重新付费 |
| 拒绝/过期/模型离线后的独立人工batch | 已实现；不需要新模型请求，保留辅助历史，排除纯人工学习 |
| 专用回环事件 / learning / naming服务 | 已实现且默认关闭；服务身份无批准权限；通知仅ID和固定码 |
| 健康页 | 区分支持、配置和未知运行状态；配置开启不证明消费者/模型/Discord connected |
| 标准 amd64 离线包 | 包装提交已整合；版本SHA/镜像ID/校验及公开模板白名单；CI构建配置门，真实NAS仍未验 |
| LAN管理登录/鉴权、TVDB/在线元数据 | 未实现；只回环。已有授权SSH隧道仅访问候选，不是已验部署 |
| 真实RSS/qB/Emby/模型/Discord/NAS/SSH、arm64 | 未部署验收；fake闭环与CI不能代替真实环境 |

集成来源：UI e3d3ef610f626b2de8298e1b682044cd48857652；NAS后端17acfcfe0849f762bfe16f3cc2eb29e20bc920bf只接3f0b933后增量；包装41be7726bb2b8900bf1ca77761773ef335215a1d保留e4bdde7后的全部五提交；agent原配套dd5b2bb06c17845406e7bce986146e1dd6030915后，按父授权小修目录/显示名预检与回归；新配套80834dcf664af482b8aeaf38351222bd5efe6c17。没有main merge/PR/部署或创建凭据。

验证入口：make verify（Go1.26.9格式/vet/race/build及Python预检），make verify-ui（保留原浏览器门并加入命名生产API测试），make package-test，make package-nas。本机SQLite/libc官方依赖下载Forbidden，本轮生产Go/浏览器由已授权CI执行；未执行或失败不能计为通过。最终运行结果见交付回复及[NAMING_ASSISTANCE_UI_EVIDENCE.md](NAMING_ASSISTANCE_UI_EVIDENCE.md)。CI归档生产浏览器日志/截图及绑定最终SHA的离线包。

当前接口见[LOCAL_MANAGEMENT.md](LOCAL_MANAGEMENT.md)、[API.md](API.md)；命名边界见[NAMING_ASSISTANCE_HANDOFF.md](NAMING_ASSISTANCE_HANDOFF.md)；后端最终12场景跨仓证据见[NAMING_ASSISTANCE_EVIDENCE.md](NAMING_ASSISTANCE_EVIDENCE.md)。包装及回环SSH候选见[PACKAGING.md](PACKAGING.md)。规则候选合同见[RULE_PROPOSALS_FRONTEND_HANDOFF.md](RULE_PROPOSALS_FRONTEND_HANDOFF.md)，既有父审证据见[RULE_REVIEW_UI_EVIDENCE.md](RULE_REVIEW_UI_EVIDENCE.md)。

## 历史阶段记录（不作为当前功能矩阵）

## 部署准备里程碑 / 2026-10-12

Library bundle v6 恢复并核对 ceef1f455412df8443d4465730cf5e45ade397f0，完整历史 fsck 通过，独立目录和新本地分支 codex/nas-deploy-prep-20261012。
定向回归已通过：100 历史订阅之后的 100 当前来源全部实际抓取；加强断言为每个来源恰好一次。实际 Chromium 验证载入/输入/静默赋值/reset/导航后旧 success/error 不能改新表单，保存绑定当前成功快照。

已有 Actions 继续复用 make verify/verify-ui；前移 Python setup 并将 5 组预检安全测试纳入 make verify。
新增 Python3 标准库只读 NAS 预检；实际 openat2、fd mount ID、当前 UID 权限、Docker 可见性等，分已证实/推断/未验。独立双参数 opt-in 探针仅写现有空隔离目录，不扫描媒体，不把同设备当硬链接成功。
标准 Docker 依赖请求仍在 proxy.golang.org:443 被拒；已有缓存经 go.sum 校验后用 Dockerfile.offline、依赖/编译阶段 network=none 完成 linux/amd64 构建，非 root 无网络只读容器配置校验通过。

最终完整验证结果在 DEPLOY_PREP.md 记录。真实 NAS、qBittorrent、Emby、Discord、arm64 和线上 CI 未验；未修改 asuka-agent、业务架构、认证或映射学习，未拓宽回环监听，未建远端/推送/合并/部署/接凭据。

## 提交阶段

- `21c5bdc`：独立 main，仅最小 README。
- `945cfa4`：设计、API 契约、Go 1.26.9、配置、SQLite、测试和 Actions。
- `c1c54c8`：fake HTTP/临时文件最小纵向闭环。
- `1addb55`：逐段复核与故障注入，修复持久化、取消、停止优先顺序、参数边界。
- `fc5d788`：RSS/.torrent HTTP 与持久轮询 daemon。
- 当前开发分支 `codex/nas-foundation-20261012`：安全单集 MVP 收尾，完成有界认证恢复、完整文件清单阻塞、公共挂载/空闲检查。

## 已实现

严格配置、安全日志；RSS magnet 和 .torrent enclosure 有界 HTTP 抓取；原始 infohash/bencode/路径校验；SQLite schema v2、完整 torrent blob、资源双重去重、job 状态/CAS/原子事件；真实 qB 5.x HTTP 登录/add/multipart/info/files/stop；真实 Linux openat2/linkat/fsync 入库和字幕；显式 SxxExx 预览与人工方案版本/摘要/指纹绑定；Emby HTTP refresh 与发现确认分离。

CLI 默认仅检查配置，显式 -run 启动 source/job 两条循环。来源和 job 的请求前预约、失败退避持久化；0–7 天做种按首次完成时间计时，截止检查绕过导入退避。flock 防重复实例；SIGINT/SIGTERM 取消 HTTP/哈希并等待循环退出。没有 HTTP listener、Discord 或模型调用。

## 最新验证

真实 Go 1.26.9 linux/amd64：`make verify` 的 gofmt、vet、40 个顶层测试（另含子测试/fuzz seeds）、race、build 全通过。
配置样例检查和无 CGO 构建通过；git diff --check 通过。
Bencode fuzz 两轮各 5 秒：52,364 与 56,171 次执行，无崩溃（最后一轮针对已加别名路径检查的解析器）。覆盖率仅作为辅助，不能推断生产安全；中途领域包覆盖率 77.2%，CLI 的 subprocess 行为覆盖不会计入父进程覆盖数字。

高价值证据：SQLite RAISE(ABORT) 在外部效果成功后的 events INSERT 中断，验证状态回滚和重启重放；并发目录/symlink 替换无外部写入；0/7 天/时钟回拨/待确认/路径不匹配/导入失败不删源；过期确认/指纹变化/目标冲突；Emby accepted≠discovered；实际子进程 SIGTERM 中断 60 秒 HTTP；token URL/远端错误不进入日志/事件；重复 feed 不重复下载 torrent；跨源/重定向/超时/大小拒绝；schema v1 数据保留和连接重建 PRAGMA；持久 torrent hash 被篡改时拒绝派发。

所有网络测试仅 httptest 回环服务器，文件仅 t.TempDir。未访问真实蜜柑、qB、Emby、NAS。Dockerfile 未做镜像构建/运行验证。

## 明确限制

- Mikan 格式由本地 RSS/enclosure fixtures 验证，尚无真实站点/具体部署兼容验收。
- qB 401/403 触发最多一次重新登录和一次原请求重放；全部认证尝试冷却 60 秒，500 等非认证错误不登录。凭据每次从配置引用取，不保存在客户端或日志。4.x 兼容后置。
- Emby 精确 Path 查询尚未做真实版本/分页验收；HTTP 200 从不等于已发现。
- 多视频 torrent 会解析/下载但不自动部分入库，进入待确认；整体多视频方案未实现。命名能力现见下文 N1；v2/webseed/复杂字幕不支持。
- Propose/Confirm 和事件游标为内部库 API，HTTP 管理接口/鉴权/UI/Discord 仍仅契约或未实现。
- 根目录必须由受信任宿主维护。不能防止有权限的进程搬走已打开父目录，不能隔离源文件恶意并发内容写入；不宣称多租户安全。
- 断电只验证 fsync/事务顺序与错误注入，未做真实断电实验。离线、系统时钟错误、长磁盘 I/O 不保证硬实时停止。大文件哈希可能延长下一批 job 轮询。
- schema v1→v2→v3 只用于此未部署项目，不代表已授权生产数据库迁移。

## 远端与交付

现有身份 okitaAsuka；先前目标查询 404，createRepository 返回 Resource not accessible by integration，开发分支推送 Repository not found，Actions 查询 HTTP 404。用户已获知并准备手动建仓/授权；本阶段未再重试建仓、推送或扩权，没有 token 创建。
无远端提交/CI 成功链接。独立本地仓库 `/workspace/asuka-nas` 和 `/workspace/asuka-nas-delivery.bundle` 保存所有阶段历史；main 未合并。asuka-agent 保持 clean，未修改。文件树、测试名、变更边界见 REVIEW.md。


## MVP 收尾（暂停扩功能）

认证：401/403、登录响应 Fails.、重新登录后仍拒绝、60 秒冷却分别可诊断；multipart 重放保留完全相同的字节。只有启动配置中的环境变量引用，未创建或回显凭据。
单集：下载器完整清单里视频数量必须恰好 1（识别更多视频扩展名防漏数），且自动入库只支持 mkv/mp4。合集/未知文件/未知季集/字幕映射不明整体 needs_confirmation，独立 block_reason 不会被做种认证错误覆盖；不选第一集。人工单文件方案也不能绕过合集和字幕约束。
字幕：运行流程只处理下载器清单中同目录、同 stem 且支持的旁车，不再扫描共享下载目录并混入其它 job 的文件。没有清单的独立预览保守阻塞不明字幕。
公共挂载：严格要求 qB save_path 等于 downloads，根不得同 inode，下载与媒体必须同设备；文件必须在本机 downloads 下真实存在且通过 fd 安全校验。这不能证明两个容器挂载的是相同宿主目录，实际 bind mount 验收仍是部署门槛。
空闲：未到期 subscription 只读快速返回；已完成和待确认 job 不预约/写事件/请求。测试比较 SQLite total_changes、事件游标和全部 HTTP 请求计数。活动下载和待发现媒体仍按配置间隔轮询。
测试新增：QBSessionRecoveryAndReplayLimit、QBAuthFailuresDoNotLoopOrLeak、QBMultipartAuthReplayPreservesExactBody、WholeManifestBlocksCollectionsAndAmbiguousSubtitles、ManifestExcludesOtherJobsSidecars、IdleJobsAndSubscriptionsDoNotWriteOrRequest、DownloadRootMustMatchAndFilesMustExistLocally。

本轮到此停止扩功能。Library 备份仅从 main+开发分支生成 bundle，不包含 git config、未跟踪部署配置、缓存、数据库或工具链；上传结果另在交付回复中记录。

## N1 已完成（2026-10-12，覆盖上文旧命名限制）

人工规则 v1：manual provider、本地稳定 series_id、显式 custom order、订阅/字幕组作用域 alias、人工季号与相对媒体目录。草稿/不可变版本/启停审计；本地 rules add/preview/enable/disable/replan/parse，和 daemon 共用实例锁，无远端操作。

schema v3 保留有界 RSS 原标题及来源关系、命名规则与历史计划。规则优先的单集通路支持 [02]/[03]，标题/文件季集矛盾及多来源映射冲突阻塞；低置信度不猜季号。原严格 SxxExx 通路保留，安全文件操作、旁车、做种计时和 Emby 状态边界不变。执行前原子冻结，未执行旧计划显式失效，执行后的规则变更不迁移历史；晚到来源冲突单独报告。

N3 仅纯解析/预览：显式第 4 季、相对 1–11、总第 67–77 和有界 MapAbsolute 函数，不连接合集执行器。东京复仇者篇章示例不赋予季号。完整用法与限制见 NAMING.md；独立阶段复核记录见 REVIEW.md。

本轮真实 Go 1.26.9 make verify 全通过：53 顶层测试及子测试、race、vet、build；git diff --check 通过。未改 asuka-agent、未访问真实服务、未重试远端、未合并或部署。GitHub 既有权限阻塞及缺少真实 CI 链接不变。后续持久合集逐文件计划、gateway 确定性通知留待下一小里程碑。

## N3/N4 已完成（2026-10-12，覆盖上文仅单集/合集预览限制）

显式有限 absolute 映射、episodes 相对边界与整包逐文件计划已接执行。合成11视频夹具分别使用相对、总第、无歧义裸总集和混合编号完成入库；实体编码/第四季/01–11/67–77 标题验证，不借此确认现实元数据。缺/重/不明/特殊/多集单文件、字幕歧义或范围证据冲突整包阻塞。

schema v4 添加整包目标预约及逐文件 pending/intended/linked/discovered 记录、事件方案/文件序号。中断不假报 imported，不删源或已成功链接；固定计划恢复；全部视频分别确认 Emby 才 ready。独立发现 F1–F5 已复现/修复/回归；人工计划和历史规则新增证据验证不再用当前新规则替换旧语义。

最终78个顶层测试及子测试、race/vet/build 通过；标题 fuzz 27,789 次无崩溃。状态机、SQL 迁移及审查发现细节见 COLLECTIONS.md/REVIEW.md。未宣称生产就绪；没有本次实现的外部二次审查、真实服务/NAS/Docker 验收。gateway/Discord/模型/元数据联网/历史媒体迁移仍未实现。未重试 GitHub，既有权限阻塞和无真实 CI 成功链接不变。

## 独立复验修复（e4de255 之后，无新增功能）

新5类缺陷已复现并修复：特殊篇统一门禁、标点 alias 绕过、Unicode/范围数字截断、迁移前缀预约冲突、旧计划冻结证据缺失。解析器v3先验证完整数字token；SQLite schema v5复用运行时预约校验并原子回滚。旧计划只有证据可证明一致时回填独立证明记录，不改历史Plan/digest；否则 legacy_evidence_unresolved 明确待人工处理。详情见 REVIEW.md。旧无证明的已执行计划尚无自动解除/历史迁移功能，禁止猜测。

本次修复最终验证：真实 Go 1.26.9，92个顶层测试及子测试、race/vet/build 全通过；原独立v2测试逐字节保持，git diff --check 通过。无真实服务或GitHub访问；待外部独立复验。

## 最终定向复验两项残余修复

统一媒体分隔 token 边界修复 _SP_/_OVA_；版本化比较视图兼容旧括号 alias，不修改历史规则或Plan。未知规范化版本/不可兼容身份直接阻塞。parser v4、identity_normalization_version=1（旧未标版本0兼容）、schema仍5。101顶层测试及子测试/race/vet/build通过，原独立v3断言保持；待外部独立复验，无生产就绪声明。

## 本地管理产品首版（8484e43 之后）

- `eec2242` 修复独立审查 P1：先取实例锁，再打开可写 SQLite/迁移；真实子进程覆盖 run/serve/rules，锁失败保持schema/表/内容不变。Docker CMD显式run，CLI默认仍只校验。
- Go embed 中文界面和实际 `/api`：持久来源引用订阅、启停/编辑/重试；分页任务/来源证据/阶段/做种期限；规则新版本、解释预览、独立启停和审计；单视频指纹方案确认、冻结计划详情、安全重试；Emby发现与事件。没有展示假产品数据。
- 显式 serve只允许回环IP；精确Host/Origin、CSRF进程内nonce、JSON请求、CSP、64KiB请求体、单写在途、版本校验、固定错误码。管理API不返回URL/GUID/magnet/凭据。HTTP取消并等处理器结束，再关闭数据库。
- 控制扩展v1持久化，不改领域schema5/历史Plan；配置来源URL仍由部署文件授权。没有任意URL输入；停订阅不取消已存在任务或停止做种保障。
- 实际Chromium桌面和390px手机视窗验收：订阅→RSS→qB→指纹人工确认→硬链接→Emby实际发现→事件；规则启用后的下一集自动；部分合集故障后真实重开SQLite并通过UI重试；XSS（订阅名与RSS标题）、快速导航晚响应、双击、HTTP错误恢复。Go测试还核对真实源/目标同inode、下载次数不增加、全部视频确认、最终事件一次。不是用静态build代替UI验收。
- 最终验证：112个顶层测试定义（含独立v4新增3项及opt-in浏览器测试）；make verify通过格式/vet/race/build，make verify-ui单独执行真实浏览器。独立v2/v3/v4断言不改。Actions已增加相同浏览器门，但未推送、未运行真实远端CI。

未完成：LAN身份认证/权限、真实NAS与Docker运行/具体上游兼容、gateway持久消费outbox与Discord发送、目录/NFO/元数据学习、模型建议、已冻结语义冲突的人工解除流程、任意合集手工逐文件改排。已有基于明确有界规则的整包导入/重放保持。未改asuka-agent；未访问真实服务、未重试GitHub或部署。事件已可读不等于通知已送达；本阶段是可用的本机管理首版，尚非完整NAS通知产品。

## 独立管理验收 v5 两项 P2 修复

原复现先在005e342运行：两次100来源配置后持久200、只见101、当前只轮询1；同页规则载入后第1季表单显示旧S02E03预览。上述PASS表示复现成功，不是修复通过。

修复后：调度独立全集查询；UI订阅100条分页/total/next，规则选择器跨页读取。控制schema2明确当前档案集合，统一活跃容量100、历史容量10000、超限事务回滚，历史证据不删除。原跨配置fixture实际收到100次本地feed请求。

预览对程序化载入/input/change/reset/导航及无事件字段变化核对代数和完整请求快照；旧成功/错误均不展示，保存草稿只提交当前已预览的revision内容。真实Chromium涵盖上述10种延迟响应组合、程序赋值后的保存拦截、102条历史分页及跨页规则订阅选择，并继续完整NAS侧fake闭环和部分合集恢复。

本轮不扩功能，未改asuka-agent/未推送/未部署/未外发。原独立管理与浏览器复现纳入仓库，将“缺陷存在”断言改为“正确行为”，HTTP负向矩阵不变，增加容量/回滚邻域测试。原版测试仍保存在审查Library v5；本轮结果与代码供下一轮独立复验，不将自测当外审。

最终验证：118个顶层测试定义（含2个opt-in真实浏览器测试）；make verify及make verify-ui通过。配置容量失败只额外公开固定subscription_limit/subscription_history_limit启动码，未知错误仍不输出原文。
