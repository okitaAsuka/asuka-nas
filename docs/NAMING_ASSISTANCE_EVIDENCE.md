# 命名辅助后端验收证据 / 2026-10-13

## 候选与边界

NAS基线 e4bdde7e53ffc397580526e6b78314c52e8fc6cd；agent基线 5e2d572293ed0074bdc5280caffe10edf18eafb0。均fetch并逐字核对，再新建 codex/nas-naming-assistance-20261013。本文件随最终候选提交；精确最终SHA由交付报告返回，不在自身提交中循环引用。

只修改Go后端/对应测试、独立smoke脚本、NAMING_ASSISTANCE_HANDOFF/EVIDENCE。未修改NAS web、浏览器测试、Makefile、Dockerfile、deploy、CI、README/STATUS/config.example。未合并其它分支，包括packaging 41be7726bb2b8900bf1ca77761773ef335215a1d。未部署、未创建凭据、未访问真实NAS/qB/Emby/Discord/LLM，无main merge或PR。checkout和/workspace中没有可读的.agents/skills目录；读取两仓AGENTS及必要工作流/设计规则后执行；未递归委派。

实现层：NAS naming_assistance（快照/有限DTO/SQLite）、naming_assistance_review（batch共用/审批事务/来源）、naming_assistance_http（既有服务和人工管理适配）；agent naslearning/naming_* 复用既有Store、owner、ModelClient、预算和事件。没有新文件执行器。纯人工与model_assisted_human不可变Comparison始终区分；后者不进入learning。

## 实际验证

工具链真实输出 `go version go1.26.9 linux/amd64`。使用 `/workspace/asuka-cloud/activate.sh`，GOPATH=/workspace/asuka-cloud/go、GOTOOLCHAIN=go1.26.9；修复了工具链sumdb在默认home路径不可写的环境问题，没有降版本/篡改go.mod。

| 命令 | 结果 |
|---|---|
| NAS `make verify` | PASS：gofmt、全仓vet、全仓race tests、build、5项既有Python预检安全测试；最终运行领域包62.191s |
| agent `go test -race -count=1 ./...` | PASS：42个有测试包，另含无测试包 |
| agent `go vet ./...` / `go build ./...` | PASS |
| NAS `go test -race ./internal/nas -run '^TestNaming' -count=1` | PASS；全检后追加来源/schema7保留/离线做种测试另作定向复验 |
| agent `go test -race ./internal/apps/gateway/naslearning -count=1` | PASS；最终命名边界及原learning测试复验 |
| agent `python3 scripts/nas-naming-smoke.py --nas-repo /workspace/asuka-nas` | PASS；启动两仓真实测试服务/handler/worker，均race，8场景 |
| agent `python3 scripts/nas-rule-proposals-smoke.py --nas-repo /workspace/asuka-nas` | PASS；原纯人工E01→待审规则→人工范围1–12→E02自动硬链接/发现仍工作 |
| 两仓 `git diff --check` | PASS |

新命名smoke真实摘要：

```text
TestCrossRepoNaming/primary, offline, rejected, stale, conflict, unresolved, budget, invalid: PASS
PASS naming real worker + model stub once + durable SQLite result before ACK + offline/restart/budget + human NAS current preview and provenance
PASS real NAS SQLite handlers + full media manifest -> agent budget/model stub -> durable result/lost ACK/both restarts -> human current preview -> actual hardlinks/fakeEmby
```

primary含2视频+同stem字幕+辅助文件，冻结完整4项清单、实际3个硬链接；逐项os.SameFile验证，保留源文件。第一次POST结果在NAS提交后注入503丢ACK；agent提交待发数据/摘要已持久，两仓reopen后仅重放同一结果，模型调用数仍1、NAS只有1份结果/1份model_assisted_human记录。人通过真实管理Origin/CSRF请求当前preview，再批准；fakeEmby返回实际目标路径后才标全包confirmed。服务身份即使带nonce仍403。

offline在请求已建立后服务503，调用预算0；agent关闭/reopen后恢复，一次模型调用后人工终审，完成实际硬链接/发现。额外NAS测试证明pending请求不拖住做种deadline：到期stop改变任务版本后旧快照拒绝，人显式刷新再完成同任务，seed始终stopped。

stale在真实stub模型返回前修改NAS来源上下文，结果409并终止，无重启重复付费。conflict在成功preview后制造他人目标，confirm拒绝，不入库。rejected同结果重放ACK维持拒绝，不重推模型。unresolved不猜目录/季号，不生成执行方案。budget人显式第二份请求后总预算已满，重启也不增加调用。invalid模型脚本/未知字段被strict DTO拒绝。

补充单测覆盖：调用在途calling→重启interrupted，必须显式retry且保留累计calls/attempts；完整清单/上下文/epoch/内容指纹过期、重复文件/缺项、未知季号、特殊篇/不支持清单、秘密URL和绝对上下文路径、已有规则优先、最新preview替换旧digest、每任务3份请求上限、SQLite失败回滚批准+预约+对比、拒绝后普通batch不能绕过当前preview、旧单文件人工独立修订也保留模型来源、schema7升级逐字保留原任务/Comparison并继续导出纯人工learning。事件和模型输入检查无GUID/URL/token/绝对目录。

既有断言调整仅因开发schema7→8：batch/legacy迁移测试预期改8；future-schema拒绝测试改9。未绕过或删安全断言，未改浏览器测试。

## 剩余交接

无后端执行阻塞。前端尚未接入新命名面板/事实询问/编辑失效/拒绝与显式retry；合同在 NAMING_ASSISTANCE_HANDOFF.md。本轮未运行浏览器交互或声明UI验收。配置样例/部署打包由已停止的packaging owner成果交父整合，本分支只新增Go中默认关闭nas_naming配置及CLI，NAS继续复用默认关闭learning回环服务。最终整合CI/真实环境验收由父安排。

单任务限制32文件、最多16标题、单一明确季号，只有人工启动；缺媒体目录/显示名或明确季号时unresolved。普通状态推进也使严格job_version过期，显式申请当前快照受3份上限，不静默反复推理。未来范围必须由人声明并走既有规则试算/启用，不能自动从本次模型结果推广。agent总预算1–1000，每项1–3次（每任务最多3请求，即最多9次且仍受全局总额度），超时1–60秒，输出256–4096tokens/32768字节。NAS领域schema8仅未部署开发库，不授权生产迁移。
