# 规则提议后端接口检查点 / 2026-10-13

这是供独立前端接入的检查点，父要求优先交接，不声称两仓完整里程碑已验收。前端/浏览器/Makefile/CI未写，两个仓库分支均codex/nas-rule-proposals-20261013；最终回复给出SHA和已触发CI状态。

核心路径：learning_samples.go保守成功投影/冻结来源/seq重放；rule_proposals.go有限schema/不可变revision/有界历史试算/人工事务批准；learning_service.go独立Bearer与本机CSRF路由；rules_store.go仅抽addRuleTx/setRuleTx复用既有manual存储、epoch和审计。schema6→7最小additive表/trigger，生产未迁移；未知>7拒绝。没有模型或外部文件执行能力移入NAS。

自审修复：不适格记录不能堵住feed前100条，learning_scans以记录ID+当前job version标记扫描；以后状态改变重新适格。只在成功时分配seq，pending不会被cursor跳过。当前样本必须仍保持原plan/context/digest和完整逐文件事实；新规则不改原样本。人工审批原子复用既有规则enable事务；审计插入失败全回滚。重复审批须与保存的请求摘要完全一致。历史试算上限100样本、2048结果、1000当前身份规则，超必要依据上限拒绝，普通历史截断显式truncated。模型目录/季集必须逐样本试算命中人工目标，agent连续范围每集都要观察依据；人工可明确声明最多32集的有限范围并显示未验证部分，不能从单例自动推广无限offset。

本机：官方Go1.26.9归档SHA256 42d158b4d8f7b61ac0a830567c940a86098fb7aac52e467a5ebec03ef5cc2f8d核对；使用预装GOMODCACHE，Go按go.sum核验，未绕过Forbidden/切换依赖源。

定向NAS race检查PASS：TestRuleProposalHumanOnlyAtomicApproval、TestRuleProposalRejectStaleAndRevision、TestLearningAuthorizationPrivacyAndSchema、TestProposalManagementDTO、TestRejectFutureSchema。真实SQLite/临时硬链接/fake qB和Emby，验证未执行/待发现不导出、成功来源、人工意图保持、单例范围门、修改/拒绝/过期、事务失败回滚、并发重复审批只有一个epoch、Bearer隔离、未知脚本字段拒绝、真实管理DTO路由与CSRF批准。未改既有业务断言规避问题；三处schema断言因schema7升级改为精确7及未来版本8。

第一次本机make verify在旧TestRejectFutureSchema把7当未来版本时FAIL；已修正为8并通过定向检查。完整最终验证转分支CI，未把第一次失败写成通过。本检查点没有运行新前端；既有UI属于基线已验收。agent定向race PASS：naslearning、runtime、config；新worker测试真实保护SQLite+fake NAS HTTP+模型stub，ACK丢失/重启/并发仅一次模型、owner锁、模型结果未知崩溃不自动付费重跑、累计预算/attempts和非法输出边界。

明确续轮：
1. 运行跨仓fake端到端（真实两仓handler/worker→模型stub→pending→人工API批准→后续新任务自动映射），不是两个仓各自mock测试替代。
2. 加厚当前协议version/digest/过期epoch、新冲突、无规则/非法输出/模型错误/预算、模型请求未知失败及敏感日志的必要场景；复核所有固定错误与事件UI可见性，限定本轮范围。
3. 两仓最终完整CI与准确SHA验证。现有CI未专门接跨仓新协议，两仓最终检查可由续轮按既有模式接入。
4. 独立前端owner完成合同交互、真实浏览器、桌面/窄屏目检；父最终主审集成。后端不写前端或共享入口，需变更由父协调。

剩余产品限制：单模型单样本归纳，字段必须用既有手动命名规则表达且agent范围限于已观察连续集数；人工可显式声明最多32集并显示未验证部分，不自动合并多样本扩范围。含URL/常见敏感标识、超过32项/16来源的对比保守不导出，不假称可学习所有合集。历史试算只覆盖已导出成功feed，不保证全部库或未来世界适用。真实模型/Discord/NAS/qB/Emby/TLS/跨机部署未验，默认关闭未配置新凭据，未main合并/PR/部署。


## 后端续轮完成 / 2026-10-13

父确认最小人工范围DTO并协调独立前端后，新增scope_declaration（仅管理revise）和trial.scope_review。旧候选正文不改，声明新revision/audit；单次最多32集、与rule.episodes/absolute严格一致。遗漏声明不继承，篡改已观察偏移拒绝。原样本意图/plan摘要保持。服务Bearer连同有效管理nonce也不能调用审核入口。

跨仓实际PASS命令：
`python3 /workspace/asuka-agent/scripts/nas-rule-proposals-smoke.py --nas-repo /workspace/asuka-nas --go /workspace/toolchains/go1.26.9/bin/go`。
两侧race、真实production learning/management handler和worker、各自保护SQLite与真实临时硬链接；模型只有stub，qB/Emby只有本机fake HTTP。断言E01成功→模型一次→提交落库后ACK丢失→agent重开/并发重放同摘要→只有一份pending→人声明E01–12→试算verified=[1]/unverified=[2..12]→旧试算不能批准新revision→人批准及幂等重试→新torrent任务E02自动映射/硬链接/全部发现，无第二次人工确认/新人工样本/重复下载。拒绝保持epoch0且不重跑模型；模型执行中来源变更时提交拒绝，agent终态stale且NAS没有候选。普通事件/Notice没有标题/秘密URL/GUID/凭据。

新增定向race PASS：ProposalHumanScopeExplicitAndBounded、ProposalHumanScopeAbsoluteAndAuthorization、ProposalHumanScopeTotalOffsetPreservesObservedMapping、ProposalScopeEpochAndNewConflictRejectApproval。涵盖声明遗漏/不匹配/33集拒绝、有限总集67–78→相对1–12/界外79拒绝、已观察偏移不得变化、有效nonce+Bearer拒绝、epoch变化与新身份冲突导致批准失败且没有规则落库。agent终态测试覆盖no_rule、非法script字段、模型原始秘密错误不入事件、推理前stale零模型调用、重启不重消费与累积预算拒绝重试；provider fake HTTP核对max_completion_tokens与无tools字段。

原检查点两仓完整CI已确认成功：NAS run38370075296、agent run38370118503。最终增补提交两仓完整CI由最终回复给出准确SHA/链接/状态；本轮不改Makefile/CI。跨仓smoke为显式独立入口，既有CI默认跳过opt-in测试，不能将常规CI成功替代跨仓实跑证据。

前述“明确续轮”中的跨仓与后端必要邻域已完成；后端最终完整CI仍依最终回包状态。剩余验收归父：独立前端交互/浏览器/视觉与集成最终主审。真实模型、NAS/qB/Emby/Discord、部署及跨机监听未验，开发无真实凭据/外发。独立前端不会被本后端修改覆盖，未写web、浏览器测试或共享入口。

本机两仓make verify通过（NAS race/vet/build/preflight，agent Go全集/vet），之后补了未知原始记录schema/origin拒绝门并通过定向race；最终全部源码以新SHA的完整CI为准。未修改既有断言掩盖问题，前端与共享入口未动。
