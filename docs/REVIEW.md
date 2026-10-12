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
