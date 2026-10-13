# 已有库候选里程碑：父任务验收入口

基线 `b9b0ce407fd18daeea9f14b3e74303f5a796b199`，远端 `okitaAsuka/asuka-nas`，分支 `codex/nas-mapping-learning-20261013`。精确交付HEAD以本文件所在提交及最终交付回复为准。只有NAS项目被修改；未合并、建PR、部署、扫描真实NAS、接凭据或真实外发。

## 关键文件 / 接口

| 文件 | 本轮变化 |
| --- | --- |
| internal/nas/library_candidates.go | 明确相对系列目录的安全只读扫描、限额/取消、局部NFO/XML解析、证据冲突、确定性摘要 |
| internal/nas/management.go | 三个CSRF保护POST：library-candidates、candidate-rule-preview、candidate-rule；预览/保存重扫；不启用、不导入 |
| internal/nas/web/app.js | 待映射任务来源线索→候选→完整快照草稿→预览/保存/单独启用；取消与旧响应归属；固定阻塞恢复说明 |
| internal/nas/library_candidates_test.go | 一致/矛盾/未知/绝对配对；路径、XML、符号链接、大小/深度/数量、确定性、取消、50GiB稀疏视频仅stat；过期拒绝、规则恢复、硬链接/Emby/事件/冻结 |
| internal/nas/library_browser_test.go + tests/library_browser.py | 实际Chromium候选链与下一集自动；双击、目录变化拒绝、取消、忽略取消的旧响应/导航 |
| Makefile | verify-ui纳入新测试；既有产品闭环与预览归属继续运行 |
| docs/NAMING.md + LOCAL_MANAGEMENT.md + STATUS.md | 当前矩阵/操作说明；旧N1文档归档至archive/N1_NAMING.md |

POST `library-candidates`: `{ "directory": "相对系列目录" }`，返回 `directory/digest/candidates`；每候选返回 `season/episodes/display_name/series_id/sources/conflicts/unknown`。`sources`含局部相对路径、证据类型、可证明季集与可选总集配对。POST `candidate-rule-preview`: `directory/digest/rule/title/source`；POST `candidate-rule`: `directory/digest/rule`，只追加manual不可变草稿；enable继续原rule-state/epoch。领域/控制schema和parser版本未改。

## 定向自审

- 候选不能授权入库；既有完整清单、指纹、全目标预约、epoch/CAS与冻结门禁均未改。已有无计划needs_confirmation的epoch重检直接复用；已有未开始旧计划仍须显式replan。
- 所有打开相对可信配置根fd，拒绝symlink/逃逸/非普通文件；512项/3层子目录/NFO64KiB单份、1MiB总计/5秒、XML4096token/16层。限额失败无部分结果。NFO外链/提供商ID/剧集title不作为身份，无联网和模型调用。
- 只建议已观察季集；Season目录不证明集序。未知篇章不外推下一季；RSS别名必须本次填写，已有明确规则身份仅精确目录/名称唯一时复用。
- 预览/保存均重扫摘要；修正了atime读访问导致的伪过期，同时保留inode/ctime/mtime/size与NFO字节变化检查。取消/输入变化/导航独立代数+目录快照，即使传输忽略AbortSignal也不接受旧响应。
- 特殊篇/不支持清单/冻结冲突没有强制通过；详情禁用不能解除这些原因的重试并说明诊断路径。失败源保留，做种stop独立，未来规则不迁移已开始计划。
- 名称/原始RSS/NFO皆不可信，只以textContent呈现；原始NFO、源路径和原始错误不写事件/日志。局部路径仅进入本机管理DTO。
- 根目录仍要求可信所有权；context无法中断内核永久挂起I/O，摘要重扫也不是对恶意并发写入的原子快照。

## 验证与真实限制

本机真实Go1.26.9（官方发布SHA256校验）、Python3.12.14、系统Chromium。当前环境Go构建缓存置于/tmp；SQLite模块的正常官方代理下载返回Forbidden，未绕过网络限制，所以本机完整make入口暂阻塞。Playwright下载Domain forbidden；已找到系统Chromium，未改下载域或权限。

本机以同一生产扫描文件/安全路径与规范化helper组成隔离包，运行实际Go1.26.9 race：四组扫描测试及九项路径/限额子测试全部通过。该检查只证明扫描组件，不替代完整仓库/API/业务验收。JS语法、Python编译、gofmt与diff whitespace检查通过。

首次代码提交 `d748b98f7c82819a2060d2f78bf3993b872aaed5` 的 [CI run 38340033425](https://github.com/okitaAsuka/asuka-nas/actions/runs/38340033425)：make verify成功（含本轮API恢复/冻结测试），旧两组实际Chromium测试成功；新浏览器测试因测试选择了页面最后一个隐藏pre而非候选证据pre失败。已针对候选证据定位修正，不放宽业务断言。最终代码/CI状态在交付回复中绑定精确HEAD与链接。

真实NAS/真实RSS/qB/Emby/Discord、跨机事件连接、内核挂起I/O、实际断电和真实权限/挂载仍未验。CI及fixture只接临时目录和fake回环HTTP。此里程碑结束后不自动开展后续阶段。
