# asuka-nas

Go 1.26.9 + SQLite 的独立 NAS 番剧入库服务。qBittorrent 下载、同文件系统硬链接、Emby 刷新与发现确认，正常流程不依赖模型。

已实现 RSS/.torrent HTTP 适配和常驻轮询，**只用本地 fixtures 验证，尚未进行真实服务或 NAS 部署验收**。无 HTTP 管理端口，无 Discord。

- [设计与运行边界](docs/DESIGN.md)
- [agent v1 契约（尚无 listener）](docs/API.md)
- [当前交付和限制](docs/STATUS.md)
- [复核材料与测试名](docs/REVIEW.md)

```sh
make verify
go run ./cmd/asuka-nas -config config.example.json
```

默认仅检查配置，不联网。只有显式 `-run` 才启动轮询；本轮未对真实服务运行此模式。
订阅配置写在 `subscriptions` 数组，每项包含 `id`、`url`，以及可选 `enclosure_origins`（完整 scheme://host[:port]）；示例为空数组。带 token 的 RSS URL 只放用户本地配置，不提交或写日志。
部署者已有凭据通过 `ASUKA_NAS_QB_USERNAME`、`ASUKA_NAS_QB_PASSWORD`、`ASUKA_NAS_EMBY_TOKEN` 提供；本项目不生成凭据。qB 5.x、下载器和本服务须相同下载挂载路径，Emby 和本服务须相同媒体挂载路径。

如 `go` 不是实际工具链，设置 `GO_BINARY=/absolute/path/to/go1.26.9/bin/go` 后运行 `scripts/dev.sh`。Dockerfile 的默认命令仍仅做配置检查；镜像尚未构建/部署验收。

安全单集 MVP：合集/未知季集/字幕映射不明整体待确认，不自动挑首个视频。qB 会话失效后仅对 401/403 有界重新登录（每次最多一次、60 秒冷却）；凭据从上述部署环境变量引用读取，不在客户端保存。完成/待确认任务和未到期订阅无持续写入或上游请求。
