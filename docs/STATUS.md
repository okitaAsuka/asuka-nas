# 当前状态 / 2026-10-12

当前最新阶段：独立复验5类缺陷修复，schema v5 / parser v3，见文末及 REVIEW.md。下文旧阶段按历史保留；生产就绪未获确认。

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
