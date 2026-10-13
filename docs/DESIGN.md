# 首版设计 / 2026-10-12

当前架构：NAS独占下载/文件/规则事实；本机管理UI/API已实现。默认关闭事件服务提供固定通知；默认关闭learning/naming服务允许agent有界候选，必须人工终审。普通通知、下载与做种不调用模型。当前边界见STATUS.md、NAMING_ASSISTANCE_HANDOFF.md、NOTIFICATIONS.md。

以下保留阶段演进记录；早期“仅单集/无HTTP/UI/Discord”等限制由末尾N3/N4、本机管理及本轮实现覆盖。

## 范围与所有权

Linux NAS 上 Docker 单实例 Go 1.26.9 服务，SQLite 持久状态，无模型依赖。
下载由 qBittorrent 完成；下载和媒体目录必须同文件系统且同时挂入容器。
架构在部署前确认，不假定 Intel 即 amd64。首版只运行 fake HTTP 与临时文件测试。
asuka-agent 将来只通过 v1 API 查询、通知和建议；不得直接写数据库或执行文件操作。
旧 Gateway 方案中的默认 S01、直接 rename、NATS 都不沿用。

## 状态与重放

subscriptions 保存来源标识；resources 以订阅+RSS GUID 去重，同时以 torrent infohash 全局去重。
RSS 标题和 URL 都不进日志或事件。支持明确 BTIH magnet 与 HTTP .torrent enclosure。enclosure 仅允许订阅同源或配置 allowlist，拒绝重定向；元数据有界读取，按原始 info 字节计算 SHA1，不重新编码。
jobs 独立保存 download/import/refresh/discovery/seed 状态。download completion 来自 qB 的 completion_on，首次持久化后不后移。
0–7 天配置，按完成时间算 stop_at；0 立即停止。只调用 stop，不调用 delete，计时不依赖命名或导入成功。
状态更新和 events 追加在同一 SQLite 事务。事件单调序号、版本、job ID、类型，不保存标题/URL/原始错误。消费者持久 cursor，以 at-least-once 消费和 ID 去重。重放不重新执行命令。
外部效果无分布式事务：先持久化意图，重试可幂等操作（以 hash 添加 torrent、stop、硬链接、Emby refresh）。
允许 refresh 重复请求，绝不将 HTTP 2xx 当成媒体已发现；只有独立查询发现才标记 confirmed。

## 文件与确认

只接受下载根下的相对文件。Linux openat2 RESOLVE_BENEATH|RESOLVE_NO_SYMLINKS 打开源和父目录；linkat 从已打开源 fd 硬链接，目标父目录 fd 固定。拒绝绝对路径、..、符号链接、非普通文件。目标创建使用独占硬链接，不覆盖。
不支持跨设备复制，EXDEV 转为 cross_device；源保持做种。已存在同 inode 的目标视为崩溃后的成功重放，其他目标为 conflict。
预览记录 SHA256、size、mtime、device/inode 与 plan version；执行重新校验，过期方案必须重新预览。
只匹配明确 SxxExx 的视频名，未知季度直接 needs_confirmation；用户可显式给出 season/episode，不能让模型自行批准。
运行流程仅从 qB 完整文件清单关联字幕，旁车仅接受同目录、同 stem 的 .srt/.ass/.ssa/.vtt（可带安全语言后缀），与视频共同冻结到方案；逐文件链接，部分成功时可恢复，不回滚/删源。
下载完成文件必须由 downloader 保持只读语义；指纹前后验证不提供对恶意并发写入内容的隔离。目录所有权须排除非受信任进程移动已打开父目录；不把本服务当多租户文件沙箱。

## 安全与运行

配置严格 JSON，URL/路径/凭据从部署配置读取；样例无凭据。错误用固定代码，日志只允许事件码/job ID。HTTP 客户端限制响应、超时、禁止重定向，避免认证头/秘密 URL 泄漏。
SQLite 本地磁盘 WAL、foreign_keys、busy_timeout；单连接串行事务。数据库目录权限 0700、文件 0600。配置读取不自动访问网络。
不造调度平台。CLI 默认只检查配置；显式 -run 启动 RSS 和 job 两条循环，无 HTTP listener。订阅预约/失败退避、job 尝试预约/失败退避都持久化；做种截止检查绕过导入退避。鉴权管理 API 和真实 NAS 验收仍后置。

## 验证门槛

make verify = 格式、go vet、race tests、build。fixture 覆盖重复 RSS/完成事件、SQLite reopen、硬链接后崩溃重放、重试、指纹变化、路径逃逸/冲突、字幕、0/7 天、独立停止、刷新与发现分离。GitHub Actions 与本地同入口；开发分支不合并。

## 上游依据与首版限制

qB adapter 使用 [官方 5.x API](https://github.com/qbittorrent/qBittorrent/wiki/WebUI-API-%28qBittorrent-5.0%29) 的 add/info/files/stop；4.x 兼容后置。add 显式 savepath，文件查询校验 save_path 与配置一致；qB 容器和本服务必须使用相同下载路径。Emby 按 [官方浏览 API](https://dev.emby.media/doc/restapi/Browsing-the-Library.html) 查询并精确核对 Path，本服务和 Emby 也须同路径挂载。实际版本兼容性尚未在真实服务验收；qB 使用部署者环境变量引用按需登录，401/403 最多重新登录一次并重放一次原请求，60 秒认证冷却；其它失败由持久 job 退避处理。

只处理单视频 torrent 的最小闭环，多视频进入待确认且当前不支持整体批准，不能只导入其中一个后误标完成。字幕仅同 stem 的有限格式，扫描上限 4096 项。人工确认预先检查目标冲突，执行仍以独占 linkat 作最终仲裁。Linux 需要支持 openat2（5.6+）和 /proc/self/fd；不支持时 fail closed。

复核补充：源和逐层目标父目录 fsync 后才提交 imported；SQLite synchronous=FULL。哈希/链接响应 context 取消。批次先扫已知完成 job 的 stop deadline。时钟回拨不重算 completed_at/stop_at，不让 stopped 变回 seeding；进程离线和宿主错误时钟不承诺实时截止。详见 REVIEW.md。


## 来源与 daemon 决策

- `.torrent` 解析依据 [BEP3](https://www.bittorrent.org/beps/bep_0003.html)，保留 info 原始字节。拒绝非规范 bencode、超过 32 层/50,000 节点、重复 key、路径穿越/符号链接元数据、v2、webseed；最多 4096 个文件，核对分片数量。上传已验证 metainfo 字节，不把秘密 URL 交给 qB。
- SQLite schema v2 在事务内为 v1 增加 torrent_payloads/source_state/source_events，已有 job/events 不改。未知更高版本拒绝打开。新连接从 DSN 恢复 foreign_keys、busy_timeout、synchronous。torrent blob 可含 tracker URL，因此数据库/备份也视作私密资料。
- 默认 feed 间隔 600 秒（配置 60–86400），job tick 15 秒（1–300），HTTP 超时 10 秒（1–60），来源请求最小间隔 250ms（100–60000）。RSS 默认 2MiB、torrent 8MiB，最大都 8MiB；最多 200 个 feed 项（1–1000）。最多 100 个订阅。
- 发请求前预约下一次尝试。来源失败指数退避到 max(轮询间隔,1小时)；job 失败退避最多 1 小时。取消保留已预约时间，重启遵守它。时钟回拨不提前轮询或重算做种 deadline；时钟跳变/进程离线不承诺真实时间精度。
- RSS 与 job 各一条串行循环，慢来源 HTTP 不占用 job 循环。一个 job 的大文件哈希仍可能延长下一批轮询；已知 deadline 在每批最前处理，不提供硬实时保证。哈希支持 context 取消，但不可能中断内核永久挂起的磁盘 I/O。
- flock 防止两个 daemon 执行相同数据库的外部副作用。SIGINT/SIGTERM 取消在途 HTTP 和用户态哈希，等待循环退出后释放 fd/SQLite/锁。外部效果不回滚，依靠可重试操作恢复。
- 管理 HTTP、Discord、模型调用均未启用；人工 Propose/Confirm 目前为内部库 API，尚无操作端 UI/CLI。


MVP 收尾：合集识别覆盖常见视频扩展名，但只自动入库唯一 mkv/mp4。整个清单必须通过路径/文件类型/字幕映射检查，否则持久 block_reason 并整体待确认；Propose 同样执行此门禁。共享目录其它任务的旁车不被吸收。qB save_path 严格等于本地 downloads，不猜容器路径映射；同 inode 根拒绝。未到期来源走只读快速路径，空闲 job 无写事务或上游请求。`needs_confirmation` 的 block_reason 与短期 ErrorCode 分离，停止做种故障不抹去阻塞原因。


## N3/N4 当前边界（覆盖早期仅单集描述）

schema v4 引入全目标预约与逐文件持久执行，支持人工明确有界映射的完整合集；状态及迁移审查见 COLLECTIONS.md。命名优先人工版本规则，保留严格 SxxExx fallback，但已知规则的模糊 scope/别名、停用和证据矛盾不能 fallback。低置信度继续待确认。每个视频分别确认 Emby Path，全部确认才产生最终 discovered；做种仍独立。没有通用工作流框架或模型依赖。


## 当前通知边界

专用回环端口只支持GET /v1/events，部署env引用的Bearer不复用管理nonce。NAS与gateway不共享SQLite。gateway单消费者持有独占owner锁，inbox/outbox/cursor同事务；外部ACK之后才能标sent。故障不改变NAS导入/做种。详细失败与隐私契约见NOTIFICATIONS.md。


## 映射学习后端检查点 / 2026-10-13

NAS仍是唯一任务/文件/命名事实源；独立默认关闭学习回环服务只导出成功人工对比的最小投影并接受pending候选。agent复用既有provider且无工具访问。批准复用manual规则的事务helper，原子验证候选CAS、当前依据、epoch和有界试算，再创建并启用规则；服务Bearer没有批准接口。原对比和样本/候选正文不可变，规则更新不改已冻结文件。完整DTO见RULE_PROPOSALS_FRONTEND_HANDOFF.md；前端已整合，实际fake/浏览器/CI验证与真实环境未验边界见STATUS.md。
