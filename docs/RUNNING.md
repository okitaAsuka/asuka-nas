# 运行与部署准备（尚未部署 NAS）

CLI 默认只检查配置；`-run` 开始 RSS/job 轮询。Docker 默认 CMD 为
`-run -config /config/config.json`，用户为 `65532:65532`，没有 EXPOSE 或管理端口映射。
纯校验必须显式覆盖参数为 `-config /config/config.json`；它不会打开数据库或访问上游。
中文前端仍仅允许回环 IP、本机单用户访问。容器内回环端口不能直接用于远程 NAS 管理；
不要扩大监听、映射管理端口或用代理/隧道绕过该边界。LAN 管理需要后续独立认证设计。

## 验证与 CI

真实 Go 1.26.9、Python 3.12：`make verify` 统一运行格式、vet、race tests、build 和预检安全回归。
环境中的 `go` 若是同名非 Go 工具，使用 `GO_BINARY=/absolute/go/bin/go scripts/dev.sh`。
`make verify-ui` 另外实际运行 Chromium；先安装 `tests/requirements.txt` 和
`python3 -m playwright install --with-deps chromium`，或指定 `CHROMIUM_PATH`。
已有 `.github/workflows/ci.yml` 复用这两个入口，Python 安装在验证入口之前。
当前没有可用独立远端仓库，本轮只核对配置并在本地执行入口，未声称 GitHub Actions 通过。

## 只读预检

将 `scripts/nas-preflight.py` 复制到有 Python 3 的 Linux NAS，使用实际存在的绝对目录：

```sh
python3 scripts/nas-preflight.py \
  --downloads /srv/asuka/downloads --media /srv/asuka/library \
  --state /srv/asuka/state
```

默认不创建目录、文件或硬链接，不列举下载/媒体内容，不读配置或凭据，不访问 qB/Emby/RSS。
只读取系统、指定根目录和 `/proc` 元数据，并对当前配置的 Docker daemon 做版本查询
（可能使用部署者已有的远端 Docker context；只输出版本/系统/架构，不输出 context、地址或原始错误）。
不修改网络、安全策略、权限或所有权。JSON 不应包含秘密；路径是操作者提供的非秘密目录。

报告每项包含 `evidence=confirmed/inferred/unverified`（已证实/推断/未验）及
`result=pass/fail/unknown`。退出 1 表示检测到失败，2 表示参数错误，0 仍可有未验项，
`deployment_ready` 始终为 false，不能把预检通过当成部署验收。

- 已证实：当前系统/架构/内核、Docker 可见性、根目录 stat、当前 UID 的 access 检查、
  当前命名空间实际只读 openat2 调用及 `/proc/self/fd` 可见性。拒绝根路径任何一层 symlink。
  按 fd 的 mount ID 核对挂载，而非仅按路径前缀猜测；挂载信息不可见时报告 null。
- 推断：下载与媒体同 `st_dev` 是必要条件，仍可能跨独立 bind mount 得到 EXDEV；
  同 mount ID 也不证明对真实下载文件能成功 link。内核版本号也不能替代容器内 syscall 验证。
- 未验：默认没有任何真实 link；容器与宿主是否映射同一目录、真实源文件所有权/ACL、
  UID65532 权限、嵌套媒体目录、已有 DB 权限、SQLite WAL/fsync 持久性、qB save_path 与 Emby Path。
  当前 UID 与运行 UID 不同，权限项明确未验；即使相同，access 标志也只是操作权限推断。
  Docker CLI 缺失/不可见不会证明 daemon 未安装，报告的是本进程看不到它。

先在宿主检查，再在部署者已有的隔离诊断环境按实际 UID65532 和相同挂载/安全策略复核。
脚本需 Python；生产镜像不含 Python，不为预检增加运行时依赖。勿为通过检查关闭 seccomp
或改权限。生产要求 Linux openat2（通常 5.6+）、`/proc/self/fd`、本地持久 SQLite 目录。

## 可选隔离写探针

只有部署者明确准备的现有空隔离临时目录可使用，必须同时给两个 opt-in 参数。
隔离目录可选在与数据相同文件系统的兄弟目录，不能在 downloads/media/state 内、等于它们或作为其祖先。
工具不创建缺失的用户指定目录，拒绝非空目录；只在私有子目录创建两个探针文件，
验证同 device/inode、link count=2 和 fsync，然后仅删除自己创建的探针文件和子目录。
不会删除任何真实下载文件。

```sh
python3 scripts/nas-preflight.py \
  --downloads /srv/asuka/downloads --media /srv/asuka/library \
  --state /srv/asuka/state --write-probe --probe-dir /srv/asuka-preflight-empty
```

成功只证实该隔离目录、当前 UID 和命名空间的普通硬链接/fsync。
它没有执行生产的 fd 绑定 linkat 流程，也没有跨下载/媒体挂载链接，不能推导真实 NAS 入库成功。
若异常中断留下 `asuka-nas-probe-*`，由部署者检查后清理；工具不扫描或递归清理遗留目录。

## 镜像构建

标准入口保留：

```sh
docker build --progress=plain -t asuka-nas:local .
```

本轮 linux/amd64 实测标准构建：基础镜像下载成功，`RUN go mod download` 在
`proxy.golang.org:443` 对 `github.com/dustin/go-humanize@v1.0.1` 的请求被拒。
没有换代理、改 DNS/网络或走直连替代通道。

已有合规缓存时使用 `Dockerfile.offline`，命名 context 只指向 Go 的 `cache/download` 子树，
不要传入 home、配置、数据库或整个工作区。要求缓存已具备当前 go.mod 的完整依赖且可信；
缺失缓存直接失败，不会补抓依赖。构建内 `go mod download/verify` 按已提交的 go.sum 校验，
依赖阶段与编译阶段均 `--network=none`。基础镜像仍需已缓存或正常获准拉取。
最终用户、CA、entrypoint、CMD 与标准 Dockerfile 一致。

```sh
docker build --progress=plain -f Dockerfile.offline \
  --build-context module-cache=/absolute/go-mod-cache/cache/download \
  -t asuka-nas:local .
smoke_dir="$(mktemp -d)"
# 仅公开无凭据的样例副本；真实配置应按部署 UID 管理权限，不能如此处理。
install -m 0444 config.example.json "$smoke_dir/config.example.json"
docker run --rm --network none --read-only --cap-drop ALL \
  --security-opt no-new-privileges \
  --mount type=bind,src="$smoke_dir/config.example.json",dst=/config/config.json,readonly \
  asuka-nas:local -config /config/config.json
rm "$smoke_dir/config.example.json"
rmdir "$smoke_dir"
```

这是本地无网络配置校验 smoke，不启动 daemon、不挂真实媒体/数据库、不接凭据。
本轮离线镜像已实际构建并校验，不能由此声称真实 NAS、arm64 或外部服务验收成功。
无缓存且标准下载被拒时，最小外部验证是在网络获准的隔离构建主机对同一提交运行
标准 `docker build` 及上面的配置校验；不要通过改通道绕过当前拒绝。

## 后续部署门槛与私密状态

下载和媒体必须同文件系统、同时可见；推荐将共同父目录一次挂入容器，减少独立 bind mount
跨 mount 的硬链接失败。qB save_path 与本服务 downloads 必须完全一致，Emby 看到的媒体路径也必须一致。
仍需真实路径/挂载验收。目录由受信任宿主维护，下载完成源文件保持只读语义。
运行用户需要读下载文件、遍历根目录、在媒体创建硬链接/目录和写 SQLite 父目录；
protected_hardlinks、源所有者、ACL、容器安全策略均可阻止实际链接。EXDEV 只报错，不复制。
保留源文件做种，0–7 天从首次完成计时，到期只 stop，不删数据。

凭据由部署者通过既有环境变量提供，RSS URL 放私有本地配置。不要提交 config.json、.env、
SQLite、torrent blob 或凭据。数据库/备份含私密元数据；停止服务并正常关闭/WAL 检查点后备份，
不能运行中只复制主 DB 忽略 WAL。恢复在隔离目录用同版本验证，禁止两个实例共用库。

真实 NAS、qBittorrent、Emby、Discord 尚未实测。本轮未接通知；后续普通通知计划由 gateway
直调 Discord，模型仅用于疑难命名。本轮不改变认证、业务架构或映射学习，不推送、不合并、不部署。
