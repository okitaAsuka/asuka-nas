# 人工批量方案与后续 agent 契约 v1

本轮只建立单下载任务的人工整理方案和可信NAS对比记录。未接入LLM、agent消费器、候选规则回传或远程写API；不存在自动学习、自动归纳或自动启用。当前认证事件服务仍仅只读事件，普通事件/Discord/日志不包含原名、目标名称、RSS标题、GUID或私密来源。

## NAS输入与执行边界

本机管理契约、JSON示例与前端交接见 [BATCH_FRONTEND_HANDOFF.md](BATCH_FRONTEND_HANDOFF.md)。`batch-preview`没有SQLite或媒体写入；`batch-confirm`重建当前job完整方案，核对job version、plan version和digest，再一个事务预约全部目标、追加plan_history/file_execution、人工作业记录及固定事件。预览取消没有成功样本。来源保留原名，媒体仅硬链接；不存在覆盖、删除、copy fallback。做种stop独立于命名和媒体入库。

每方案覆盖一个已下载任务的完整清单，最多512项/128视频：mkv/mp4；同目录同stem的srt/ass/ssa/vtt，最多两个安全语言后缀；现有受支持辅助文件只冻结指纹不入库。显式季号1..99、集号1..999；不支持特殊篇、非整数、多集单文件、混季或不明确字幕关联。逐行人工编号可非连续；从明确单编号提取的偏移范围-998..998；连续编号要求展示并显式确认用户提供的行序，服务端不按字典序推断剧情。

已开始计划禁止修改目标。每次恢复先整包指纹及目标预检，再依持久逐文件pending/intended/linked事实重放；链接成功但状态落盘前崩溃由同inode幂等硬链接恢复。不能因为一部分链接或Emby返回2xx而ready；全部视频Path确认和最终领域事务提交后才整包完成。

## 不可变人工意图

schema6只additive增加`comparison_records(job_id,plan_version,body)`及禁止UPDATE/DELETE的触发器。新样本与批准计划同事务，唯一(job,plan)，稳定sample_id=SHA256(jobID + ':' + planVersion + ':' + digest)。重试/双确认/重启不新增同一意图；未开始方案被替换时旧记录仍保留，动态状态为superseded。产品入口继续通过OpenLockedStore先取实例锁再打开/迁移；未知schema>6拒绝，失败事务回滚，不生产自动迁移或删除历史。

记录schema_version=1、origin=human、时间、job/plan/context/parser版本和digest；完整原清单/指纹；每入库文件原名→目标相对名、明确季集、源名称中可证明的原季集/总集、字段改动；批量组织的最终人工字段及编号方式/行序。字幕继承主视频映射，明确`inherited_video_mapping`，不伪称字幕自身提供编号证据。证据只含最多100个(subscription,title_hash)引用，不复制原始GUID、RSS标题、秘密URL或凭据；有需要时未来授权消费者按冻结哈希与context/parser版本在NAS本机解析字幕组等上下文。

每条changes新增`semantics_resolved`：新批量方案全部true。兼容旧单视频opaque目标时false，季集0表示未声明，UI应显示“季集未声明”；未来消费不得从0编造季集或据此生成已证实映射。旧单视频确认同样留下原名→目标意图；不会放宽新批量的明确季集门禁。

量有界：全库最多10000份人工记录、每条JSON最多1MiB、每页最多20份；达到限额整体拒绝新确认，不静默裁剪或删除。页游标为plan version，按单job递增。原名/目标及私密原始命名只存保护SQLite（目录0700/文件0600）和本机管理DTO，不进入普通事件/日志/Discord；本轮不导出到外部agent或模型。

`intent`永久不变；`files/status/error_code`是另读原(job,plan,index)持久事实的本机DTO投影。status=confirmed_intent、partial、awaiting_discovery、complete或superseded。完整成功要求全linked、全部视频discovered且当前同计划最终领域imported/discovery confirmed提交成功、无阻塞；部分效果、final事务失败和superseded不能称完整成功。完成后源被外部修改/删除、内核挂起、真实断电和真实Emby兼容仍是部署验收范围。

## 后续消费协议（设计，未实现）

消费者不能直写NAS SQLite、执行文件操作或通过事件API下命令。未来经独立授权的本机导出/专用接口才可读取必要隐私字段；默认只消费`status=complete`且全部`semantics_resolved=true`的记录，不把人工意图、部分失败或取消当成功训练样本。失败/特殊篇记录若被授权用作反例，应另带明确样本用途，不能与完成样本混淆。

样本幂等键：`sample_id`；消费者持久保存sample_id、record schema、digest和执行状态证据游标，重复交付只重放同样本。任务/计划版本不作为唯一全局ID；相同记录的后来执行进度不会改变sample_id或intent。每次建议显式引用`sample_id + digest + schema_version`和依据文件索引，禁止仅凭模糊标题或“高置信度”绕过人审。

未来回传候选应包含：

- `candidate_id`、不可变revision、candidate digest、producer/model/parser版本（未知必须标未知）；来源标记agent。
- `basis[]`样本ID/digest及逐文件对比依据，正例/反例分类，尚未知或冲突的事实；证据只可指向被授权可读快照。
- `proposed_rule`：订阅/字幕组/完整别名作用域、本地series_id/order、人工待确认season/directory/display_name、有界相对/总集映射；不得把篇章名或目录季号当新篇章季号授权。
- `applicability`与`exceptions`：准确文件格式、编号样式、订阅作用域、有界offset、已知特殊篇/多集/混季/字幕限制和无法推广的反例。
- `historical_dry_run`：固定证据版本的适用历史任务全集或声明有界范围；每任务完整清单预测结果、冲突/拒绝原因、与人工目标差异、未验样本数量；不能只挑支持规则的样本。试算没有文件或规则启用效果。

候选状态机：pending_review → approved / rejected；approved可因依据或契约变化进入invalidated。revision变化重新pending_review，历史依据/审阅意见不可改写。审批绑定候选ID、revision、digest、范围与试算版本，由本机有权人明确确认；置信度不改变状态机。最终人审前任何候选都不能启用，也不能执行本次命名。approved只是通过人工终审；实际启用仍需明确授权并遵守既有不可变规则版本/epoch门禁。rejected/invalidated禁止启用，重新归纳需新revision和再次人审。

候选落库、审批审计与激活的可靠事务关系、身份和权限、导出最小化/限额、消耗模型预算、撤销与失效策略，应在下一独立阶段评审实现。本轮没有相应表或路由，不向外假称该状态机已运行。
