# 命名辅助后端验收证据 / 2026-10-13

## 候选与边界

NAS基线 e4bdde7e53ffc397580526e6b78314c52e8fc6cd；agent基线 5e2d572293ed0074bdc5280caffe10edf18eafb0。均fetch并逐字核对，再新建 codex/nas-naming-assistance-20261013。本文件随最终候选提交；精确最终SHA由交付报告返回，不在自身提交中循环引用。

只修改Go后端/对应测试、独立smoke脚本、NAMING_ASSISTANCE_HANDOFF/EVIDENCE。未修改NAS web、浏览器测试、Makefile、Dockerfile、deploy、CI、README/STATUS/config.example。未合并其它分支，包括packaging 41be7726bb2b8900bf1ca77761773ef335215a1d。未部署、未创建凭据、未访问真实NAS/qB/Emby/Discord/LLM，无main merge或PR。checkout和/workspace中没有可读的.agents/skills目录；读取两仓AGENTS及必要工作流/设计规则后执行；未递归委派。

实现层：NAS naming_assistance（快照/有限DTO/SQLite）、naming_assistance_review（batch共用/审批事务/来源）、naming_assistance_http（既有服务和人工管理适配）；agent naslearning/naming_* 复用既有Store、owner、ModelClient、预算和事件。没有新文件执行器。纯人工与model_assisted_human不可变Comparison始终区分；后者不进入learning。

## 实际验证

工具链真实输出 `go version go1.26.9 linux/amd64`。使用 `/workspace/asuka-cloud/activate.sh`，GOPATH=/workspace/asuka-cloud/go、GOTOOLCHAIN=go1.26.9；修复了工具链sumdb在默认home路径不可写的环境问题，没有降版本/篡改go.mod。

| 命令 | 结果 |
|---|---|
| NAS `make verify` | PASS：gofmt、全仓vet、全仓race tests、build、5项既有Python预检安全测试；语义/自动增补全量运行领域包69.321s；随后人工fallback补丁的最终全量正在运行，结果由后续证据提交补记 |
| agent `go test -race -count=1 ./...` | PASS：42个有测试包，另含无测试包 |
| agent `go vet ./...` / `go build ./...` | PASS |
| NAS `go test -race ./internal/nas -run '^TestNaming' -count=1` | PASS；全检后追加来源/schema7保留/离线做种测试另作定向复验 |
| agent `go test -race ./internal/apps/gateway/naslearning -count=1` | PASS；最终命名边界及原learning测试复验 |
| agent `python3 scripts/nas-naming-smoke.py --nas-repo /workspace/asuka-nas` | PASS；启动两仓真实测试服务/handler/worker，均race，最终已验12场景 |
| agent `python3 scripts/nas-rule-proposals-smoke.py --nas-repo /workspace/asuka-nas` | PASS；原纯人工E01→待审规则→人工范围1–12→E02自动硬链接/发现仍工作 |
| 两仓 `git diff --check` | PASS |

新命名smoke真实摘要：

```text
TestCrossRepoNaming/primary, offline, rejected, stale, conflict, unresolved, budget, invalid: PASS
PASS naming real worker + model stub once + durable SQLite result before ACK + offline/restart/budget + human NAS current preview and provenance
PASS real NAS SQLite handlers + full media manifest -> agent budget/model stub -> durable result/lost ACK/both restarts -> human current preview -> actual hardlinks/fakeEmby
```

primary含2视频+同stem字幕+辅助文件，冻结完整4项清单、实际3个硬链接；逐项os.SameFile验证，保留源文件。第一次POST结果在NAS提交后注入503丢ACK；agent提交待发数据/摘要已持久，两仓reopen后仅重放同一结果，模型调用数仍1、NAS只有1份结果/1份model_assisted_human记录。人通过真实管理Origin/CSRF请求当前preview，再批准；fakeEmby返回实际目标路径后才标全包confirmed。服务身份即使带nonce仍403。

offline在请求已建立后服务503，调用预算0；agent关闭/reopen后恢复，一次模型调用后人工终审，完成实际硬链接/发现。额外NAS测试证明pending请求不拖住做种deadline：到期stop改变任务版本后原快照/结果仍有效，当前preview/人工批准恢复同任务，模型调用/请求份数不增加，seed始终stopped。

stale在真实stub模型返回前修改NAS来源上下文，结果409并终止，无重启重复付费。conflict在成功preview后制造他人目标，confirm拒绝，不入库。rejected同结果重放ACK维持拒绝，不重推模型。unresolved不猜目录/季号，不生成执行方案。budget人显式第二份请求后总预算已满，重启也不增加调用。invalid模型脚本/未知字段被strict DTO拒绝。

补充单测覆盖：调用在途calling→重启interrupted，必须显式retry且保留累计calls/attempts；完整清单/上下文/epoch/内容指纹过期、重复文件/缺项、未知季号、特殊篇/不支持清单、秘密URL和绝对上下文路径、已有规则优先、最新preview替换旧digest、每任务3份请求上限、SQLite失败回滚批准+预约+对比、拒绝后普通batch使用独立当前人工preview成功授权，保留原建议/来源且不进入learning、旧单文件人工独立修订也保留模型来源、schema7升级逐字保留原任务/Comparison并继续导出纯人工learning。事件和模型输入检查无GUID/URL/token/绝对目录。

既有断言调整仅因开发schema7→8：batch/legacy迁移测试预期改8；future-schema拒绝测试改9。未绕过或删安全断言，未改浏览器测试。

## 剩余交接

无后端执行阻塞。前端尚未接入新命名面板/事实询问/编辑失效/拒绝与显式retry；合同在 NAMING_ASSISTANCE_HANDOFF.md。本轮未运行浏览器交互或声明UI验收。配置样例/部署打包由已停止的packaging owner成果交父整合，本分支只新增Go中默认关闭nas_naming配置及CLI，NAS继续复用默认关闭learning回环服务。最终整合CI/真实环境验收由父安排。

单任务限制32文件、最多16标题、单一明确季号。支持人工启动和默认关闭learning.automatic_naming opt-in；缺资料时unresolved。job_version仅采集审计，正交进展不使命名请求失效；preview绑定当前必要版本。未来范围必须由人声明并走既有规则试算/启用，不能自动从本次模型结果推广。agent总预算1–1000，每项1–3次（每任务最多3请求，即最多9次且仍受全局总额度），超时1–60秒，输出256–4096tokens/32768字节。NAS领域schema8仅未部署开发库，不授权生产迁移。


## 产品增补与人工fallback检查点

在首版已推送NAS 3f0b9333be3b298bd9f48f51a7fd5b7bd5445b04 / agent f55e9f777b1d51098bc77a3f6561b62f8c8c2de4之上连续修复，不更换writer、不合并前端e3d3ef610f626b2de8298e1b682044cd48857652或packaging。环境disconnected通知后，2026-10-13T12:01:48Z两仓实际只读命令均返回exit0，确认仍可执行；没有将通知后未返回的命令当成功。

DTO差异已在HANDOFF冻结：snapshot.semantic_digest/plan_digest/trigger；job_version仅审计；Plan.naming_review区分专用命名审核和全新普通人工授权。Hints/请求来源仍由完整digest绑定。提交/preview/专用批准在事务中验证命名不变量；seed/stop/attempts/错误不使原结果过期。

默认关闭的自动辅助接入真实Daemon.TickJobs，先完成原规则/下载/做种处理。每tick最多8项、持久扫描游标、每任务3份/全NAS10000份、每语义snapshot唯一自动索引；worker原预算保持持久且无真实环境启用。拒绝/unresolved停自动，人工可补充不同hints并显式retry（旧项原子标superseded审计），或直接人工编辑preview无需新推理。特殊篇/不支持/已匹配规则/人工计划不入队；追加定向race验证8份坏上下文不会饿死后面的3份可支持任务。

新增真实跨仓场景：

- seed_progress：真实stub模型调用期间NAS完成QB stop，任务版本确实增加；原结果仍持久成功，当前preview批准、实际硬链接/fakeEmby发现完成；请求1、模型calls1。
- automatic：没有人工naming-request，配置opt-in的Daemon创建请求；无资料模型返回unresolved；NAS与agent重启、反复tick及seed stop都无新请求/推理；人补充BatchOrganization后终审同一结果，硬链接/fakeEmby完成；请求1、calls1。
- rejected：原建议人工拒绝后走真实batch-preview/confirm重新明确授权，导入成功，原rejected状态/不可变结果保留；请求1、calls1。
- manual_stale：已收到建议后新增NAS来源上下文，建议显示stale；人以当前完整清单做普通batch预览/批准，导入成功；请求1、calls1，原建议未改。
- manual_offline：辅助服务503、模型未调用，人直接普通batch整理成功；agent恢复后旧请求校验stale、模型仍calls0；请求1。记录保守为model_assisted_human，无模型结果时result_digest明确为空。

全部12场景race PASS，生产handler/SQLite/worker、真实临时硬链接/fakeEmby均实际完成；以上人工fallback都被断言不进入纯人工learning。新增NAS五类定向race还覆盖拒绝、上下文/规则/指纹过期和离线，人工preview之后再次改写仍拒绝，重预览可恢复，无新模型请求。agent新增语义投影测试确认审计job版本可变化，人工计划摘要被篡改则拒绝。原纯人工E01→待审有界规则→人终审→E02自动路径再次跨仓PASS。

既有断言调整：首版TestNamingOfflineDoesNotHoldSeedDeadline的“旧版本应拒绝”改为本次明确要求的“正交seed进展原结果仍有效且请求1”；manual_provenance原“普通batch应拒绝”改为“当前人工preview可独立批准、不可变原建议/模型来源保留且learning空”。没有绕开断言或删安全覆盖。
