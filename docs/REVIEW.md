# 独立复核与故障注入 / 2026-10-12

## 复核结论（c1c54c8 后）

1. 修复：只 fsync 最终目录不能保证新建上层目录持久化。现在逐层 fsync 父目录，源文件 fsync，再 linkat + 最终目录 fsync，最后才提交 imported；SQLite 显式 synchronous=FULL。
2. 修复：大文件哈希无取消点导致退出延迟。引入可取消 reader，链接前也检查取消；已发生的链接保留供恢复，不删除源或回滚文件。
3. 修复：按 job 串行导入可能让队尾已完成任务的停止检查被大文件阻塞。每批先扫描已知完成任务的 deadline，再处理导入。仍不保证操作系统长时间阻塞/进程离线期间准点停止。
4. 修复：配置限制 reader 的 EOF 可把超长尾部空格当合法文件；现在先有界读，再精确检查总长。
5. 加固：qB 边界拒绝 all、hash 列表和任意 URL，避免错误调用扩大停止范围。

不宣称恶意并发写入安全：已打开父目录若被可信宿主之外的进程搬到根外，fd 仍指向原目录。部署必须控制目录 rename/write 权限；源在下载完成后保持不再写入。测试验证 symlink 替换不能被跟随，不等同于多租户文件隔离或断电实测。

## 故障注入证据

`TestExternalEffectThenTransactionFailure` 在 events INSERT 上安装 SQLite RAISE(ABORT) trigger，分别中断 imported、seed_stopped、refresh_requested。验证 job UPDATE 与 event 一起回滚；重开 SQLite 后同 inode 链接重放、stop/refresh 可重复外部调用，但逻辑事件只出现一次；源仍存在。
`TestDirectorySwapCannotFollowSymlink` 并发替换目录和外部 symlink，外部目录保持空。
`TestClockRollbackRestartAndNoSourceDeletion` 验证 deadline 不后移，已停止状态重启/时钟回拨也不恢复做种。
`TestCancelledImportHasNoFilesystemEffect` 验证取消后无目标效果。
原测试继续覆盖 0 天/7 天、待确认、冲突、跨设备、字幕、指纹、Emby accepted≠discovered。

## 实现边界

- `files_linux.go`：真实 Linux fd/openat2/linkat/fsync，非 fake importer。
- `store.go`：真实 SQLite WAL、版本 CAS 和原子事件，非内存 fake。
- `clients.go`：真实 qB 5.x 和 Emby HTTP adapters；测试服务器是 httptest，没有真实上游验收。
- `engine.go`：真实一次 Tick 领域流程，完成与 seed/import/refresh/discovery 正交；Propose/Confirm 是库 API。
- 此复核阶段 CLI 仍仅校验配置；常驻 daemon、RSS HTTP/.torrent 在下一提交完成；HTTP 管理 API 和 Discord 仅文档契约。

## 文件树

cmd/asuka-nas/main.go — CLI
internal/nas/{config,store,clients,engine,files_linux}.go — 领域/边界
internal/nas/{config,clients,files,integration,recovery}_test.go — 行为测试
docs/{DESIGN,API,STATUS,REVIEW}.md — 决策、契约、证据
.github/workflows/ci.yml、Makefile、scripts/dev.sh、Dockerfile — 构建入口

## 测试名
internal/nas/clients_test.go:func TestHTTPBoundaryRejectsRedirectAndRedacts(t *testing.T) {
internal/nas/clients_test.go:func TestRSSUntrustedAndUnsupported(t *testing.T) {
internal/nas/clients_test.go:func TestQBLoginAndSemanticFailure(t *testing.T) {
internal/nas/clients_test.go:func TestDownloaderRejectsBroadOrInjectedCommands(t *testing.T) {
internal/nas/integration_test.go:func TestVerticalLoopReplayAndIndependentSeeding(t *testing.T) {
internal/nas/integration_test.go:func TestUnknownSeasonStopsImmediatelyAndConfirmation(t *testing.T) {
internal/nas/integration_test.go:func TestRefreshRetryAndCrashAfterLink(t *testing.T) {
internal/nas/integration_test.go:func TestFilesFailureDoesNotPreventStop(t *testing.T) {
internal/nas/integration_test.go:func TestDownloadPathMismatchStillStops(t *testing.T) {
internal/nas/recovery_test.go:func TestExternalEffectThenTransactionFailure(t *testing.T) {
internal/nas/recovery_test.go:func TestClockRollbackRestartAndNoSourceDeletion(t *testing.T) {
internal/nas/recovery_test.go:func TestDirectorySwapCannotFollowSymlink(t *testing.T) {
internal/nas/recovery_test.go:func TestCancelledImportHasNoFilesystemEffect(t *testing.T) {
internal/nas/recovery_test.go:func TestConfigSizeLimitIncludesTrailingWhitespace(t *testing.T) {
internal/nas/files_test.go:func TestFileSafetyAndIdempotence(t *testing.T) {
internal/nas/files_test.go:func TestStaleConfirmationAndSymlinkSidecar(t *testing.T) {
internal/nas/files_test.go:func TestConflictDoesNotBlockSeedDeadline(t *testing.T) {
internal/nas/files_test.go:func TestConfirmationRechecksConflict(t *testing.T) {
internal/nas/config_test.go:func TestConfigSafety(t *testing.T) {
internal/nas/config_test.go:func TestStoreAtomicReplay(t *testing.T) {
internal/nas/config_test.go:func TestRejectFutureSchema(t *testing.T) {

## RSS/daemon 阶段复核包

新增 `torrent.go`（有界 BEP3 原始 info 校验）、`source.go`（真实 RSS/.torrent HTTP 与来源约束）、`schedule.go`（持久预约/退避/来源事件）、`daemon.go`（两条常驻循环与单实例锁）、`source_test.go` 和 `cmd/asuka-nas/main_test.go`。
SQLite v2 增加 torrent_payloads/source_state/source_events；job JSON 增加 has_torrent/attempts/next_attempt，旧数据兼容。
旧 downloader/Emby/file adapters 仍是真实现，只有测试服务器 fake。daemon 现在是真实 CLI 能力，但只在本机 fixture/子进程中运行；无网络管理接口。

### 核心路径

Source.Poll：限频 GET RSS → 丢弃标题、哈希 GUID → 已见项跳过 → 同源/allowlist GET enclosure → TorrentHash 校验 → metainfo+job+resource_seen 同事务持久化。
Engine.RetryTick：先停止全部已知到期任务 → 按 next_attempt 限频 → attempt_started 预约 → pending/requesting/requested/completed → approved plan → LinkContext → imported → refresh_requested → 独立 discovered；失败持久化 retry_scheduled。
Files.LinkContext：openat2 固定源 → 指纹 → 逐层 mkdirat/openat2/fsync → 源 fsync → 独占 linkat → 同 inode 重放或冲突 → 再核指纹 → 目标目录 fsync → 返回给 SQLite imported 事务。
Daemon.Run：RSS/job 两循环共享 SQLite，context 取消 → 取消在途请求/哈希 → WaitGroup 等待 → CLI defer 关闭资源；真实 SIGTERM 子进程用例已验证。

### 最新全部测试名
internal/nas/integration_test.go:func TestVerticalLoopReplayAndIndependentSeeding(t *testing.T) {
internal/nas/integration_test.go:func TestUnknownSeasonStopsImmediatelyAndConfirmation(t *testing.T) {
internal/nas/integration_test.go:func TestRefreshRetryAndCrashAfterLink(t *testing.T) {
internal/nas/integration_test.go:func TestFilesFailureDoesNotPreventStop(t *testing.T) {
internal/nas/integration_test.go:func TestDownloadPathMismatchStillStops(t *testing.T) {
cmd/asuka-nas/main_test.go:func TestDaemonSIGTERM(t *testing.T) {
internal/nas/config_test.go:func TestConfigSafety(t *testing.T) {
internal/nas/config_test.go:func TestStoreAtomicReplay(t *testing.T) {
internal/nas/config_test.go:func TestRejectFutureSchema(t *testing.T) {
internal/nas/config_test.go:func TestSchemaMigrationAndReopenedConnectionPragmas(t *testing.T) {
internal/nas/recovery_test.go:func TestExternalEffectThenTransactionFailure(t *testing.T) {
internal/nas/recovery_test.go:func TestClockRollbackRestartAndNoSourceDeletion(t *testing.T) {
internal/nas/recovery_test.go:func TestDirectorySwapCannotFollowSymlink(t *testing.T) {
internal/nas/recovery_test.go:func TestCancelledImportHasNoFilesystemEffect(t *testing.T) {
internal/nas/recovery_test.go:func TestConfigSizeLimitIncludesTrailingWhitespace(t *testing.T) {
internal/nas/source_test.go:func TestTorrentExactInfoHashAndRejectMalformed(t *testing.T) {
internal/nas/source_test.go:func TestRSSHTTPToTorrentUploadAndRestart(t *testing.T) {
internal/nas/source_test.go:func TestSourceFailureBackoffPersists(t *testing.T) {
internal/nas/source_test.go:func TestEnclosureOriginRedirectAndBodyLimits(t *testing.T) {
internal/nas/source_test.go:func TestDaemonCancellationAndInstanceLock(t *testing.T) {
internal/nas/source_test.go:func TestJobRetrySurvivesRestartWithoutDelayingStop(t *testing.T) {
internal/nas/source_test.go:func FuzzTorrentHashDoesNotPanic(f *testing.F) {
internal/nas/source_test.go:func TestDaemonFailureLogsOnlySafeCodes(t *testing.T) {
internal/nas/source_test.go:func TestSourceReservationSurvivesCrashBeforeRequest(t *testing.T) {
internal/nas/source_test.go:func TestMultiFileTorrentRejectsPathAndAliasEscapes(t *testing.T) {
internal/nas/source_test.go:func TestPersistedTorrentCorruptionCannotDispatchDifferentHash(t *testing.T) {
internal/nas/clients_test.go:func TestHTTPBoundaryRejectsRedirectAndRedacts(t *testing.T) {
internal/nas/clients_test.go:func TestRSSUntrustedAndUnsupported(t *testing.T) {
internal/nas/clients_test.go:func TestQBLoginAndSemanticFailure(t *testing.T) {
internal/nas/clients_test.go:func TestDownloaderRejectsBroadOrInjectedCommands(t *testing.T) {
internal/nas/files_test.go:func TestFileSafetyAndIdempotence(t *testing.T) {
internal/nas/files_test.go:func TestStaleConfirmationAndSymlinkSidecar(t *testing.T) {
internal/nas/files_test.go:func TestConflictDoesNotBlockSeedDeadline(t *testing.T) {
internal/nas/files_test.go:func TestConfirmationRechecksConflict(t *testing.T) {

### 当前完整文件树（不含 .git 和本地二进制）
.dockerignore
.github/workflows/ci.yml
.gitignore
AGENTS.md
Dockerfile
Makefile
README.md
config.example.json
docs/API.md
docs/DESIGN.md
docs/REVIEW.md
docs/STATUS.md
go.mod
go.sum
internal/nas/clients.go
internal/nas/clients_test.go
internal/nas/config.go
internal/nas/config_test.go
internal/nas/daemon.go
internal/nas/engine.go
internal/nas/files_linux.go
internal/nas/files_test.go
internal/nas/integration_test.go
internal/nas/recovery_test.go
internal/nas/schedule.go
internal/nas/source.go
internal/nas/source_test.go
internal/nas/store.go
internal/nas/torrent.go
scripts/dev.sh

## 单集 MVP 收尾复核

- clients.go：新增只携带 HTTP 状态的错误类型；QB.request 对401/403才触发凭据引用→一次Login→一次重放，authRetryAt冷却60秒；非认证500不登录；multipart request body有界保留供精确重放。qbFailureCode把401、403、错误凭据、缺失凭据、冷却分别映射为固定码。
- selection.go：singleVideo检查完整清单，广泛识别视频以避免漏数，只支持唯一mkv/mp4；未知/多视频/不明字幕整体阻塞。engine.Propose也调用同一门禁。持久block_reason与临时error_code分离。
- files_linux.go：运行流程用PreviewManifest/ProposedManifest；旁车必须来自该torrent清单，不吸收共享下载目录其他任务的字幕。根同inode拒绝，原有同设备、openat2/linkat/fsync/指纹契约保持。
- schedule.go：未到期订阅读取next_poll后直接返回，不开写事务；终态/待确认job继续只读。空闲测试覆盖total_changes、事件增量、全部HTTP请求计数。
- 部署门槛未消失：必须核对容器真实bind mounts、Linux openat2/proc、文件权限/静态性、qB5/Emby版本、Docker与真实CI。没有管理接口和多集完整方案，不适合直接投入无人值守生产。

最新完整测试清单（40个Test + 1个Fuzz入口）：
internal/nas/integration_test.go:func TestVerticalLoopReplayAndIndependentSeeding(t *testing.T) {
internal/nas/integration_test.go:func TestUnknownSeasonStopsImmediatelyAndConfirmation(t *testing.T) {
internal/nas/integration_test.go:func TestRefreshRetryAndCrashAfterLink(t *testing.T) {
internal/nas/integration_test.go:func TestFilesFailureDoesNotPreventStop(t *testing.T) {
internal/nas/integration_test.go:func TestDownloadPathMismatchStillStops(t *testing.T) {
cmd/asuka-nas/main_test.go:func TestDaemonSIGTERM(t *testing.T) {
internal/nas/config_test.go:func TestConfigSafety(t *testing.T) {
internal/nas/config_test.go:func TestStoreAtomicReplay(t *testing.T) {
internal/nas/config_test.go:func TestRejectFutureSchema(t *testing.T) {
internal/nas/config_test.go:func TestSchemaMigrationAndReopenedConnectionPragmas(t *testing.T) {
internal/nas/auth_test.go:func TestQBSessionRecoveryAndReplayLimit(t *testing.T) {
internal/nas/auth_test.go:func TestQBAuthFailuresDoNotLoopOrLeak(t *testing.T) {
internal/nas/auth_test.go:func TestQBMultipartAuthReplayPreservesExactBody(t *testing.T) {
internal/nas/recovery_test.go:func TestExternalEffectThenTransactionFailure(t *testing.T) {
internal/nas/recovery_test.go:func TestClockRollbackRestartAndNoSourceDeletion(t *testing.T) {
internal/nas/recovery_test.go:func TestDirectorySwapCannotFollowSymlink(t *testing.T) {
internal/nas/recovery_test.go:func TestCancelledImportHasNoFilesystemEffect(t *testing.T) {
internal/nas/recovery_test.go:func TestConfigSizeLimitIncludesTrailingWhitespace(t *testing.T) {
internal/nas/clients_test.go:func TestHTTPBoundaryRejectsRedirectAndRedacts(t *testing.T) {
internal/nas/clients_test.go:func TestRSSUntrustedAndUnsupported(t *testing.T) {
internal/nas/clients_test.go:func TestQBLoginAndSemanticFailure(t *testing.T) {
internal/nas/clients_test.go:func TestDownloaderRejectsBroadOrInjectedCommands(t *testing.T) {
internal/nas/source_test.go:func TestTorrentExactInfoHashAndRejectMalformed(t *testing.T) {
internal/nas/source_test.go:func TestRSSHTTPToTorrentUploadAndRestart(t *testing.T) {
internal/nas/source_test.go:func TestSourceFailureBackoffPersists(t *testing.T) {
internal/nas/source_test.go:func TestEnclosureOriginRedirectAndBodyLimits(t *testing.T) {
internal/nas/source_test.go:func TestDaemonCancellationAndInstanceLock(t *testing.T) {
internal/nas/source_test.go:func TestJobRetrySurvivesRestartWithoutDelayingStop(t *testing.T) {
internal/nas/source_test.go:func FuzzTorrentHashDoesNotPanic(f *testing.F) {
internal/nas/source_test.go:func TestDaemonFailureLogsOnlySafeCodes(t *testing.T) {
internal/nas/source_test.go:func TestSourceReservationSurvivesCrashBeforeRequest(t *testing.T) {
internal/nas/source_test.go:func TestMultiFileTorrentRejectsPathAndAliasEscapes(t *testing.T) {
internal/nas/source_test.go:func TestPersistedTorrentCorruptionCannotDispatchDifferentHash(t *testing.T) {
internal/nas/control_test.go:func TestWholeManifestBlocksCollectionsAndAmbiguousSubtitles(t *testing.T) {
internal/nas/control_test.go:func TestManifestExcludesOtherJobsSidecars(t *testing.T) {
internal/nas/control_test.go:func TestIdleJobsAndSubscriptionsDoNotWriteOrRequest(t *testing.T) {
internal/nas/control_test.go:func TestDownloadRootMustMatchAndFilesMustExistLocally(t *testing.T) {
internal/nas/files_test.go:func TestFileSafetyAndIdempotence(t *testing.T) {
internal/nas/files_test.go:func TestStaleConfirmationAndSymlinkSidecar(t *testing.T) {
internal/nas/files_test.go:func TestConflictDoesNotBlockSeedDeadline(t *testing.T) {
internal/nas/files_test.go:func TestConfirmationRechecksConflict(t *testing.T) {

## N1 单独复核（2026-10-12）

完成实现后另做一次逐段人工自查（未委托第二审阅者）：规则解析/匹配、SQLite 事务、执行冻结、重试跳过条件、本地 CLI。发现并修复：
- 空标题来源原先可能被忽略：现保存空上下文，参与跨订阅一致性判定。
- 旧计划重规划后缺少完整记录：plan_history 在 plan_ready 同一事务保存不可变版本。
- 无计划的合集阻塞遇 epoch 后可能反复写入：记录本次已检查的规则/来源版本，重复 tick 不写事件。
- 多集连写 S01E02E03、S01E02-E03 和显式 S00 原先可能被误当单集/未知季：保守拒绝。
- 导入开始前的来源/规则竞态：import_started 事务校验 epoch，加 job CAS 拒绝并发来源变化。
- 外部文件错误保留固定诊断码，数据库等非命名错误不进入原文日志。

行为证据：同一持久人工规则对不同 infohash 的 [02]/[03] 自动硬链接，重启不重新确认；字幕/文件安全保持原测试覆盖；不同来源映射、标题/文件矛盾、停用规则不可 fallback；晚到矛盾保留历史目标；新规则使未执行预览 stale，显式 replan 后生成新历史版本；import_started 后规则升级仍恢复旧目标；空标题不能绕过；事件触发器回滚验证 ingest 与规则启用事务原子性；CLI 草稿不自动启用、共用 daemon lock、解析预览不联网。

最终验证：真实 Go 1.26.9，make verify（gofmt、vet、53 个顶层测试及子测试、race、build）通过；git diff --check 通过。所有网络仅 httptest，所有媒体仅临时目录。本轮未构建/运行 Docker、未尝试 GitHub、未部署。新增 golang.org/x/text 只用于 Unicode NFKC。

剩余边界：N1 没有合集执行、绝对集数自动执行映射、冻结冲突解除或历史迁移、元数据 provider、模型、Discord/gateway 发信；命名规则不是任意 DSL。任意规则启停使所有未执行预览保守失效。此自查和模拟测试不等于第三方审计或真实部署验证。

## N3/N4 与独立发现修复（2026-10-12）

外部独立审查针对 dcc97ca，证据来自 Library libfile_4686f4abe4f881919c9c3feeddb7c84e。其 independent_review_test.go 原样保留，cmp 验证未改断言。先在当前代码运行：F1 两个格式、F2、F4、F5 仍失败；F3 因本轮绝对证据门禁已通过。完成修复后全部独立用例通过，另覆盖小数/SxxExx 小数/连接编号、常规扩展名与分辨率、模糊已知 scope、历史规则升级后合集恢复及新 GUID、人工语义/任意目标、真实新矛盾和旧假冲突解除。

本轮另做完整 SQL/状态机人工自查：全包预约与历史/事件同事务；部分预约冲突回滚；intent 提交在硬链接之前；linked 提交在硬链接之后；所有 file records linked 才 imported；所有视频发现才 discovered。历史计划、规则和文件目标不因恢复切换到未来规则。完整迁移回滚检查确认重复历史目标不会更新 schema 版本或遗失 jobs。详见 COLLECTIONS.md。

新增高价值故障：第5个硬链接成功、file_linked 事件插入失败，数据库保持 intended，重启同 inode 恢复；第7个文件权限错误，中间成功链接保留；file_intent 提交失败时对应文件完全未链接；最终 discovered 事务失败保留11视频发现进度，重启只提交一次最终事件；部分合集失败到7天截止仍停止做种。权限故障由文件边界注入，未宣称真实宿主权限测试。跨任务目标预约测试故意先插入一个不冲突目标再遇冲突，验证整事务回滚。

最终：真实 Go 1.26.9 linux/amd64，make verify 的 gofmt/vet/78 个顶层测试及子测试/race/build 全通过；git diff --check 通过。FuzzTitleEvidenceBounds 5秒、2 workers、27,789 次执行，无崩溃。原独立审查不覆盖本次新增实现，尚无第二轮外部审查，也没有真实服务、NAS、断电或 Docker 运行验收。没有新依赖、模型、Discord、GitHub 重试、合并或部署；asuka-agent clean。

## e4de255 独立复验修复（暂停扩功能）

输入：Library libfile_4686f4abe4f881919c9c3feeddb7c84e v1（33062B）。保留 independent_v2_test.go / independent_v2_recovery_test.go 原始断言。修复前实际复现全部5类、11个失败实例；原跨进程预约/CAS、真实第5硬链接后退出恢复、部分 Emby 发现等对照仍通过。

1. RSS 与所有 manifest 项统一特殊篇门禁，包含视频、字幕和辅助文件；有界/无界规则都不能忽略 SP/OVA/OAD 等标记。
2. 规范化中统一中英括号、全角冒号；别名边界支持冒号后完整季集 token，已知身份但 scope 不明确仍阻塞。未采用无界子串匹配。
3. NFKC 之前拒绝 Unicode No/Nl 数字（分数/上标等），避免改变数字语义；数字 token 先完整消费，只接受整数或整数范围，有限质量/年份和视频扩展名是明确例外。移除原来的后缀补丁黑名单；范围端点、小数和分数不能截断。解析器版本3。
4. schema v5 在同一 sql.Tx 中调用运行时 reservePlanTx 回填旧计划；完全一致的目标/前缀冲突检查。错误回滚 DDL、状态、事件和 schema 版本，保留旧库。没有磁盘媒体操作。
5. 旧无规则计划的证明数据放入 legacy_plan_evidence，不改 Plan 或其 digest。仅在计划/当前来源版本一致、目标季集可解析且全部保留标题语义相符时回填；否则标记 legacy_evidence_unresolved 并阻塞后续导入/媒体处理，保持已归档历史。相同或兼容语义的新 GUID 与证明数据比较，真正新矛盾仍阻塞。无证明不猜、不自动迁移媒体。

新增表驱动覆盖有界/无界与 RSS/文件位置、SP/OVA等类别、括号/冒号类别、分数/上标/实体编码/范围端点；旧证据的存在/缺失/矛盾/晚到版本及摘要不变。保留原独立复验测试，不改变其断言。完整验证结果见本次交付，待外部独立复验；不宣称生产就绪。

## 6d23b5d 两项 P1 残余修复（仅修复，无新增功能）

输入独立报告 libfile_4686f4abe4f881919c9c3feeddb7c84e v2、43403B。原样引入 independent_v3_targeted_test.go；修复前实际复现 _SP_、_OVA_ 与旧停用圆括号 alias 共3个失败实例，其余对照通过。修复后与报告文件 cmp 一致，未改断言。

共同契约位于 identity_contract.go：
- releaseDelimiter 采用 Unicode 空白/标点/符号分词，明确包含下划线。特殊篇只比较完整标记 token（允许数字后缀），不再使用 regexp 的 ASCII word boundary；SPARK/NOVATION/OVATION 等普通单词不命中。RSS、全部 manifest 项和规则解析共用此门禁。
- 新规则标记 identity_normalization_version=1；未标版本0作为历史原文兼容。comparisonView 对 alias/group 创建副本并做与标题一致的规范化，不更新规则行、不改变目录/显示名/历史Plan。结构括号前布局分隔符及括号/冒号空白采用同一幂等处理，作品名内部标点不折叠。HTML/NFKC只执行一次，避免重复解码造成不对称。
- 未知规范化版本或不能安全规范化的存量身份，在所属订阅直接 rule_normalization_unresolved，不允许回落严格文件名。已知身份的未识别尾部仍 rule_scope_ambiguous，不能因格式变化丢掉停用规则。

表驱动覆盖标记×分隔符、普通嵌入词对照、旧alias圆/方括号/年份/质量/HTML实体、启用与停用、结构布局、未知版本、数字语义不兼容；比较规则原文不变、冻结PlanDigest在新GUID/重启后不变。parser版本4，SQLite仍schema5，无迁移或历史重写。

最终真实 Go1.26.9 make verify：101顶层测试及子测试、race、vet、build 全通过；git diff --check通过，原独立v3测试逐字节保持。未访问真实服务/GitHub、未修改asuka-agent、未合并或部署。等待外部独立复验，不宣称生产就绪。

## 产品整链审查 P1：迁移前实例锁

独立报告 PRODUCT_REVIEW.txt（Library 审查包 v4）复现第二实例在持锁失败前把 schema4 改5。统一 OpenLockedStore 先准备私有父目录、取 flock，再打开可写 SQLite/迁移；daemon 与 rules CLI 共用。真实子进程在另一个进程持锁时启动失败，断言 schema4、缺失的迁移表、哨兵内容保持不变。SIGTERM 原回归保持。其余产品矩阵与待接入项见 WEB_PLAN.md；底层测试通过不等于通知产品闭环。

## 中文管理面验收

management_test覆盖Host/Origin/CSRF、SSRF来源引用限制、秘密不出站、大小限制、单写在途、订阅持久启停、预览不落库、规则审计/epoch、确认CAS、冻结计划重试、HTTP取消等待。startup_lock_test真实第二进程覆盖run/serve/rules。

TestBrowserProductLoop + tests/browser_e2e.py使用实际Chromium与临时文件/HTTP，操作真实界面；fake控制只用于测试进程模拟外部下载和发现，不存在于产品。验证订阅管理→人工文件确认→已导入但未发现→发现事件；启用规则→下集自动；权限故障部分合集SQLite重开后UI重试。断言同inode、没有重下、PlanDigest不变、每job discovered一次。浏览器动态故障包括不可信HTML、双击、延迟旧响应和503恢复；截图经目视检查，仅存在/tmp，不上传Library。

独立审查包v4提供的independent_v4_acceptance_test.go原样纳入，继续锁定分隔token、旧group/alias兼容和历史路径/digest不变、未知规范化版本阻塞。所有旧复现文件不改断言。


## P2 原复现→修复→回归（审查包v5）

输入HEAD005e342，先原样运行TestIndependentControlProfileChurn/TestIndependentBrowserPreviewOwnership/TestIndependentHTTPNegativeMatrix；原日志记录200/101/1和旧S02E03。随后只把两项“缺陷存在”预期转换为正确行为，并扩展边界：分页和调度独立、停用/缺失档案、初始化/API同容量、超限事务回滚；浏览器输入/载入/无事件赋值/reset/导航分别延迟真实API成功与错误响应、保存快照校验、分页与规则选择器跨页。API返回仍不含秘密URL。

关键差异：daemon调用ScheduledSubscriptions；SubscriptionPage只供UI；InitControl在事务内维护control_profiles和控制schema2并检查容量；PutSubscription同容量检查。fill调用invalidatePreview，预览响应ownsResponse比较页面代数、表单代数、完整request快照，saveDraft再次核对并提交acceptedPreview.rule。既有后端计划digest、规则revision/epoch授权不变。
