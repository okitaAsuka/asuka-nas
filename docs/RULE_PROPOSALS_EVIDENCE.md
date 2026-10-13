# 规则提议后端接口检查点 / 2026-10-13

这是供独立前端接入的检查点，父要求优先交接，不声称两仓完整里程碑已验收。前端/浏览器/Makefile/CI未写，两个仓库分支均codex/nas-rule-proposals-20261013；最终回复给出SHA和已触发CI状态。

核心路径：learning_samples.go保守成功投影/冻结来源/seq重放；rule_proposals.go有限schema/不可变revision/有界历史试算/人工事务批准；learning_service.go独立Bearer与本机CSRF路由；rules_store.go仅抽addRuleTx/setRuleTx复用既有manual存储、epoch和审计。schema6→7最小additive表/trigger，生产未迁移；未知>7拒绝。没有模型或外部文件执行能力移入NAS。

自审修复：不适格记录不能堵住feed前100条，learning_scans以记录ID+当前job version标记扫描；以后状态改变重新适格。只在成功时分配seq，pending不会被cursor跳过。当前样本必须仍保持原plan/context/digest和完整逐文件事实；新规则不改原样本。人工审批原子复用既有规则enable事务；审计插入失败全回滚。重复审批须与保存的请求摘要完全一致。历史试算上限100样本、2048结果、1000当前身份规则，超必要依据上限拒绝，普通历史截断显式truncated。模型目录/季集必须逐样本试算命中人工目标，连续范围每一集都要观察依据，不能从单例推广无限offset。

本机：官方Go1.26.9归档SHA256 42d158b4d8f7b61ac0a830567c940a86098fb7aac52e467a5ebec03ef5cc2f8d核对；使用预装GOMODCACHE，Go按go.sum核验，未绕过Forbidden/切换依赖源。

定向NAS race检查PASS：TestRuleProposalHumanOnlyAtomicApproval、TestRuleProposalRejectStaleAndRevision、TestLearningAuthorizationPrivacyAndSchema、TestProposalManagementDTO、TestRejectFutureSchema。真实SQLite/临时硬链接/fake qB和Emby，验证未执行/待发现不导出、成功来源、人工意图保持、单例范围门、修改/拒绝/过期、事务失败回滚、并发重复审批只有一个epoch、Bearer隔离、未知脚本字段拒绝、真实管理DTO路由与CSRF批准。未改既有业务断言规避问题；三处schema断言因schema7升级改为精确7及未来版本8。

第一次本机make verify在旧TestRejectFutureSchema把7当未来版本时FAIL；已修正为8并通过定向检查。完整最终验证转分支CI，未把第一次失败写成通过。本检查点没有运行新前端；既有UI属于基线已验收。agent定向race PASS：naslearning、runtime、config；新worker测试真实保护SQLite+fake NAS HTTP+模型stub，ACK丢失/重启/并发仅一次模型、owner锁、模型结果未知崩溃不自动付费重跑、累计预算/attempts和非法输出边界。

明确续轮：
1. 运行跨仓fake端到端（真实两仓handler/worker→模型stub→pending→人工API批准→后续新任务自动映射），不是两个仓各自mock测试替代。
2. 加厚当前协议version/digest/过期epoch、新冲突、无规则/非法输出/模型错误/预算、模型请求未知失败及敏感日志的必要场景；复核所有固定错误与事件UI可见性，限定本轮范围。
3. 两仓最终完整CI与准确SHA验证。现有CI未专门接跨仓新协议，两仓最终检查可由续轮按既有模式接入。
4. 独立前端owner完成合同交互、真实浏览器、桌面/窄屏目检；父最终主审集成。后端不写前端或共享入口，需变更由父协调。

剩余产品限制：单模型单样本归纳，字段必须用既有手动命名规则表达且范围限于已观察连续集数；不自动合并多样本扩范围。含URL/常见敏感标识、超过32项/16来源的对比保守不导出，不假称可学习所有合集。历史试算只覆盖已导出成功feed，不保证全部库或未来世界适用。真实模型/Discord/NAS/qB/Emby/TLS/跨机部署未验，默认关闭未配置新凭据，未main合并/PR/部署。
