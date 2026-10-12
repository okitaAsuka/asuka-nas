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
