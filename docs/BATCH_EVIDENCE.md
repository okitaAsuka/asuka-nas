# 批量整理后端：父审证据 / 2026-10-13

基线`547d151b25c559841bcb68d743d8608bf26a6bbd`，工作树`/workspace/asuka-nas`，远端`okitaAsuka/asuka-nas`分支`codex/nas-batch-organizer-20261013`。精确最终HEAD见交付回复。没有main合并、PR、部署、真实NAS扫描/上游服务、凭据配置、外发或asuka-agent修改；没有子agent或另开reviewer。

用户新要求前端独立owner后，本owner停止web、浏览器测试及Makefile写入。已有前端草稿固定在交接提交`8fee28b3fcdb6cc094b7e5ce580e032f64363395`；后续后端提交不会修改这些文件。API/DTO交接见BATCH_FRONTEND_HANDOFF.md；新`changes.semantics_resolved`标志与限额补充见BATCH_AGENT_CONTRACT.md。最终整合、审美/组件复用评审和完整浏览器验收待父任务协调。

## 核心入口

| 文件 | 父审重点 |
| --- | --- |
| internal/nas/batch_organizer.go | 完整manifestGroups门禁→逐行明确季集/有限offset/显式顺序→原文件层，preview零写入，confirm重建摘要/CAS/原job-version重放绑定 |
| internal/nas/store.go | schema6 additive；batch_confirmed与plan_ready同预约/历史逻辑；comparison记录和job/event同一事务；unknown schema拒绝 |
| internal/nas/comparison_records.go | 样本稳定ID、原始意图不可变、证据只存引用、1MiB/10000/100/20限额；原计划执行投影不假报complete；旧opaque单视频季集明确未知 |
| internal/nas/frozen_evidence.go + engine.go | 冻结人工batch新增来源保守拒绝；旧单视频确认绑定全清单；复用execution_engine整包预检、逐文件恢复和独立做种 |
| internal/nas/management.go | loopback+Origin/CSRF保护的batch-preview/confirm和本机比较页；没有新增远程写路由或命名内容事件 |
| internal/nas/batch_organizer_test.go | 完整人工合集、目录/编号/字幕/特殊篇/遗漏/重复、全指纹与版本过期、跨job预约、事务回滚、链接后崩溃、partial/Emby/final事务、幂等、冻结/规则变更、限额/分页和迁移历史 |

## 自审与修复

- 不通过逐个单集API挑选合集：先验证完整清单、全部视频、字幕owner及所有行，再处理分组；辅助文件指纹同样进入摘要、confirm和执行。
- 对比记录在批准同一事务写入，外部链接尚未执行；双确认/重启只重放同plan/digest/organization和原job version，不制造成功样本。preview不预约、不写记录；中间部分状态仍独立。
- sample immutable与运行status分离，所有视频discovered并且最终领域事务成功前不能complete；旧未执行记录被替换仍保留、标superseded。
- 第一次CI准确暴露测试预约同job误用、旧opaque目标兼容回归；已改真实跨job冲突测试，并保留旧单视频unknown语义（semantics_resolved=false），新批量季集门禁保持严格，未放宽断言。
- 来源证据改有界distinct哈希引用，不复制GUID/原始标题；字幕before季集继承原主视频，避免把字幕缺编号误称人工修改；新增JSON字节上限；幂等重放额外绑定原job version。
- 没有规则学习/候选回传实现；未来契约强制pending_review→人工终审，任何置信度不可激活。规则变化和后续来源证据不能重写人工记录或迁移已开始目标。

## 已验与未验

本机真实Go1.26.9（官方发布SHA256 `42d158b4d8f7b61ac0a830567c940a86098fb7aac52e467a5ebec03ef5cc2f8d` 核验）可运行gofmt；SQLite/libc正常官方代理下载Forbidden，未绕过，所以make verify/verify-ui在依赖加载阶段停止。这不是业务测试/浏览器断言通过或失败。JS语法/Python编译/diff whitespace及五组Python预检通过。

第一次分支CI `8fee28b`：[run 38343108391](https://github.com/okitaAsuka/asuka-nas/actions/runs/38343108391)失败，以上两项已修复。修复候选`81a8d79f22711b7da7535c931e958fbca20d2b76`：[run 38343491580](https://github.com/okitaAsuka/asuka-nas/actions/runs/38343491580)，真实Go1.26.9/Python3.12的make verify已success；完整make verify-ui及本文件后续后端最终候选结果以交付回复/最后证据更新为准。

真实NAS/RSS/qB/Emby/Discord、真实断电、权限/挂载、多租户与恶意并发修改、内核永久挂起I/O未验。当前浏览器脚本只是草稿，前端owner应以生产API/fake回环HTTP跑实际Chromium；不能以构建成功代替交互验收。依父任务最新指令，最后整体CI/UI验收留待前端候选集成，不假称已完成。
