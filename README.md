# asuka-nas

Go 1.26.9 + SQLite 的独立 NAS 番剧入库服务：RSS/.torrent → qBittorrent 下载 → 明确命名 → 同文件系统硬链接 → Emby 刷新及逐视频发现。保留下载源文件做种，到期仅停止、不删除。

中文本机管理页支持订阅、人工命名规则、完整任务批量整理、不可变对比记录，以及 agent 命名建议与规则候选的人工审核。普通下载、人工整理和做种不依赖模型。命名建议先预览完整文件表，再由人批准；拒绝、过期或模型离线时仍可直接人工 batch 整理，辅助来源永久保留。可显式复用已确认事实为规则草稿，核对别名和有限范围后独立预览、保存与启用。

专用事件通知与 learning/naming 服务已实现，默认关闭，仅回环监听。`learning.automatic_naming` 默认 false；显式开启后只对支持的待确认任务入队，agent 的默认关闭 `nas_naming` 仍需独立配置及有界持久预算。空必需目录/显示名由agent确定性列缺资料，不调用模型或消费预算；人补齐再明确请求。没有TVDB/在线元数据搜索；服务身份没有人工文件/规则批准权限。

**真实 NAS/qB/Emby/模型/Discord、SSH 与 arm64 未做部署验收。** 导入镜像不会开放 LAN 管理；默认仍是回环单用户模式。已实现显式 opt-in 的 HTTPS 管理身份、有限会话/节流和独立 CSRF，需要部署者提供客户端正常信任的 TLS 证书/私钥、独立口令 verifier、明确 origin/监听与只读挂载；不生成实际凭据，不要求忽略证书警告。登录 UI 的最终整合及实际浏览器验收以集成交付版本为准。已有授权 SSH 的回环隧道仍是可选默认模式访问方式。

- [当前能力、版本和验证限制](docs/STATUS.md)
- [本机管理 UI/API](docs/LOCAL_MANAGEMENT.md)
- [可选 HTTPS 管理身份与浏览器合同](docs/MANAGEMENT_ACCESS_CONTRACT.md)
- [命名辅助与语义快照合同](docs/NAMING_ASSISTANCE_HANDOFF.md)
- [规则候选审核合同](docs/RULE_PROPOSALS_FRONTEND_HANDOFF.md)
- [事件与通知](docs/NOTIFICATIONS.md)
- [本地出包 → 解包取image.tar → 极空间GUI导入（Compose可选）](docs/PACKAGING.md)
- [只读 NAS 预检与运行边界](docs/RUNNING.md)
- [已实现接口与未来接口的区分](docs/API.md)

```sh
make verify
make verify-ui
make package-test
make package-nas ARCH=amd64 PACKAGE_OUT="$PWD/dist"
```

Go 检查需要真实 Go 1.26.9；UI 测试需要 Python3.12、`tests/requirements.txt` 与 Chromium。标准出包在Docker内编译；用户电脑无需另装Go/npm，NAS无需源码。极空间GUI导入先解包外层tar.gz并选择image.tar，不要求NAS CLI；GUI的host模式未验，导入不会开放LAN管理。既有candy为Caddy外网反代，配置/证书/权限未检查，本包不修改或复用。Windows 使用 WSL2 + Docker Desktop 的 Linux containers，完整前提见 PACKAGING.md。CI 保留完整 Go/UI 门及独立包装门，包内 VERSION.json 绑定源码 SHA；配置模板默认无来源和凭据，HTTPS 管理/事件/learning/自动命名均关闭。包不包含实际证书、私钥、verifier 或密码。出包和配置校验不等于真实部署成功。

CLI 默认仅校验配置、不联网；`-run` 启动 daemon；`-serve` 同时启动管理页和 daemon，会访问用户明确配置的服务。配置来源 URL、已有 qB/Emby 凭据只在本地私密配置/环境，不提交或进入日志。qB 与本程序必须报告同一下载路径，Emby Path 必须与媒体库路径一致；下载/库需要同设备且不重叠。
