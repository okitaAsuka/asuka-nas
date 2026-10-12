# 下载、映射与确定性通知

NAS执行无需模型或gateway在线；通知只是输出，不提供Discord命令入口。两服务可以同机运行，也可由部署者自行建立保持NAS回环边界的可信隧道；本轮没有建立隧道或修改监听。

在NAS已有部署JSON增加（样例默认false，部署者明确开启）：

```json
"events": {
  "enabled": true,
  "listen": "127.0.0.1:8788",
  "token_env": "ASUKA_NAS_EVENTS_TOKEN"
}
```

`ASUKA_NAS_EVENTS_TOKEN`引用部署者已有秘密（32–4096字节）。本轮没有创建、配置或连接持续凭据。显式`asuka-nas -config config.json -serve`启动daemon、本机管理页及事件服务；`-run`也可启动事件服务，CLI默认配置校验不监听。

在gateway已有配置增加：

```yaml
nas_notifications:
  enabled: true
  source_id: nas-local
  nas_url: http://127.0.0.1:8788
  nas_token_env: ASUKA_NAS_EVENTS_TOKEN
  discord_url: https://discord.com/api/v10
  discord_token_env: MAIN_AGENT_DISCORD_TOKEN
  channel_id: "123456789012345678"
  agent_id: main
  store_path: ./.nas-notification-store/notifications.sqlite3
  poll_seconds: 5
  progress_seconds: 30
  timeout_seconds: 5
```

频道ID为占位，必须由部署者选定；agent_id必须是已启用归属。标准gateway命令启动消费者；不会创建Runtime推理任务。NAS目标仅允许回环IP；Discord只允许官方HTTPS API或回环fake API。HTTP不跟随重定向。更改来源、API、频道或agent归属需新队列路径；禁止沿用错误cursor。

本机映射：管理页“命名规则”填写manual series_id/order、订阅/组scope、别名、人工季号与目录，预览后保存并启用。待映射任务在详情重新规划，再执行；下一集沿用规则。东京复仇者三天战争篇[02]无季号时仍阻塞，测试人员明确选择合成Season09；Re:Zero第四季01–11/总67–77使用明确Season04、bounded67→1..77→11，绝不联网推断现实季度。合集保持全包校验和逐文件冻结/恢复。HTTP refresh成功只代表请求接受，全部视频独立发现才通知“可播放”。

## 持久化与失败操作

- gateway拥有独立SQLite，单连接、WAL/FULL；owner SQLite连接长期持有BEGIN EXCLUSIVE，进程退出/崩溃自动释放，不用易残留pid文件。inbox/outbox/cursor同一事务；读取失败和落盘失败不推进cursor。
- 普通resource_seen/download_submitted延迟30秒；同job尚未尝试的普通进度合并。完成、入库、可播放、人工阻塞、固定故障、做种停止各自保留。关键状态优先发送，失败行退避时其它就绪行可继续。已尝试内容/nonce不会被合并改写。
- 出站at-least-once。使用稳定24字符nonce和enforce_nonce=true；官方仅对过去几分钟相同author/nonce去重。发送成功后本地提交丢失、超出窗口或人工重试仍可能重复，不能承诺exactly-once。
- 网络/超时/5xx指数退避（2秒起，有界≤1小时），每行最多12次尝试后dead保留。重启沿用预约、attempt和cursor。429使用Retry-After/JSON retry_after较长者，并暂停该固定发送目标的所有请求至到期；非法/异常巨大值暂停外发等待人工处理。
- 400等永久请求错误dead隔离，不阻塞后续行。401/403/404同时暂停整个固定目标，避免反复无权发送；NAS401/403独立暂停读取，已收到通知仍可发送。
- 可识别id的未知schema/kind、非法安全字段是quarantined；不存原payload，后续事件继续。没有可信id/坏页面cursor则不能安全跳过，固定nas_page_invalid审计+有界退避，需修复NAS来源协议。
- 停gateway后运行`asuka-agent --config <配置目录> gateway nas-notifications status`看cursor/pending/dead/quarantined/暂停计数；修好配置/权限后`... resume`重置dead和暂停再启动。隔离事件仍保留，协议升级前由开发者按可信NAS事件重放处理，不把坏payload发出。无远程恢复命令。
- 安全状态投影到gateway现有/events和Operator“事件记录”（source=nas_notifications，event_type=nas_notification_state）。该投影沿用既有best-effort日志；独立inbox/outbox才是恢复事实源。NAS管理health不探测gateway/Discord连接，不作为本通知状态依据。持久库失败会停止消费者并记录固定错误，不影响NAS下载、入库与做种。

通知仅公开校验后的任务标识、固定中文阶段/故障说明和本机操作指引；展示名称不启用。RSS URL、magnet、tracker、源绝对路径、标题、凭据与原始远程错误不进入消息/队列/日志。Discord allowed_mentions.parse=[]且replied_user=false。NAS本机管理页仍可显示授权本机用户需要的标题/相对计划。

## fake跨仓验收

恢复两个bundle到相邻独立仓库后，用真实Go1.26.9：

```bash
GOMODCACHE=<可写模块缓存> GOCACHE=<可写构建缓存> \
python3 asuka-agent/scripts/nas-notifications-smoke.py \
  --nas-repo ./asuka-nas --go /absolute/go1.26.9/bin/go
```

只启动NAS测试fixture、真实认证事件handler、真实持久gateway Worker和本地fake Discord。覆盖重复下载、需映射→管理API预览/保存/启用/重新规划→硬链接→Emby发现→通知、通知503与重启持久重试、mention/隐私边界。合集两种编号及单个视频未发现见NAS定向/完整测试；429/授权/毒事件/ACK落盘失败见gateway队列测试。没有真实Discord发送、NAS访问或部署。

上游：[Discord Create Message](https://docs.discord.com/developers/resources/message#create-message)、[Discord Rate Limits](https://docs.discord.com/developers/topics/rate-limits)。真实qB/Emby/NAS/Discord版本、Bot权限、隧道及跨机部署仍未验。
