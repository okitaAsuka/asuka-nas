# 当前状态 / 2026-10-12

## 阶段交付

- main：`21c5bdc`，仅最小 README；未推送、未合并开发分支。
- 第一阶段：`945cfa4`，设计、API 契约、Go 1.26.9、配置、SQLite、测试和 GitHub Actions。
- 第二阶段：当前开发分支 `codex/nas-foundation-20261012`，fake HTTP/临时文件最小闭环。

已实现：magnet RSS 解析/去重、持久 job/CAS/事件、qB 5.x add/login/info/files/stop 边界、完成时间独立做种计时、显式 SxxExx 命名预览、人工方案摘要/版本/指纹确认、安全硬链接及同 stem 字幕、Emby 刷新与发现分离、幂等重放和故障恢复。CLI 只校验配置，不启动后台任务。

## 验证证据

真实 `go version go1.26.9 linux/amd64`（隔离缓存下载，未使用系统同名错误程序）。
`make verify` / `scripts/dev.sh`：gofmt、go vet、15 个测试的 race run、go build 全通过。
领域包覆盖率 80.2%；配置样例检查和 CGO_ENABLED=0 构建通过；git diff --check 通过。

覆盖：重复 RSS/资源 hash 去重、SQLite reopen/CAS/事件游标、硬链接成功但状态未提交的模拟崩溃、刷新重试、0/7 天计时、文件列表失败/下载路径不符/导入冲突均不阻塞到期停止、低置信度不猜季度、过期确认/指纹变化/目标冲突、字幕、路径穿越/符号链接/跨设备拒绝。测试仅本机 fake HTTP 与 t.TempDir，未连接实际 RSS/下载/媒体服务。
Dockerfile 是构建骨架；未构建/运行镜像，未部署 NAS。

## 远端阻塞

现有 `gh api user --jq .login` 返回 `okitaAsuka`。
目标查询返回 404/不可解析（现有认证无法证明私有仓库不存在或不可见）。
`gh repo create okitaAsuka/asuka-nas --private ...` 返回：
`GraphQL: Resource not accessible by integration (createRepository)`。
开发分支 `git push -u origin codex/nas-foundation-20261012` 返回 `Repository not found`。
`gh run list --repo okitaAsuka/asuka-nas --branch codex/nas-foundation-20261012` 返回 HTTP 404。
因此无远端提交或真实 CI 成功链接；工作流已落盘，但尚未执行。需要用户侧让现有连接具备建仓/访问权限，或创建该私有仓库并授权现有连接访问；本次不生成 token、不扩权。

## 未实现 / 后续

- 真实蜜柑 HTTP 轮询和 .torrent enclosure 下载/infohash 提取（目前仅 magnet fixture）。
- 常驻 poller、可用 HTTP API listener/鉴权、Discord/agent 通知（只定义契约）。
- 多视频 torrent 整体方案、复杂字幕和复杂命名、qB 4.x/认证续期、Emby 分页/实际版本兼容验收。
- 真实 NAS 架构/挂载、硬链接权限、Docker 构建、真实 CI。

参考仓库 `/workspace/asuka-agent` 保持 clean，未修改。预置历史不含 a1ff5c4，读取当前 ACTIVE_PLAN H1/H2 与归档设计。交付独立目录 `/workspace/asuka-nas`，另有包含 main 和开发分支的 Git bundle。

## 独立复核阶段

在 c1c54c8 基线上逐段审查，修复持久化目录链、取消、批次停止顺序、配置总长度和 downloader 参数边界。21 个顶层测试（含事务故障子测试）通过 race/vet/build；不以覆盖率认定生产安全。完整发现、测试名、文件树和真实适配/fake 边界见 REVIEW.md。远端按用户要求不再重试创建或扩权。
