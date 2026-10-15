# 架构 lane / 2026-10-15

基线：`f19f1d275c1f755aca8c4024243fe089fb810208`，来自
`codex/nas-frontend-workflow-20261013`。本 lane：`codex/nas-architecture-20261015`。
环境初始 `work` 分支为 `ba619cf`，工作树干净；已保留该分支，没有 merge main。
本文只记录本 lane 的诊断、归属与验证，不覆盖现有产品状态或授权部署。

## 优先诊断与边界

| 优先级 | 代码位置 | 观察与处理 |
| --- | --- | --- |
| P1 | `collection_plan.go:collectionPlan`、`batch_organizer.go:bindManifest` | **已修复**：两条规划路径共用 `plan_manifest.go:bindManifest`。原 helper 函数体保持，包含辅助文件证据、排序和已取指纹复用；不改映射、批准或执行行为。 |
| P1 | `execution_engine.go:executePlan`、`execution_store.go:advanceFileTx`、`files_linux.go:LinkContext` | 已有正确分层：完整预检→持久 intent→硬链接/fsync→逐文件确认→整包完成。不可把预检与最终 fd 校验合并：两者覆盖不同时间窗口。此轮不改变执行器。 |
| P1 | `integration_test.go:newFixture`、`recovery_test.go`、`collection_test.go` | **已修复**：移至 `fixture_test.go`，构造开始即注册带 nil 检查的 cleanup；中途 Fatal 仍释放已取得的 HTTP/fd/DB。`fixture_fault_test.go:failEvent` 统一有界事件/可选文件序号故障注入及解除，保留原恢复断言。 |
| P2 | `engine.go:Tick/tick`、`schedule.go:RetryTick`、`daemon.go:TickJobs/Run` | 作业正交状态与持久退避分开；两条入口都先处理做种截止。重复扫描不是简单重复代码：直接 Tick 与 daemon RetryTick 的预约/退避语义不同。此轮不合并，不引入通用状态机框架。 |
| P2 | `store.go:OpenStore/save`、`control_store.go:InitControl`、`migration.go:migratePlansTx` | 单连接 SQLite + JSON job/CAS + 事务事件；控制扩展单独版本。迁移复用运行时目标预约，旧证据无法证明则阻塞。schema SQL 集中且跨模块 helper 耦合，但没有足够收益支持本轮重拆 repository；不改 schema。 |
| P2 | `management.go:api/detail`、`learning_service.go:proposalAPI`、`naming_assistance_http.go` | 路由、DTO 和应用编排仍在同包，部分处理器触及 Store。功能迭代应调用 Engine/Store 现有事务入口，不在 HTTP 层复制批准逻辑。此轮保留公共 API 和人工终审门。 |
| P2 | `event_service.go:EventHandler`、`learning_service.go:LearningHandler` | **已修复（父任务追加授权）**：同一 Bearer/Host/Origin/Sec-Fetch 门禁提取为 `service_boundary.go` 私有 wrapper。两份路由主体保持；事件只读405、学习未知方法404、缺命名引擎503仍独立。先增加真实 Handler 合同，再提取。 |
| P2 | `clients.go:HTTPBoundary.request/QB.request`、`source.go`、`config.go` | 网络适配器有超时、响应上限、禁重定向、有限认证重放；取消在上层凭 ctx 判断。配置不探测真实服务。固定错误码的字符串分类是债务，不能以泛化错误包装改变日志/事件隐私。 |
| P2 | `selection.go:singleVideo`、`collection_plan.go:manifestGroups` | 扩展名和字幕分类重复，但单集与合集对不支持视频/数量的错误优先级不同。本轮不直接替换为一个通用分类器，避免悄然改变自动命名门禁。 |
| P2 | `Makefile`、`.github/workflows/ci.yml`、`.github/workflows/package.yml`、`scripts/package_nas.py` | 已有 Go/race、独立真实浏览器门、公开模板白名单和离线包校验。环境默认 `go` 为同名其它工具；`scripts/dev.sh` 已支持显式真实工具链。没有发现本轮必需的流水线修复；共享工作流/Makefile/顶层文档保持。 |

## 已确认归属与兼容性

- 生产：`internal/nas/collection_plan.go`；`batch_organizer.go` 仅移动 `bindManifest`；`event_service.go`、`learning_service.go` 仅替换共同请求门；新增 `plan_manifest.go`、`service_boundary.go`。
- 测试：`integration_test.go`、`recovery_test.go`、`collection_test.go`；新增 `fixture_test.go`、`fixture_fault_test.go`、`plan_manifest_test.go`、`service_boundary_test.go`。
- lane 文档：本文件。以上修改均在父任务确认归属后开始，初始独立合同测试先提交以验证旧行为。
- 依赖：保留 `FileOperations`、`Plan`、`SourceEvidence`、`Store`、既有 fixture API；无 production 接口、JSON、schema、migration、默认配置变化，不需要 feature/UI 改接口。
- 不写：`management.go`、`config.go`、`cmd/asuka-nas/main.go`、`management_access*`、前端/浏览器测试、engine/store/migrations、命名处理器/规则、其它 lane 文档、asuka-agent、部署或网络配置。
- 两份服务内部处理器的路由主体逐字保留；超时仍由服务自己的处理器设置。没有将管理身份、服务 Bearer 或人工终审合并。
- 构造服务时的 token 范围/独立身份检查仍在原 `Open*Service`；私有 wrapper 保持原 handler 的长度与精确身份检查，不新增策略选项。

## 已完成与延期

已完成三项有界改进：完整清单绑定去重；事件/学习请求身份门去重；共享假上游与故障注入整理，修复 fixture 构造失败时资源清理缺口。
没有产品功能被宣称“补全”；这次修复的是测试基础设施的失败清理行为，生产改动为保留行为的提取。

明确延期：单集/合集分类器合并、Engine Tick/RetryTick 合并、repository/schema 重拆、HTTP 层与应用层全面分包、固定错误码类型化、配置校验去重。
这些分别涉及错误优先级、调度退避、事务/CAS、批准边界或隐私契约，收益不足以支持与本次小提取合并。
真实 NAS、上游版本兼容、部署和其它 lane 的 HTTPS 管理能力仍由对应 owner 验收。

## 测试库存与保留原则

基线静态库存为 **42 个 Go 测试文件、170 个 Test 函数、2 个 Fuzz 函数**。
这不是执行通过数：包含 opt-in 浏览器/cross-repository 测试。
完整名称清单在执行环境 `/workspace/architecture-evidence/baseline-test-inventory.json`。

没有发现可直接删除且无损的“垃圾测试”。历史 independent/review 文件包含独立回归，
文件名历史化不代表覆盖低价值。本轮删除的仅为重复脚手架语句，**未删除测试或安全断言**。

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

## 前后测试库存

| 项目 | 基线 | 最终实现 |
| --- | ---: | ---: |
| Go `_test.go` 文件 | 42 | 46 |
| 顶层 Test 函数 | 170 | 174 |
| Fuzz 函数（常规门执行 seeds） | 2 | 2 |
| 删除的原 Test/Fuzz | — | 0 |

库存逐名比对保留全部原测试；`integration_test.go` 的五个 Test 函数体逐字未变。
四个既有恢复 Test 仅替换 CREATE/DROP TRIGGER 脚手架；同 inode、先提交 intent、逐文件状态、重开数据库、独立做种、最终事件恰好一次等断言原样保留。

新增四个行为合同（不是源码字符串断言）：

- `TestPlanManifestEvidenceContract`：两条规划路径完整真实指纹，包括不入库辅助文件；不同清单顺序的完整 Plan JSON/摘要相同，预览无 DB/events/目录效果。
- `TestPlanManifestRejectsUnavailableAuxiliary`：两条路径分别覆盖缺失/symlink 辅助文件和取消，失败不返回或持久化可执行方案。
- `TestServiceRequestIdentityContract`：两个真实 Handler 各13项身份场景；32/31字符边界、精确 Bearer/Host、Origin、Fetch 元数据、转发头、响应头与请求体不读取。
- `TestServiceRoutePoliciesRemainDistinct`：两个真实 Handler 的鉴权优先顺序、禁止审核/管理路由、严格 cursor、各自方法/命名路由响应，及接入 Engine 后仍能读命名队列。

## 验证与限制

已下载官方 Go 1.26.9 linux/amd64 并按 `https://go.dev/dl/?mode=json&include=all`
中的 SHA256 校验：`42d158b4d8f7b61ac0a830567c940a86098fb7aac52e467a5ebec03ef5cc2f8d`。
工具链安装在工作区，不替换系统命令。

本地检查：

- `python3 -B -m unittest discover -s scripts -p 'test_package_nas.py' -v`：10项通过，fake Docker，非镜像构建。
- `python3 -B -m unittest discover -s tests -p 'test_preflight.py' -v`：5项通过，只使用临时目录。
- `gofmt`、`git diff --check`、前后逐名测试库存比对通过。
- 显式 Go 1.26.9 的 `make verify` 在 vet 依赖下载阶段退出2：`modernc.org/sqlite@v1.50.1` 官方代理下载 Forbidden；一次备用代理也403。没有跳过依赖或修改模块来制造本地通过，Go/race/build/UI 依赖已授权的原有 CI。

提取前合同证据：

- `9e48e6403303fae0bce43d8edc0a4b6a5a047cc3` 仅新增清单合同和诊断；[CI 38685765701](https://github.com/okitaAsuka/asuka-nas/actions/runs/38685765701) 成功，包含 Go1.26.9 `make verify` 与全部7项真实 Chromium 测试（fake上游）；[包装38685765833](https://github.com/okitaAsuka/asuka-nas/actions/runs/38685765833) 成功。
- `0d479dcd6c07c8538d9b4eb2869503dc835094cf` 在生产门禁提取前增加服务合同，用于比较旧实现与新实现；运行结果及最终实现 SHA 的 CI 在任务最终交付绑定。

最终实现继续使用未修改的 `make verify`、`make verify-ui` 和包装 CI。
常规 Go 门包含按环境变量 opt-in 的 skip，库存数量不等于实际通过数；跨仓 agent smoke、持续 fuzz、真实断电/部署/上游/NAS 未在本轮执行。
执行环境日志与机器库存位于 `/workspace/architecture-evidence/`，完整可审阅结果以最终提交与 GitHub run/job 为准。

无真实 RSS/qB/Emby/Discord/LLM、私有 NAS 数据、凭据创建、Caddy/网络配置访问；离线包构建及无网络配置校验不是部署。
父任务仍需在集成提交上验证其它 lane 变更与这套 fixture/服务合同兼容，本 lane 自测不代替最终集成验收。
