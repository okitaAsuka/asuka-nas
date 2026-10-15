# 架构 lane / 2026-10-15

基线：`f19f1d275c1f755aca8c4024243fe089fb810208`，来自
`codex/nas-frontend-workflow-20261013`。本 lane：`codex/nas-architecture-20261015`。
环境初始 `work` 分支为 `ba619cf`，工作树干净；已保留该分支，没有 merge main。
本文只记录本 lane 的诊断、归属与验证，不覆盖现有产品状态或授权部署。

## 优先诊断与边界

| 优先级 | 代码位置 | 观察与处理 |
| --- | --- | --- |
| P1 | `collection_plan.go:collectionPlan`、`batch_organizer.go:bindManifest` | 两份完整清单指纹绑定逻辑逐句重复。辅助文件即使不入库，也必须进入证据。应复用同一 helper，保留排序、指纹复用和序列化结果；不改映射、批准或执行行为。 |
| P1 | `execution_engine.go:executePlan`、`execution_store.go:advanceFileTx`、`files_linux.go:LinkContext` | 已有正确分层：完整预检→持久 intent→硬链接/fsync→逐文件确认→整包完成。不可把预检与最终 fd 校验合并：两者覆盖不同时间窗口。此轮不改变执行器。 |
| P1 | `integration_test.go:newFixture`、`recovery_test.go`、`collection_test.go` | 共用假上游埋在业务测试里；资源 cleanup 在构造成功末尾才注册，早期失败可能遗留 HTTP/fd/DB；故障触发器模板重复。应分离夹具、在资源取得后注册 cleanup、统一事件故障注入，保留原恢复断言。 |
| P2 | `engine.go:Tick/tick`、`schedule.go:RetryTick`、`daemon.go:TickJobs/Run` | 作业正交状态与持久退避分开；两条入口都先处理做种截止。重复扫描不是简单重复代码：直接 Tick 与 daemon RetryTick 的预约/退避语义不同。此轮不合并，不引入通用状态机框架。 |
| P2 | `store.go:OpenStore/save`、`control_store.go:InitControl`、`migration.go:migratePlansTx` | 单连接 SQLite + JSON job/CAS + 事务事件；控制扩展单独版本。迁移复用运行时目标预约，旧证据无法证明则阻塞。schema SQL 集中且跨模块 helper 耦合，但没有足够收益支持本轮重拆 repository；不改 schema。 |
| P2 | `management.go:api/detail`、`learning_service.go:proposalAPI`、`naming_assistance_http.go` | 路由、DTO 和应用编排仍在同包，部分处理器触及 Store。功能迭代应调用 Engine/Store 现有事务入口，不在 HTTP 层复制批准逻辑。此轮保留公共 API 和人工终审门。 |
| P2 | `event_service.go:EventHandler`、`learning_service.go:LearningHandler` | Bearer/Host/Origin/Sec-Fetch 门禁重复；未来可用行为合同约束后提取私有 helper，但这涉及另一个边界，不和清单 refactor 同时进行。 |
| P2 | `clients.go:HTTPBoundary.request/QB.request`、`source.go`、`config.go` | 网络适配器有超时、响应上限、禁重定向、有限认证重放；取消在上层凭 ctx 判断。配置不探测真实服务。固定错误码的字符串分类是债务，不能以泛化错误包装改变日志/事件隐私。 |
| P2 | `selection.go:singleVideo`、`collection_plan.go:manifestGroups` | 扩展名和字幕分类重复，但单集与合集对不支持视频/数量的错误优先级不同。本轮不直接替换为一个通用分类器，避免悄然改变自动命名门禁。 |
| P2 | `Makefile`、`.github/workflows/ci.yml`、`.github/workflows/package.yml`、`scripts/package_nas.py` | 已有 Go/race、独立真实浏览器门、公开模板白名单和离线包校验。环境默认 `go` 为同名其它工具；`scripts/dev.sh` 已支持显式真实工具链。本轮不修改共享工作流/Makefile/顶层文档，未获父任务确认。 |

## 归属提案（等待父任务与 feature owner 协调）

- 生产：`internal/nas/collection_plan.go`；`internal/nas/batch_organizer.go` 仅移动 `bindManifest`；新增 `internal/nas/plan_manifest.go`。
- 测试：`internal/nas/integration_test.go`、`internal/nas/recovery_test.go`、`internal/nas/collection_test.go`；新增 `fixture_test.go`、`plan_manifest_test.go`。
- lane 文档：本文件。
- 依赖：保留 `FileOperations`、`Plan`、`SourceEvidence`、`Store`、既有 fixture API；不要求 feature/UI 修改接口。若 feature owner 需要 `batch_organizer.go`，可保留 helper 原位，由合集调用它。
- 不写：前端资产/浏览器测试、engine/store/migrations、naming/rules/API、其它 lane 文档、asuka-agent、部署或网络配置。

共享代码修改必须在父任务确认后开始；隔离文档、基线验证和新测试原型可先完成。

## 测试库存与保留原则

基线静态库存为 **42 个 Go 测试文件、170 个 Test 函数、2 个 Fuzz 函数**。
这不是执行通过数：包含 opt-in 浏览器/cross-repository 测试。
完整名称清单在执行环境 `/workspace/architecture-evidence/baseline-test-inventory.json`。

没有发现可直接删除且无损的“垃圾测试”。历史 independent/review 文件包含独立回归，
文件名历史化不代表覆盖低价值。本轮计划删除的仅为重复脚手架语句，**不删除测试或安全断言**。

| 必留不变量 | 现有行为测试（代表） |
| --- | --- |
| 路径约束、不覆盖、同 inode 重放 | `TestFileSafetyAndIdempotence`、`TestDirectorySwapCannotFollowSymlink`、`TestConfirmationRechecksConflict` |
| 先 commit intent 再链接；外部效果后事务失败可恢复 | `TestFileIntentMustCommitBeforeLink`、`TestExternalEffectThenTransactionFailure`、`TestCollectionMidLinkDatabaseFailureAndFrozenRecovery` |
| 整包完整、逐视频发现、最终事件恢复 | `TestCollectionElevenVideosRelativeAbsoluteMixed`、`TestAllVideoDiscoveryRequiredAndPersists`、`TestReadyEventFailureDoesNotLosePerVideoDiscovery` |
| 旧审阅/指纹/规则变化不能批准 | `TestStaleConfirmationAndSymlinkSidecar`、`TestBatchStalenessFullFingerprintsAndReservations`、`TestNamingSemanticChangesStillStale` |
| 不可变意图与来源 | `TestBatchWholePlanIntentAndCompletion`、`TestNamingAssistanceApprovalAndProvenance`、`TestNamingLegacyManualRetainsExposure` |
| 认证/CSRF/服务身份隔离 | `TestIndependentHTTPNegativeMatrix`、`TestLearningAuthorizationPrivacyAndSchema`、`TestNamingServiceIsolation` |
| 做种独立、0–7天、停止不删除 | `TestVerticalLoopReplayAndIndependentSeeding`、`TestClockRollbackRestartAndNoSourceDeletion`、`TestJobRetrySurvivesRestartWithoutDelayingStop` |
| 人工终审复用规则 | `TestRuleProposalHumanOnlyAtomicApproval`、`TestRuleProposalRejectStaleAndRevision` |
| 有界认证重放、取消、隐私 | `TestQBMultipartAuthReplayPreservesExactBody`、`TestDaemonCancellationAndInstanceLock`、`TestDaemonFailureLogsOnlySafeCodes` |

## 验证与限制

已下载官方 Go 1.26.9 linux/amd64 并按 `https://go.dev/dl/?mode=json&include=all`
中的 SHA256 校验：`42d158b4d8f7b61ac0a830567c940a86098fb7aac52e467a5ebec03ef5cc2f8d`。
工具链安装在工作区，不替换系统命令。后续命令显式指定 PATH/GOTOOLCHAIN。

基线 Python 包装测试：10 项通过（fake Docker，非镜像构建、非部署）。
Go 基线 `make verify` 在 vet 的依赖下载阶段失败：官方模块代理跳转的
`modernc.org/sqlite@v1.50.1` 下载返回 Forbidden；单次备用代理重试也返回 403。
未运行到 Go 测试/race/build，未将该门槛记为通过。

协调期间新增独立 `plan_manifest_test.go` 原型，两组行为合同覆盖两条现有规划路径：

- 清单含真实完整指纹，输入顺序改变不改变完整 Plan JSON 或摘要；预览不改变下载器顺序、DB/events/媒体目录。
- 缺失或 symlink 辅助文件以及取消必须拒绝方案，不遗留可执行计划或目录效果。

尚未修改任何基线已有文件。当前库存变为 43 个 Go 测试文件、172 个 Test、2 个 Fuzz；
原 170 个 Test 和全部断言保持。新增测试尚待 Go 依赖可用的本地或 CI 执行，
不能算作已验证实现。`gofmt` 与 `git diff --check` 通过。
无真实 RSS/qB/Emby/Discord/LLM、私有 NAS 数据、凭据创建、Caddy/网络配置访问。

协调检查点不是最终交付。待父任务明确归属后才提取生产 helper、迁移共用夹具与故障注入。
共享 Makefile/workflows/顶层文档仍保持基线。最终提交、验证与兼容性将在实现后补齐。
