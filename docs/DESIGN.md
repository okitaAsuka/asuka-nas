# 首版设计 / 2026-10-12

## 范围与所有权

Linux NAS 上 Docker 单实例 Go 1.26.9 服务，SQLite 持久状态，无模型依赖。
下载由 qBittorrent 完成；下载和媒体目录必须同文件系统且同时挂入容器。
架构在部署前确认，不假定 Intel 即 amd64。首版只运行 fake HTTP 与临时文件测试。
asuka-agent 将来只通过 v1 API 查询、通知和建议；不得直接写数据库或执行文件操作。
旧 Gateway 方案中的默认 S01、直接 rename、NATS 都不沿用。

## 状态与重放

subscriptions 保存来源标识；resources 以订阅+RSS GUID 去重，同时以 torrent infohash 全局去重。
RSS 标题和 URL 都不进日志或事件。v1 仅接受带明确 BTIH 的 magnet，避免为了获取哈希访问未知 URL；蜜柑 torrent enclosure 解析后续实现。
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
字幕旁车仅接受同目录、同 stem 的 .srt/.ass/.ssa/.vtt（可带安全语言后缀），与视频共同冻结到方案；逐文件链接，部分成功时可恢复，不回滚/删源。
下载完成文件必须由 downloader 保持只读语义；指纹前后验证不提供对恶意并发写入内容的隔离。目录所有权须排除非受信任进程移动已打开父目录；不把本服务当多租户文件沙箱。

## 安全与运行

配置严格 JSON，URL/路径/凭据从部署配置读取；样例无凭据。错误用固定代码，日志只允许事件码/job ID。HTTP 客户端限制响应、超时、禁止重定向，避免认证头/秘密 URL 泄漏。
SQLite 本地磁盘 WAL、foreign_keys、busy_timeout；单连接串行事务。数据库目录权限 0700、文件 0600。配置读取不自动访问网络。
不造调度平台。首版库提供一次 Tick，CLI 只检查配置；常驻 poller、鉴权 API listener 和真实 NAS 验收后置。

## 验证门槛

make verify = 格式、go vet、race tests、build。fixture 覆盖重复 RSS/完成事件、SQLite reopen、硬链接后崩溃重放、重试、指纹变化、路径逃逸/冲突、字幕、0/7 天、独立停止、刷新与发现分离。GitHub Actions 与本地同入口；开发分支不合并。

## 上游依据与首版限制

qB adapter 使用 [官方 5.x API](https://github.com/qbittorrent/qBittorrent/wiki/WebUI-API-%28qBittorrent-5.0%29) 的 add/info/files/stop；4.x 兼容后置。add 显式 savepath，文件查询校验 save_path 与配置一致；qB 容器和本服务必须使用相同下载路径。Emby 按 [官方浏览 API](https://dev.emby.media/doc/restapi/Browsing-the-Library.html) 查询并精确核对 Path，本服务和 Emby 也须同路径挂载。实际版本兼容性与认证过期刷新尚未在真实服务验收。

只处理单视频 torrent 的最小闭环，多视频进入待确认且当前不支持整体批准，不能只导入其中一个后误标完成。字幕仅同 stem 的有限格式，扫描上限 4096 项。人工确认预先检查目标冲突，执行仍以独占 linkat 作最终仲裁。Linux 需要支持 openat2（5.6+）和 /proc/self/fd；不支持时 fail closed。

复核补充：源和逐层目标父目录 fsync 后才提交 imported；SQLite synchronous=FULL。哈希/链接响应 context 取消。批次先扫已知完成 job 的 stop deadline。时钟回拨不重算 completed_at/stop_at，不让 stopped 变回 seeding；进程离线和宿主错误时钟不承诺实时截止。详见 REVIEW.md。
