# 部署准备交接 / 2026-10-12

基线：`ceef1f455412df8443d4465730cf5e45ade397f0`，从同一 Library bundle v6 恢复。
候选分支：`codex/nas-deploy-prep-20261012`；最终 HEAD 与 Library 替换结果随交付回复提供。
实现 owner 单人执行，最终审查留给父任务。实际模型、推理档位和服务层级未由运行时暴露，均未知。

## 变更与证据

- 业务源码不变。增强已有订阅回归：第 1 页完全是 100 条历史记录，随后 100 条当前来源各自恰好一次抓取。
- 既有四项定向 Go race 回归通过；增强后的订阅回归再次通过。
- 实际 `make verify-ui` 通过：Chromium 154.0.8037.57、Python 3.12.14、Playwright 1.62.0。
  旧预览成功/失败覆盖输入、载入、静默赋值、reset、导航；包含当前快照保存门禁、订阅翻页和后页规则作用域。
- Actions YAML 本地解析与入口/顺序检查通过。已有 Go1.26.9、make verify、make verify-ui 保留；Python setup 前移。
- 新增 `scripts/nas-preflight.py`、5 组安全回归与 RUNNING.md 使用说明。默认只读和明确隔离写探针均用临时合成目录验证；实际当前环境的只读预检确认 Docker/openat2 可见，硬链接及部署仍未验。
- 标准镜像构建准确失败于依赖下载：proxy.golang.org:443 connection refused。没有改下载通道。
  `Dockerfile.offline` 使用已有 cache/download、go.sum 校验和 network=none 实际构建成功。
  UID65532、无网络、只读 rootfs、drop capabilities、no-new-privileges 下，仅挂公开样例副本作配置校验，返回 configuration_valid。

最终 `make verify`：真实 Go1.26.9 linux/amd64 一次完整通过；gofmt/vet、两个 Go 包全部 race tests、build、5 组预检安全测试成功（领域包 63.608 秒）。
`git diff --check` 通过；asuka-agent 工作区检查为 clean。
本地离线镜像 `asuka-nas:deploy-prep`：`sha256:9e007c59a629587ad4225ec76c7955c6888ace947f8062b116611414c9c20569`，linux/amd64；不作为跨环境可用的镜像交付或生产验收。

## 复现与验收边界

使用真实 Go1.26.9；若环境中的 go 同名程序不是 Go，设置显式 PATH 或使用 scripts/dev.sh。
项目验证入口与预检/镜像命令见 RUNNING.md。云环境验证使用独立本地依赖缓存，未修改 asuka-agent 或其配置。

当前没有可用独立远端，不能提供线上 Actions 成功链接。标准联网构建阻塞仍保留；有完整合规缓存的离线构建已完成。
真实 NAS 挂载、源所有权/ACL、容器 linkat、SQLite 持久性及 qB/Emby 路径一致性都需部署者以后实测。
普通硬链接探针成功只限其隔离目录；同设备/同 mount 与只读 access 均不能证明生产入库。
前端继续 loopback 本机使用；真实 NAS/qBittorrent/Emby/Discord 和 arm64 没有验证。
下载原文件保留做种，0–7 天到期只停止不删数据；通知尚未接入，普通通知后续由 gateway 直调 Discord，模型仅用于疑难命名。

本轮不创建远端、不推送、不合并、不部署、不接凭据；bundle 只包含已提交源码和完整历史，不含工具链、缓存、镜像、数据库、临时截图或私有配置。
