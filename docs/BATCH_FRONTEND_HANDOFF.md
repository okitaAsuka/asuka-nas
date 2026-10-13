# 批量整理前端交接（2026-10-13，草稿候选）

基线 `547d151b25c559841bcb68d743d8608bf26a6bbd`；实现工作树 `/workspace/asuka-nas`；远端分支 `codex/nas-batch-organizer-20261013`。精确SHA由本次交接回复给出。收到父任务前端拆分要求后，后端owner停止所有前端与浏览器文件写入；已有草稿保留供独立前端owner重构。

## 写入所有权

已有前端修改：`internal/nas/web/app.js`，新增批量表与记录面板；未动 `index.html/style.css`。已有浏览器候选：`tests/batch_browser.py`、`internal/nas/batch_browser_test.go`；尚未实际运行，其断言/交互覆盖作为起点，不代表验收成功。
共享入口 `Makefile` 已将 `TestBatchOrganizerBrowserLoop` 加入 `verify-ui`。从交接提交起后端owner不再编辑它、web文件、浏览器测试；前端owner建议只写 `internal/nas/web/*`、`tests/batch_browser.py`、`internal/nas/batch_browser_test.go`，如需共享入口改动由父任务协调。后端owner继续 `batch_organizer.go/comparison_records.go/store.go/management.go` 与非浏览器Go测试和契约文档。

不引入远端写API、模型、凭据、自动启用。既有管理API仍是loopback单用户、Origin/Host/CSRF/固定错误码门；不能把目录候选或任何置信度当执行授权。

## API / DTO（已编码，等待CI验证）

所有POST使用已有 `/api/session` nonce：`Content-Type: application/json`、`Origin`、`X-CSRF-Nonce`；响应失败409 `{"error":"固定码"}`。

`POST /api/batch-preview`：

```json
{"id":"opaque-job-id","version":7,"organization":{"directory":"Example","display_name":"Example","season":4,"mode":"manual","offset":0,"start":0,"order_confirmed":false,"rows":[{"source":"Example [67].mkv","episode":1},{"source":"Example [68].mp4","episode":2}]}}
```

成功返回 `{job_version,plan,digest}`；`plan.version = job.version + 1`，`plan.files[]`为全部视频/关联字幕`{source,target,fingerprint}`，`plan.manifest[]`为完整下载器清单（包括不入库辅助文件）`{source,fingerprint}`；`plan.batch`为上述人工意图。预览不写媒体、不持久计划/预约/样本。

`POST /api/batch-confirm`：同一请求，另加`plan_version`与`digest`。服务端重新构建、核对job版本及完整指纹摘要，然后一个事务保存已批准计划、全部预约/执行记录、不可变人工对比记录及固定事件。相同已批准plan/digest/organization的重放成功，不新造记录；变更或过期拒绝。返回`{"ok":true}`；客户端刷新任务。批准≠已执行≠全部Emby发现。

编号方式：

- `manual`：逐行填明确集号；`offset/start=0`、`order_confirmed=false`。
- `extracted`：每行集号必须等于源文件明确相对或总编号加`offset`，offset范围-998..998；双编号未选对应契约时拒绝；`start=0/order_confirmed=false`。
- `sequential`：用户调整行序后，必须先展示全部连续编号并明确确认排序；每行episode严格为`start + 行索引`；`offset=0/order_confirmed=true`。服务器不排序或按字典序分配集数。

季号1..99，集号1..999；目录安全相对路径，显示名单段且规范化、最多200字节，目录最多512字节。完整任务至多512文件/128视频。支持mkv/mp4；字幕仅同目录同stem的srt/ass/ssa/vtt，最多两个安全语言后缀。不支持特殊篇、分数集、多集单文件、混季、遗漏/重复视频或集号、不明确字幕关联。辅助文件仅绑定指纹，不入库。

已有`GET /api/jobs/{id}`新增`batch_hints[]`：`{source,season,relative,absolute,problem?}`，是解析提示而非执行批准；`job.sources`仍是完整清单。详情字段`files[]`已有 `{Index,State,Video,Discovered}`（保持现有大小写）；`ready`只在领域imported+discovery confirmed且无阻塞时为true。已开始计划禁止新预览，使用既有`job-action/retry`恢复原冻结方案；旧`replan`不能迁移已开始计划。

`GET /api/comparisons?id={jobID}&after={planVersion}`：`{items,next}`，每页最多20份；next为下一页游标或0。items为：

```json
{"intent":{"sample_id":"sha256","schema_version":1,"origin":"human","job_id":"...","job_version":7,"context_version":1,"plan_version":8,"digest":"...","parser_version":4,"created_at":1790000000,"organization":{},"manifest":[],"evidence":[{"subscription":"fixture","title_hash":"..."}],"changes":[{"source":"原名","target":"目标相对名","season":4,"episode":1,"before_season":0,"before_episode":67,"changed_fields":["target_relative_name","season","episode"]}]},"files":[{"Index":0,"State":"pending","Video":true,"Discovered":false}],"status":"confirmed_intent","error_code":""}
```

`intent`不可变；files/status是原plan的实时持久事实，不写回intent。状态：`confirmed_intent`、`partial`、`awaiting_discovery`、`complete`、`superseded`。只有全部linked、全部video discovered并最终领域事务已提交且无阻塞，才complete。最多10000份确认记录；不静默删除，达到上限拒绝新的确认。

重要错误：`stale_version/stale_plan/plan_frozen/invalid_job_state/invalid_batch/order_confirmation_required/collection_incomplete/invalid_file_manifest/duplicate_episode/episode_out_of_bounds/season_conflict/episode_unresolved/collection_preview_only/unsupported_episode_kind/unsupported_video/unsupported_file_set/subtitle_mapping_ambiguous/target_conflict/target_reserved/comparison_limit`。其它文件安全错误继续现有门禁。

## 前端待完成与验收

现有草稿使用生产vanilla JS DOM helpers，尚未做设计质量或完整交互验收；请按用户审美/复用/可维护性要求重构，不必保留草稿结构。应复用现有安全textContent渲染与写按钮/会话边界，优先拆出清晰职责的批量编辑、预览、排序确认、记录组件。重点检查输入/程序静默赋值/reset/取消/导航对晚success/error的归属；双击仅一次写请求；排序变更需重新确认；整表含字幕；错误修正恢复；完成前及部分执行不得显示成功。记录查看不能把human意图和实际结果混为一谈。

本机Go1.26.9官方SHA256核验成功，但SQLite/libc正常下载Forbidden，未绕过；make verify在依赖加载阶段停止，尚无Go业务或Chromium通过证据。JS语法/Python编译/五组Python预检已通过。后端将继续自审并用分支CI查业务；最终完整CI/UI验收需父任务集成前端候选后重新运行。
