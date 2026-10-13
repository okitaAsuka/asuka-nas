# 规则建议人工终审 UI · 2026-10-13

## 版本与边界

前端基线 `b8298f8f168356e47a90ba033fa5d1f532a1f99b`，独立分支 `codex/nas-rule-review-ui-20261013`。除合入父指定后端提交和更新STATUS说明外，独立前端修改限于 web、浏览器测试、verify-ui 入口及本文；不另改业务 Go/SQL/DTO/认证/CSP。未递归委派、合并 main、创建 PR 或部署。已在原前端HEAD `041420df3ad58489fe84d1a6e05ece7a571c8acd` 上 cherry-pick 后端 `82c525958faee8a37adb1e5fb727f64581a8f985`（本地同内容提交 `9ae64f9`）；范围 DTO 按父确认的 `scope_declaration` / `scope_review` 合同接入。

## 交互与可复用组件

命名规则页的“agent 待审建议”按创建游标分页，展示状态文字和范围；审核顺序为原始人工依据与来源、有限规则草稿、完整历史试算、人工批准/拒绝。

- `ProposalEditor`：字段/字节与范围校验、第一错误字段聚焦；固定候选 ID/样本集合，仅保存新 revision。人工有限范围必须显式选择并填写理由。
- `ProposalReview`：复用 `ReviewedRequestGate` 和全局单写门；代数、页面 generation、候选 revision/version 与完整 DOM 原值共同绑定。输入/change/reset、静默赋值、取消、导航或新候选不能接收旧 success/error。失败保留草稿，旧试算失效；重新读取或重新试算有明确入口。
- `reviewPages`：结果与覆盖各自每页20条，保留完整集合的可达性；`truncated` 明示覆盖有限。匹配、未匹配、范围外、冲突有文字；冲突非零禁止批准。
- 人工终审有明确勾选；人工声明但未验证集数单独列全量与数量并二次确认。批准请求精确携带成功试算的 revision/version/epoch/digest。批准或拒绝后重新 GET 持久状态再显示实际结果。
- 复用安全 `el`/textContent、`comparisonPanel`/`renderComparison`、响应式文件表和 CSS 设计变量；样本/解释/来源不转 HTML、不加载外部链接。页面标题及错误状态可聚焦，390px 表格转卡片。

## 本机验证（模拟布局，不能替代真实业务 E2E）

`node --check internal/nas/web/app.js`、Python 编译、gofmt 和 `git diff --check`。
`python3 -B tests/proposal_layout_browser.py`：实际 Chromium，模拟 API；验证 desktop/390px 无横向溢出、样本与模型恶意文本安全、45条结果和覆盖全部分页、truncated 提示、人工范围二次确认、静默赋值/取消、精确批准载荷和写后重新读取。内部截图 `/tmp/asuka-rule-mock-{desktop,mobile}.png` 实际目检，未上传。

真实 Go 1.26.9 已安装并确认；本机真实 SQLite 编译因 modernc.org/sqlite v1.50.1 下载返回 Forbidden 阻塞，未绕过或换源。未声称本机 `make verify`/真实 API 浏览器 E2E 通过。

## 生产 API 浏览器门与 CI

`TestRuleProposalBrowserLoop`/`tests/proposal_browser.py` 复用 `learningFixture`，真实 SQLite、生产 LearningHandler/Management、临时媒体文件、fake 模型/qB/Emby。覆盖人工记录→agent候选→新revision→试算→批准，强制验证 E01–12 人工声明、已验证E01/11个未验证集数和 E02 自动硬链接/发现；另外验证拒绝不启用、stale/历史、epoch恢复、冲突锁定、23个revision分页、双击、晚success/error、取消、键盘和390px。

`make verify-ui` 保留四组旧浏览器门并增加新门。已回收完整日志：`c36cc7fd46bac687fb998168720550e97f9993de` 的 [CI #18](https://github.com/okitaAsuka/asuka-nas/actions/runs/38371932356) 与 `9623b8007f279be07339f2c1291be7124be376db` 的 [CI #19](https://github.com/okitaAsuka/asuka-nas/actions/runs/38372444731) 均成功。两次为真实 Go 1.26.9，make verify 的格式/vet/race/build/5项Python预检与四组旧浏览器回归、新生产API浏览器门全部通过；#19还运行了独立模拟布局门。取消已送达批准后晚响应不显示成功、重读实际approved，以及生产学习候选中的恶意模型字符串均通过。最终自审修复后的候选CI将在交付回复绑定准确SHA。四组旧 Python/Go 浏览器文件保持未改。

## 自审修复

初始化放在所有审核类定义之后，避免顶层 await 下导航遇到未初始化模块状态。人工确认开关不改变保存内容快照，但所有决定入口重新核对快照；静默修改不会绕过授权。取消在途写仅使 UI 响应过期，提示用户重新读取服务实际状态，不假定后端撤销。新revision保存/批准/拒绝完成后重新读取并核对实际状态，不乐观显示成功。分页到首/末页使按钮不可用时将键盘焦点移至计数状态，避免焦点丢失。Go空slice可能返回null，范围集数按空集合安全显示；候选声明存在但试算缺失scope_review时拒绝接受试算。来源长文本使用断行避免窄屏溢出。

## 旧前端检查点未运行项（本轮解除）

旧前端 `041420d` 在后端增补到位前，生产E2E明确输出 `NOT RUN next E02`。本轮后端82c5259已集成，删掉所有scope条件跳过与NOT RUN分支；Go编译需要真实声明字段，浏览器需要真实scope_review DTO与完整verified/unverified集合，缺少合同或E02自动链不能完成直接失败。原“仍需父整合后端”阻塞已解除。


CI运行中的真实API桌面与390px截图只保存在runner临时目录，没有上传或取回；真实API窄屏通过Chromium无溢出断言，实际截图目检仅为本机模拟API布局。未访问真实NAS/真实模型/真实qB/Emby。

## 前后端集成强制验收 / 2026-10-13

本轮仅集成父指定后端提交，业务实现无另行修改。前端按冻结合同将声明理由校验收紧到512字节，并将proposal_scope_declared审计翻译为人工声明有限范围。

生产浏览器新增强制路径：E01人工原始记录→agent候选→未声明扩大范围失败且保留草稿→显式声明E01–12/理由→双击仅一次新revision→旧revision stale/不可批准→当前成功试算精确verified=[1]、unverified=[2..12]和理由→一般人工确认仍不能批准→未验证范围二次确认→双击批准/取消晚响应/重新读取actual approved→新E02无需人工确认自动硬链接及fake Emby发现。Go核对目标同inode、原E01冻结plan/人工意图不变、样本feed仍只有原E01且总下载恰好两次。

保存后的声明也覆盖真实API晚success/error矩阵：开关、理由、静默开关/理由、边界、reset、取消、导航和历史revision切换；均不能接收旧试算。513 UTF-8字节理由在前端定位阻止提交，符合后端512字节上限。自审修复静默赋值后的原值恢复：请求边界观察字段时同步编辑器去重基线，恢复已保存值重新开放试算；批准入口的静默声明开关/理由变更均不写入，恢复后仍需新试算及两次确认。保留拒绝、stale、epoch、冲突、23revision分页、不可信模型文本、键盘和390px，以及原四组浏览器回归。

本机模拟布局检查已通过。当前环境仍无预装SQLite模块缓存，既有现代SQLite下载Forbidden不重试/换源；真实生产浏览器与最终完整验证使用授权功能分支CI。集成最终SHA/CI链接及结果在最终交付回复绑定，不把旧前端CI当作本轮集成通过。

父提供并已完成的独立跨仓证据：NAS后端CI [38374145844](https://github.com/okitaAsuka/asuka-nas/actions/runs/38374145844)，agent `5e2d572293ed0074bdc5280caffe10edf18eafb0` CI [38374847569](https://github.com/okitaAsuka/asuka-agent/actions/runs/38374847569)；后端owner跨仓race smoke通过真实handler/worker、模型stub、ACK丢失重启只一次模型调用、人工声明1–12批准及E02实际硬链接/发现。这是父交接的实跑证据，当前前端环境没有重跑两仓smoke。

集成检查点 `353d998ad90451053c2b20bf981b2588a333635f` 的 [CI 38376206997](https://github.com/okitaAsuka/asuka-nas/actions/runs/38376206997) 已成功：make verify、模拟布局门、原四组浏览器回归和强制生产API范围审核→E02链全部通过。该运行包含保存后的声明晚响应矩阵、513字节校验、历史与重复点击；修复静默编辑恢复后的最终候选将再运行完整CI并在交付回复绑定唯一SHA。
