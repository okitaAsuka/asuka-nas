# N1 人工命名规则

命名不依赖在线模型或元数据。RSS 原始标题（XML 解码后的字符串，最多 8192 字节）和订阅/GUID 关系保存在受保护 SQLite，事件/日志不携带标题或 URL。同 GUID 的标题变化追加证据，不能抹掉旧矛盾；重复不写新事件。空标题也保留来源，不能绕过跨订阅冲突检查。

规则 JSON 样例（**虚构 fixture，季号 9 不是任何现实作品的季号**）：

```json
{
  "id": "fiction-arc",
  "revision": 1,
  "series_id": "fiction-series",
  "provider": "manual",
  "order": "my-confirmed-order",
  "subscription": "fixture",
  "group": "测试组",
  "aliases": ["虚构作品 篇章", "Fiction Arc"],
  "season": 9,
  "directory": "用户选择目录",
  "display_name": "虚构作品"
}
```

series_id 是稳定本地 ID，不冒充 TMDB/TVDB；同 ID 的 order 不可改变。order 是人工明确命名的集序，N1 的 `[NN]` 直接对应此序下的第 NN 集；绝对集数执行偏移尚未支持。directory 是媒体根下的相对目录，最终模板固定为 `directory/Season NN/display_name - SnnEnn.ext`。规则不是任意路径模板，文件层继续拒绝穿越、符号链接、跨设备和冲突。

先停止 daemon，使用同一配置和实例锁执行：

```sh
asuka-nas rules add -config config.json -file rule.json
asuka-nas rules preview -config config.json -id fiction-arc -revision 1 \
  -title '[测试组] 虚构作品 篇章 [02]' -source '虚构作品 [02].mkv'
asuka-nas rules enable -config config.json -id fiction-arc -revision 1
asuka-nas rules disable -config config.json -id fiction-arc -revision 1
asuka-nas rules replan -config config.json -job OPAQUE_JOB_ID
asuka-nas rules parse -title '[测试组][虚构作品第四季 / Fiction 4th - 01~11][总第67~77]'
```

add 只存不可变草稿。修改要使用新 revision；enable 明确激活该版本。preview 是所选草稿的纯命名计算，不验证磁盘文件或授权执行。parse 不打开数据库、不访问网络。不要把秘密写进 title 或命令行；真正 RSS URL 只放受保护配置。

规则只匹配 subscription、可选 group 和分段起始完整 alias；NFKC、HTML entity 和空白规范化用于比较，拒绝控制/双向格式字符。支持 `[02]`、` - 02`、SxxExx；年份、分辨率不作集数，RSS 与视频的季集证据矛盾会阻塞。没有相应证据不能猜季号。东京复仇者三天战争篇示例只能得到相对第 02 集，季号未知。

所有来源必须得到同一 series/order/season/episode/target；不同规则冲突、某来源无法映射、命中停用规则均阻塞，不退回严格文件名。无规则命中时保留原严格 SxxExx 通路，但仍检查标题里的显式季集矛盾。多个规则即使更具体也没有隐式优先权，矛盾必须人工处理。

计划保存规则引用、epoch、来源版本、决定、路径和指纹，完整版本存于 plan_history。任一规则启停保守地使所有未开始计划失效，即使变更的是另一条规则；需要显式 replan。无计划的命名待确认 job 在规则/来源变化后自动重新检查。执行前事务再次检查 epoch 与 job CAS；先校验指纹，再持久化 import_started，之后冻结目标。硬链接成功、状态提交前崩溃仍按原 inode 恢复。规则更新不改动已导入文件；晚到来源矛盾发 late_mapping_conflict 并暂停该 job 后续媒体操作，不移动已有文件，做种停止仍独立运行。N1 尚无冻结冲突解除/历史迁移工具。

合集依旧整体阻塞。解析预览可显示显式第 4 季、相对 1–11、总第 67–77；MapAbsolute 仅是显式范围的纯函数，越界报错，不接执行器。没有逐文件合集计划、自动绝对集映射、元数据联网、LLM、Discord 或 HTTP 管理接口。
