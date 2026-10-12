# v1 agent 契约（设计，HTTP listener 尚未实现）

部署时用户提供静态 Bearer token，仅用于本服务；不由开发流程生成。仅可信内网，外部访问需要 TLS。agent 不得直连 SQLite。

- GET /v1/jobs/{id}: version、download_state、import_state、refresh_state、discovery_state、seed_state、completed_at、stop_at、error_code、block_reason。
- GET /v1/confirmations: 待确认 job 和 plan version。原始文件名/RSS 上下文只可在鉴权响应中返回，永不进事件/日志；UI 必须转义。
- POST /v1/jobs/{id}/preview: 明确 title/season/episode（不猜季度），返回 version、plan digest、文件指纹、相对目标。
- POST /v1/jobs/{id}/confirm: version + plan digest，绑定完整文件集合；过期 409、指纹变化 412、路径错误 422、冲突 409。确认只批准该方案，执行仍校验文件。
- GET /v1/events?after=123&limit=100: {id, schema_version:1, job_id, kind}；按 ID 正序，断线从 last acknowledged ID 恢复。消费者按 id 幂等，不承诺 exactly-once。

事件类型：resource_seen、download_requested、download_submitted、download_completed、needs_confirmation、plan_ready、plan_confirmed、imported、refresh_requested、discovered、seed_stopped、operation_failed。
事件只包含不透明 job ID 与固定类型，不包含 URL、标题、token、原始异常。
Discord、远程命名建议提交、鉴权 listener 和持久消费 cursor API 都后置，当前 Store.Events(after) 已可重放。


实现补充（无 HTTP listener）：`Store.SourceEvents(after)` 返回独立来源事件游标，字段 id/subscription/kind/created_at，kind 为 poll_succeeded/poll_failed。订阅 ID 是配置内非秘密标识；无 URL/错误原文。job 事件新增 attempt_started/retry_scheduled/retry_cleared。HTTP 路由和鉴权仍是契约，不应将内部方法当已部署接口。

N1 内部契约：Job 增加 naming_epoch/context_version/checked_context；Plan 增加 rule_epoch/context_version/decision/rule_refs/started，digest 包含规则快照引用与来源版本，排除 approved/started 两个生命周期标记。新事件 source_context_added、source_context_checked、late_mapping_conflict、naming_retry_ready、plan_stale、plan_invalidated、import_started。plan_history 保存每个 plan_ready 的不可变完整方案。规则审计 naming_events 是独立递增游标表（本阶段无对外读取接口），只含 rule_id/revision/kind/created_at，kind 为 rule_drafted/rule_enabled/rule_disabled。相关本地 CLI 和边界见 NAMING.md；gateway 确定性通知仍待下一阶段实现。


N3/N4 内部契约：Plan 新增 collection、decisions（逐视频决定）、manifest（所有源指纹）、manual（人工计划语义）、evidence（订阅与标题摘要），全部参与摘要与不可变历史。Store.FileExecutions(job,version) 返回序号/state/video/discovered；events 增加可空 plan_version/file_index，新增 file_intent/file_linked/video_discovered/mapping_conflict_cleared。最终 discovered 仅在所有视频确认后产生一次；HTTP/gateway/Discord 仍未实现。
