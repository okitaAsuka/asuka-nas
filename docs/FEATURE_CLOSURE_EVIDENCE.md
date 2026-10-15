# 未完成功能 lane / 2026-10-15

基线：`f19f1d275c1f755aca8c4024243fe089fb810208`，从 origin 的 `codex/nas-frontend-workflow-20261013` fetch 并逐字核对。保存环境初始为干净 `ba619cf`，没有意外修改。工作分支 `codex/nas-feature-closure-20261015`。父任务已批准本轮认证管理范围并分配独占文件；未递归委派、未修改 asuka-agent 或其他 lane。

## 覆盖矩阵与本轮取舍

| 优先级 | 能力 | 核验结论 / 证据 |
| --- | --- | --- |
| P0 | 可用的 LAN 管理身份 | 基线缺失；本轮新增显式 opt-in HTTPS、人类密码 verifier、有限内存会话/节流和 API 合同。UI 独立 owner 待整合。 |
| P1 | RSS → qB → 规则映射 → hardlink → Emby | 基线已实现。TestNotificationDownloadMappingVertical、TestRSSHTTPToTorrentUploadAndRestart、合集恢复测试。只 fake 兼容证据，未验证真实服务。 |
| P1 | 无模型离线人工命名 | 基线已有 TestNamingManualBatchFallbackWithoutModelRetry，覆盖拒绝/stale/offline。无需再开平行流程。 |
| P1 | 不可变意图、全包恢复、逐视频发现 | 基线已有 TestCollectionMidLinkDatabaseFailureAndFrozenRecovery、TestFileIntentMustCommitBeforeLink。本轮保持执行器/schema 不变。 |
| P1 | 人工对比 → 规则建议 → 人批 | 基线已有 TestRuleProposalHumanOnlyAtomicApproval；服务身份不能批准。 |
| P1 | 浏览器接入配置 | 部分：订阅档案引用/启停可编辑；URL、凭据仍为文件/env。后续单独设计原子保存与秘密边界。 |
| P2 | 元数据辅助 facts | 部分：有界目录/NFO 本地候选存在，在线适配器缺失；需明确 provider、provenance、fake 响应和人批，不猜季号。 |
| P2 | 通知标题/集号/链接 | 部分：确定性事件已实现；现有跨仓合同刻意只发 ID/固定码。任何内容扩展需父任务协调 agent/privacy 合同。 |
| P2 | 做种覆盖策略 | 部分：全局 0–7 天在 job 首次创建时冻结；订阅/任务覆盖控件未实现。 |
| P3 | 特别篇、跨季批次 | 未实现，当前明确 fail closed；一个 BatchOrganization 只有一个 season，不能靠去掉门禁实现。 |
| 实机验收 | NAS GUI 导入、挂载、网络、真实上游 | 未验证/等待用户部署验收；型号后缀/固件仍未核实。 |

## 实现边界和所有权

已有文件仅 `internal/nas/management.go`、`internal/nas/config.go`、`cmd/asuka-nas/main.go`。新增 access implementation、独立 access unit tests、`internal/nas/management_access_integration_test.go`、`cmd/asuka-nas/management_access_test.go` 和两份本 lane 文档。没有修改 UI、schema、执行器、事件/learning handler、Makefile/CI、打包模板或共享部署文档。

精确 DTO、cookie、CSRF、错误和 UI hooks 见 [MANAGEMENT_ACCESS_CONTRACT.md](MANAGEMENT_ACCESS_CONTRACT.md)。默认模式保留旧 process nonce 和回环 listener，session JSON 仅增加 mode/authenticated。认证模式不信任 forwarded headers，也不复用服务 bearer。所有上游/文件授权仍通过原 Management handler、CSRF/CAS 和完整 preview-confirm；身份认证不自动批准命名或规则。

## 验证入口与证据

本机真实 Go 1.26.9 linux/amd64 官方归档 SHA256：`42d158b4d8f7b61ac0a830567c940a86098fb7aac52e467a5ebec03ef5cc2f8d`。工具链位于 `/workspace/scratch/nas-feature-toolchain/go`。

本机 baseline `make verify` 的格式检查通过，`go vet` 阶段下载 `modernc.org/sqlite@v1.50.1` 返回 Forbidden。未绕过，不把未执行的 SQLite 集成/race/build 当通过。基线 Python preflight 5/5、package 10/10 通过；基线 SHA 已有 [CI](https://github.com/okitaAsuka/asuka-nas/actions/runs/38410078812) / [offline package](https://github.com/okitaAsuka/asuka-nas/actions/runs/38410078808) 成功，不能把它们当新代码验证。

独立 access 代码只依赖标准库，本地执行：

```sh
PATH=/workspace/scratch/nas-feature-toolchain/go/bin:$PATH \
GOMODCACHE=/workspace/scratch/nas-feature-toolchain/modules \
GOCACHE=/workspace/scratch/nas-feature-toolchain/cache \
go test -race -count=1 -v internal/nas/management_access.go internal/nas/management_access_test.go
```

配置/JSON 加固后，8 个顶层 access 测试全部 PASS（21.974s，另含 30 个边界/证书子测试）。覆盖配置规范化（默认端口、IPv4 简写、IPv6）、KDF/verifier 拒绝、服务实际口令复用、匿名静态/session bootstrap、精确 Host/Origin、伪造 forwarded headers、JSON/UTF-8/大小/方法限制、cookie 属性、旧 nonce/cookie、logout/relogin/restart/绝对到期、容量、随机源失败、单个 KDF 在途、全局/IP 并发节流、实际 TLS 与正确 CA 信任、错误 CA 拒绝。最终提交与新代码完整 CI 结果由交付回复绑定；独立测试不替代 SQLite 集成或浏览器检查。

SQLite 集成 `TestManagementAccessProductionTLSAndFrozenWorkflow` 使用生产 TLS listener/Management/SQLite 和临时 fake qB/Emby：匿名不能读任务；登录后完整人工 batch；独立 session nonce；原意图重复确认幂等、tampered digest 拒绝；logout 拒绝重放；已批准任务继续硬链接/逐文件发现/独立 stop。另有默认回环兼容/配置纯校验/plain HTTP 拒绝。CLI 子进程覆盖纯校验不访问文件/秘密、重复 listen 拒绝、缺/坏 verifier、缺 TLS，所有失败无上游访问、无遗留 listener、无秘密错误输出。这些用原 `make verify` 在既有 CI 执行；不增加独立测试入口或修改其他 lane fixture。

## 恢复和整合限制

- 会话/节流仅内存；重启全部会话失效并清空节流。绝对期限 12h、上限16；丢失 cookie 无法注销其对应服务器记录，等待到期或其他有效会话正常退出。不是多用户/IAM。
- logout 阻止后续请求，不取消已经通过授权并开始的业务；不回滚文件效果、冻结意图或做种截止。
- verifier、证书变更需部署者更新现有配置/env/挂载后重启；本轮没有创建实际凭据/证书、修改 NAS/Caddy/网络/权限、启动真实 listener 或连接真实上游。
- TLS fixture 用临时 CA 并显式配置客户端信任，没有 InsecureSkipVerify；真实证书信任、名称解析、GUI 挂载/端口映射仍需实机验收，不能要求用户忽略浏览器证书警告。
- UI 登录/注销/401失效/禁止写入自动重放由独立 UI owner 完成，父任务须整合后做实际浏览器鉴权验收。本分支只验证后端和既有回环 UI 回归。
- 共享 STATUS/LOCAL_MANAGEMENT/PACKAGING 文档的精确新增要点位于合同末节，由父任务整合；不改本 lane 以外文件。
- 配置编辑、在线 metadata、通知内容、独立 seed policy、special/mixed-season 均保留为后续迭代，不宣称端到端全部完成或已部署。
