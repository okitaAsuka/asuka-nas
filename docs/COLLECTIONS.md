# N3/N4：有界映射与合集恢复

本阶段仅本地规则、fake HTTP、临时文件。所有第四季、67→1 等数字都是合成 fixture 中人工确认的配置，不是现实作品元数据。没有元数据联网、模型、Discord、真实 NAS 或远端推送。

## 确认规则

在 NAMING.md 的人工规则上增加：

```json
{
  "episodes": {"start": 1, "end": 11},
  "absolute": {"start": 67, "end": 77, "relative_start": 1}
}
```

这两个字段可选。episodes 是明确相对范围；absolute 是明确有限映射，不能外推。仅有 absolute 时相对范围由其长度和 relative_start 得到。series_id/provider/order/season/目录仍必须人工明确。改变映射要新增规则 revision 并显式 enable；完整旧计划保留。

ParseTitle v2 支持实体编码、中文第四季、相对 01–11、总第67–77。每个视频必须有自身编号，不从文件顺序分配。相对编号在界内优先；显式“总第”同时出现必须与相对集一致。只有无显式季号、无总第标记的裸整数，落在相对范围外而绝对范围内时，才按已确认有限映射解释为总集数。例如相对 1–11、绝对67–77下的 `[67]` 可以唯一得到第1集；S04E67 不作这种转换。

标题给出的范围必须和它的总集证据一致；整个清单的集号集合必须恰好覆盖标题范围。缺集、重复集、编号缺失、超界、特别篇、小数集、多集合一文件、来源决定冲突均整包阻塞。当前不支持 S00 特别篇或小数集。一个合集只有一个包含多集的视频也不能批准。

最多 512 个清单项、128 个视频；支持 mkv/mp4。字幕仅同目录、同 stem 的有限 suffix；必须唯一属于一个视频。来源辅助文件不入库，但必须真实存在、是普通文件并通过指纹预检。共享下载目录里不在 manifest 的文件不被吸收。

## 状态及恢复

```text
pending -> 完整清单/映射/指纹/目标预检
        -> planned + plan_history + 全目标预约 + file_execution(pending)
        -> import_started（规则 epoch + job CAS，冻结计划）
每个文件: pending -> intended（事务及事件）-> 安全 link -> linked（事务及事件）
全部 linked -> imported -> refresh_requested
每个视频: 精确 Path 发现 -> video_discovered（持久）
全部视频发现 -> discovery=confirmed + discovered（ready 事件）
```

`ready` 是派生条件：imported、discovery confirmed 且没有 block_reason；discovered 是历史完成事实。晚到矛盾另发 late_mapping_conflict，不能因过去 ready 就忽略新证据。所有阶段都保持做种截止独立，不删除源文件或已成功硬链。

每次恢复完整检查源指纹与目标；先写 intent 再 link，link 成功而数据库提交失败时留在 intended。重启继续固定 plan，用同 inode 幂等链接恢复，不改成新规则目标。linked 记录也重新验证安全幂等链接，避免外部删除导致虚假成功。遇中间失败保持 planned，不发 imported。所有视频逐一查询 Emby，已确认的视频持久化，不重复查询；字幕不参与视频发现计数。

## SQLite v4 审查结论

- `target_reservations(target PRIMARY KEY, job_id, plan_version)`：整包在 plan_ready 事务预约；同时检查文件/目录前缀冲突。另一 job 即使使用同一 inode 也不能占用同目标。过期未执行方案的预约在显式 replan 时释放；执行后的预约保留。
- `file_execution PRIMARY KEY(job_id,plan_version,file_index)`：状态只能 pending/intended/linked；更新使用预期旧状态条件，和 job CAS、事件在同一事务提交。
- events 新增可空 plan_version/file_index；旧游标和旧事件保持。消费者按事件 id 幂等，逐文件事件可对应精确方案与序号；不承诺跨外部效果 exactly-once。
- 旧数据库迁移在事务中增加表/列，回填当前计划预约、执行记录及缺失的 plan_history；已导入记 linked，未导入从 pending 安全重放。不移动或创建媒体文件。历史重复目标导致迁移失败并回滚，禁止静默挑一个所有者。测试确认失败后 user_version 仍3、原 jobs 不变、新表不存在。未知未来 schema 拒绝打开。
- 事务无法包含真实硬链接效果；可重放意图、同 inode 校验和 fsync 是恢复依据，不把这称为原子整包文件操作。

## 独立审查修复

基线 dcc97ca 的独立报告由 Library `libfile_4686f4abe4f881919c9c3feeddb7c84e` 物化，原样复制独立测试，未改其断言。修复前在当前工作树复现 F1/F2/F4/F5；F3 已被本轮未解释绝对证据门禁阻塞。修复后原始独立测试及新增变体通过。

- F1 成立：保留首括号作为作品名候选，识别别名后完整 SxxExx token；已知身份但格式/group 不明确时 rule_scope_ambiguous，不能 fallback 绕过停用。
- F2 成立：小数、未支持连接编号和超长编号不会截断为整数。保留正常整数、扩展名、720p/480p/1080p 的对照。
- F3 成立：无绝对映射的证据阻塞；有界映射时总集/相对证据都验证，矛盾不会被优先级覆盖。
- F4 成立：冻结校验只使用 RuleRefs 指定的历史规则，不使用 naming_active。相同标题的新 GUID 不因未来目录变更而阻塞；真实新矛盾仍阻塞。旧误报可在冻结证据重新验证成功后清除，不改目标。
- F5 成立：人工计划冻结目标 identity/season/episode 和原始上下文摘要；新增证据与之比较。没有可提取语义的任意目标，对新增未解释证据保守阻塞；不能以 nil 当一致。

最新实现未得到另一位审阅者重新审查；本轮另做 SQL/状态机人工自查及模拟测试，不宣称生产就绪。真实服务兼容、断电、文件系统/权限环境及性能验收仍待部署阶段。权限失败测试是在文件边界注入 EACCES 等价错误，非实际 NAS 权限验收。

后续复验修复：schema v5 的迁移已改为调用运行时 reservePlanTx，目标前缀冲突同样回滚；旧计划证明记录放在独立 legacy_plan_evidence 表，不修改原计划。解析器 v3 先验证完整数字 token，NFKC 前拒绝分数/上标等不支持的数字语义。特殊篇在 RSS 和全部 manifest 项统一阻塞。新旧边界及复验证据见 REVIEW.md 末节。
