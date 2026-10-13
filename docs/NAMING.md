# 当前命名规则与已有库候选

命名始终使用确定性本地规则，不调用 TVDB/TMDB、联网元数据或 LLM。当前能力如下，早期阶段说明存于 [archive/N1_NAMING.md](archive/N1_NAMING.md)。

| 能力 | 当前状态与边界 |
| --- | --- |
| 人工版本规则 | CLI 和回环中文 UI/API；不可变 revision，保存草稿与显式 enable 分开，epoch 防止旧操作 |
| 相对集数 / SxxExx / 有界绝对编号 | 已实现；全来源季集/目标必须一致，绝对编号仅在明确有限映射内执行 |
| 合集 | 明确有界规则下逐文件冻结、硬链接、恢复、逐视频 Emby 确认，见 [COLLECTIONS.md](COLLECTIONS.md) |
| 已有媒体文件名 / NFO | 用户明确选择相对系列目录后，只读提取局部候选；预览、保存均重新校验目录摘要 |
| 篇章 → 季号 / RSS 别名 | 现有明确规则或此次用户选择；已有 Season 01/02 不意味着新篇章 Season 03 |
| NAS 事件 / agent 固定通知 | NAS 默认关闭的认证只读事件服务、agent 配套发送均已实现；未真实连接或外发 |
| 模型学习 / 历史移动 | 未实现；本轮保存可复用人工规则，不宣称模型学习，不迁移已开始计划 |

## 显式规则

以下季号仅为虚构 fixture：

```json
{
  "id": "fiction-arc",
  "revision": 1,
  "series_id": "fiction-series",
  "provider": "manual",
  "order": "my-confirmed-order",
  "subscription": "fixture",
  "aliases": ["虚构作品 篇章", "Fiction Arc"],
  "season": 9,
  "directory": "用户选择目录",
  "display_name": "虚构作品"
}
```

series_id 是稳定本地 ID，不冒充在线提供商 ID；同 ID 的 order 不可改变。目标模板固定为 `directory/Season NN/display_name - SnnEnn.ext`。`episodes` 与 `absolute` 的明确有限范围见 COLLECTIONS.md；候选观测集数不自动成为未来范围。目录、名称、编号仍受既有校验约束。

规则匹配 subscription、可选 group 与分段起始完整 alias，无隐式优先级。原文保留，identity_normalization_version=1 使用一致的 NFKC、HTML entity、空白和结构布局比较；旧版本0使用只读比较视图，未知规范化版本保守阻塞。拒绝控制/双向字符、特殊篇、小数、未支持编号；parser v4。年份和分辨率不当作集数。

RSS 原始标题（XML 解码后最多8192字节）和来源关系保存在受保护 SQLite。同 GUID 的标题变化追加证据，空标题也不绕过跨订阅冲突。所有来源必须决定同一 series/order/season/episode/target；已知停用规则、scope 模糊、来源无法映射和矛盾都阻塞，不退回猜测。无规则命中时仅保留严格 SxxExx 通路并核对 RSS 显式证据。

停止 daemon 后，CLI 与服务使用同一实例锁：

```sh
asuka-nas rules add -config config.json -file rule.json
asuka-nas rules preview -config config.json -id fiction-arc -revision 1 \
  -title '[测试组] 虚构作品 篇章 [02]' -source '虚构作品 [02].mkv'
asuka-nas rules enable -config config.json -id fiction-arc -revision 1
asuka-nas rules disable -config config.json -id fiction-arc -revision 1
asuka-nas rules replan -config config.json -job OPAQUE_JOB_ID
```

## 已有库候选

从任务的“补充可复用映射规则”进入，选择允许的来源线索，或直接进入规则页；输入配置媒体根内的相对系列目录后点击“读取目录候选”。不接受媒体根本身，不自动后台全库扫描。

扫描使用 openat2 固定根 fd，拒绝 symlink/逃逸/非普通文件。最多512目录项、3层子目录；NFO每份64KiB、总计1MiB，XML最多4096 token/16层，用户态超时5秒，可取消。不读取视频内容、不请求 NFO 外链、不接受 DTD/实体声明/其它处理指令。内核永久挂起的文件系统 I/O 无法由 context 强制打断。

仅完整明确的 `名称 - SxxExx.mkv/mp4` 文件名和 `tvshow.title`、`episodedetails.showtitle/season/episode/absolute_number` 作为局部证据。NFO在线提供商ID、URL和剧集标题不作为系列身份。文件名/NFO同 stem 的季集冲突、名称冲突、同季绝对与相对编号冲突会列出；无证据显示未知，不以“第四季”目录或搜索相似度决定集序。

每季候选展示观察到的集号集合及逐项来源；总集数只展示已证明的逐集配对，不自动填入或外推绝对范围。新的本地系列 ID 建议按明确目录摘要生成，用户须核对；同目录/同名称的当前明确规则身份唯一时可以复用其 series_id/order。规则列表最多500版本，未出现在该窗口的身份不推断。RSS订阅/完整别名仍需本次选择/填写。

选择候选只填草稿。存在冲突时不填季号，用户解决后手动明确；季号是已有观测，并不证明新篇章属于同季。完整规则+revision+示例+候选摘要绑定预览，输入/change/reset/载入/导航使旧响应失效；保存再次核对快照。预览和保存均重新扫描相同目录，inode/ctime/mtime/size/目录项与 NFO内容变化返回 candidate_stale/library_changed；访问时间不影响摘要。无后台候选缓存。最终仍只保存 manual 草稿，单独 enable 后进入既有执行路径。

候选读取/校验没有入库、修改历史、规则启用或 Emby副作用。目录摘要不是文件导入批准，也不提供对恶意并发写入的隔离；执行仍用原指纹、目标预约、独占硬链接和冻结门禁。原始 NFO不进日志/事件，局部路径仅在本机管理页展示。

## 阻塞与恢复

无计划的 needs_confirmation 任务在规则 epoch/来源变化后自动重检，适用明确规则后安全执行；下载仍按 infohash去重。未开始计划因规则变化失效时显示 stale_plan，需显式“重新规划”；安全重试仅解除退避。完整合集仍检查有界范围、所有来源、完整清单与目标预约。

计划保存规则引用、epoch、来源版本、决定、路径、指纹及历史。开始前再次核对并持久化 import_started，之后冻结；未来规则不得改已开始目标。链接后崩溃按同 inode恢复，失败不删源、不覆盖目标。晚到来源矛盾/legacy证据不明、特殊篇和不支持清单没有强制通过或历史迁移工具；详情页固定说明，重复重试不能解决。做种到期只 stop，独立于映射/通知。


## 单任务人工批量边界

除了既有可复用人工版本规则，现有后端支持仅本次完整下载任务的人工批量方案；用户不需要当场手写推广规则。明确季号/目录/显示名、逐行集号、明确编号的有界offset或显式确认的连续行序，仍经过全包/字幕/安全路径/完整指纹/预约/冻结门禁。确认生成不可变对比记录供本机查看；后续agent归纳候选未实现，未来必须人工终审后才可启用。契约与状态以BATCH_AGENT_CONTRACT.md及STATUS.md为准，前端草稿正交独立owner集成。
