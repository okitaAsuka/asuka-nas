# 规则建议人工终审 UI · 2026-10-13

## 版本与边界

前端基线 `b8298f8f168356e47a90ba033fa5d1f532a1f99b`，独立分支 `codex/nas-rule-review-ui-20261013`。仅修改 web、浏览器测试、verify-ui 入口和本文；不修改业务 Go/SQL/DTO/认证/CSP。未递归委派、合并 main、创建 PR 或部署。父将回收后端最新提交再集成；范围 DTO 按父确认的 `scope_declaration` / `scope_review` 合同接入。

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

`TestRuleProposalBrowserLoop`/`tests/proposal_browser.py` 复用 `learningFixture`，真实 SQLite、生产 LearningHandler/Management、临时媒体文件、fake 模型/qB/Emby。覆盖人工记录→agent候选→新revision→试算→批准，后端人工范围提交集成后验证 E02 自动硬链接/发现；另外验证拒绝不启用、stale/历史、epoch恢复、冲突锁定、23个revision分页、双击、晚success/error、取消、键盘和390px。

`make verify-ui` 保留四组旧浏览器门并增加新门。已回收完整日志：`c36cc7fd46bac687fb998168720550e97f9993de` 的 [CI #18](https://github.com/okitaAsuka/asuka-nas/actions/runs/38371932356) 与 `9623b8007f279be07339f2c1291be7124be376db` 的 [CI #19](https://github.com/okitaAsuka/asuka-nas/actions/runs/38372444731) 均成功。两次为真实 Go 1.26.9，make verify 的格式/vet/race/build/5项Python预检与四组旧浏览器回归、新生产API浏览器门全部通过；#19还运行了独立模拟布局门。取消已送达批准后晚响应不显示成功、重读实际approved，以及生产学习候选中的恶意模型字符串均通过。最终自审修复后的候选CI将在交付回复绑定准确SHA。四组旧 Python/Go 浏览器文件保持未改。

## 自审修复

初始化放在所有审核类定义之后，避免顶层 await 下导航遇到未初始化模块状态。人工确认开关不改变保存内容快照，但所有决定入口重新核对快照；静默修改不会绕过授权。取消在途写仅使 UI 响应过期，提示用户重新读取服务实际状态，不假定后端撤销。新revision保存/批准/拒绝完成后重新读取并核对实际状态，不乐观显示成功。分页到首/末页使按钮不可用时将键盘焦点移至计数状态，避免焦点丢失。Go空slice可能返回null，范围集数按空集合安全显示；候选声明存在但试算缺失scope_review时拒绝接受试算。来源长文本使用断行避免窄屏溢出。

## 尚待父集成

截至前端最终候选制作，后端分支仍为基线 b8298f8，父已批准的人工范围DTO实现SHA尚未提供。当前生产E2E明确输出 `NOT RUN next E02: backend scope declaration commit not integrated`；不能把旧基线的单集规则批准等同于后续E02自动。前端声明、理由、试算区分与二次确认已在模拟接口检查，但真实声明保存/试算/批准→E02执行必须在父回收后端提交后重跑。本测试通过JSON tag识别新合同，并使用管理API制作分页revision，避免绑定后端新增方法签名。

CI运行中的真实API桌面与390px截图只保存在runner临时目录，没有上传或取回；真实API窄屏通过Chromium无溢出断言，实际截图目检仅为本机模拟API布局。未访问真实NAS/真实模型/真实qB/Emby。
