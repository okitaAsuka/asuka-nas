# asuka-nas

独立 NAS 番剧入库服务：Go 1.26.9 + SQLite，qBittorrent 下载、同文件系统硬链接、Emby 刷新和发现确认，正常流程不依赖模型。

当前为 **fake 集成验证阶段**，没有生产 daemon 或 Discord。不要部署到真实 NAS。

- [设计](docs/DESIGN.md)
- [agent v1 契约](docs/API.md)
- [进度与限制](docs/STATUS.md)

```sh
make verify
go run ./cmd/asuka-nas -config config.example.json
```

配置检查不连接任何服务。部署凭据将由用户配置，不在仓库内生成。
