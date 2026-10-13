# 批量工作台前端证据 / 2026-10-13

## 基线与边界

从 `8a30caa44e500af1a859e1d30373ec6164182203` 建立 `codex/nas-batch-workbench-ui-20261013`。独立前端 owner 单会话实现、自审，没有递归委派或 reviewer。写入范围仅 web、浏览器测试、本证据文档；Makefile 原 verify-ui 已包含四组实际 Chromium 测试，无需改动。业务 Go、SQL/schema/API/DTO、CSP/CSRF/Origin、依赖与主分支保持不变。没有真实服务、凭据、部署、PR、agent 学习或自动启用。

## 产品与职责

工作台顺序：任务选择 → 批量编辑 → 全部视频/字幕预览 → 确认方案 → 原方案执行与对比记录。默认媒体库硬链接，下载原文件保留做种。确认仅批准不可变人工意图，不表示执行或入库完成。只有后端 complete 才显示完成；部分执行、等待全部 Emby 发现使用明确文字且无绿色完成。

管理服务静态路由只允许 app.js/style.css；保持入口兼容，在 app.js 内以独立职责拆分：BatchEditor 管字段、逐行编号、行序和排序确认；ReviewedRequestGate 管请求代数与完整原始 DOM 快照；BatchWorkbench 管预览、取消、确认、反馈；renderBatchPreview/renderComparison 管安全展示。复用 el/textContent、field/select、table/row、writeButton、api/session 与单写在途边界，未引入框架。文件路径对比组件复用于预览及人工记录。

手动填写、明确提取编号+偏移、连续编号均按完整 DTO。保留下载器初始顺序，不字典排序；连续模式显式移动、展示编号、勾选全部行序，再完整预览确认。行序及输入更改解除排序批准；静默赋值在请求返回和确认时核对原始 DOM 快照（包括逐行输入及非当前模式字段），防止复用旧预览。候选填表仅供参考。取消保留草稿；失败就地说明，可修正后重新预览。确认失败不丢草稿。

季集与路径、识别提示和逐文件执行事实分列。辅助文件可展开核对，不作为入库文件。human 意图不可变，实际结果独立显示，semantics_resolved=false 显示“季集未声明”。提供人工规则维护入口，本次确认不创建或启用规则。桌面定宽列，390px 下文件表转逐行卡片，键盘移动保持焦点，输入有标签，错误标记/定位，异步操作有 aria-busy 与反馈，状态不单靠颜色。

## 自审修复

- 草稿只从内存行数据构建请求，漏掉无事件逐行赋值：改为直接读取当前每行 input.value，与全字段原始快照核对。
- 旧排序勾选未绑定本次全表：保存排序签名，静默改首集/季号/行号/顺序即不能批准旧排序。
- 双击预览/确认：复用全局单在途；确认只提交成功预览保存的请求对象、plan_version/digest。
- 晚 success/error：请求代数、页面 generation、DOM 快照共同判归属；取消显式失效并中断可取消预览，确认发送后明确告知无法撤销。
- 记录曾把未声明语义显示 S0E0：严格检查 semantics_resolved；执行状态不写回 human 意图。
- 两组旧部分合集回归仍找“安全重试”：按冻结语义更新为“恢复原冻结方案”，保留实际 SQLite 重开、恢复硬链接和发现断言。

## 验证状态（待 CI 更新）

本机：官方 Go 1.26.9 linux/amd64 归档 SHA256 `42d158b4d8f7b61ac0a830567c940a86098fb7aac52e467a5ebec03ef5cc2f8d` 校验通过；node --check、四组 Python AST、git diff --check、五组 preflight unittest 通过。

本机 make verify 与 make verify-ui 均在 modernc.org/sqlite v1.50.1 下载返回 Forbidden 时停止；Playwright 官方 Chromium 下载返回 403 Domain forbidden。未更换依赖来源或绕过限制，未把 mock UI 当生产后端链。桌面/390px 截图仅由实际浏览器测试保存到 /tmp，不上传。

新增实际 API 测试扩展了原批量闭环：视频/字幕完整预览、辅助文件、预览无持久意图；实际目标冲突及确认失败恢复；输入、静默字段/逐行修改、reset、模式、排序、取消、导航的晚成功/错误；键盘排序及重新确认；双击单请求；部分文件故障与冻结方案恢复、部分 Emby 发现后不完成、全部视频发现后 complete；旧 opaque 记录季集未声明；人工规则下一步入口。Go 末尾仍核对源/目标同 inode、仅一份人工记录、仅一次下载。

CI 将在此分支运行既有 make verify 与四组 make verify-ui；最终链接、HEAD 与结果待完成后填入。当前尚无本轮完整浏览器通过结论。没有截图人工目检结论（本机 Chromium 不可用）。
