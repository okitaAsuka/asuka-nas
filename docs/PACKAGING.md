# 本机出包与极空间 GUI 导入 image.tar

本流程参考 asuka-agent `5e2d572293ed0074bdc5280caffe10edf18eafb0` 的
`scripts/build_docker_and_save.bat`（docker build → docker save）；该版本 Makefile 没有发布 target。
NAS本流程只构建、出包，不推registry、不启动服务、不更改qB/Emby或Caddy，不删除旧包/数据。

用户当前是极空间：在本地打Docker镜像包，再经极空间GUI导入；NAS Docker CLI/Compose/SSH权限尚未核验，不作为本轮前提。已有名为candy的Caddy做外网端口反代，配置、证书和权限未检查；不自动修改或复用，不把管理页默认公开到公网。

**GUI要选择解包后的 `image.tar`，不是外层 `asuka-nas-...tar.gz`。** 外层归档还提供公开模板、版本与校验清单。Compose仅可选参考；导入镜像不代表NAS LAN地址可访问管理页。

## 用户电脑本机构建（不要求 NAS CLI）

需要 Git、GNU Make、Python 3.12+、Docker Engine/Desktop（Linux containers）及网络。
Go 1.26.9、依赖、嵌入 UI 均在 Docker 构建阶段完成；NAS 无需 Go/npm 或源码。
Docker 基础镜像标签与依赖下载需要网络；“离线”指交付包在 NAS 导入运行不拉镜像。
这是可重复的操作步骤，不承诺跨时间字节一致（基础镜像 tag 未固定 digest）。

Windows：安装 WSL2 Ubuntu 24.04（Python 3.12）与 Docker Desktop，开启该发行版的 WSL integration；
在 WSL 终端安装 `sudo apt-get install git make python3`，将仓库 clone 到 WSL 的 `~/src`，
执行 `docker info` 确认 Linux daemon 可用。无需在 Windows 安装 Go/npm。

```sh
cd ~/src/asuka-nas
# 先提交所有已跟踪改动，构建绑定已提交 HEAD；不跟踪的配置/凭据不会复制。
make package-test
make package-nas VERSION=v0.1.0 ARCH=amd64 PACKAGE_OUT="$PWD/dist"
```

若已有经过可信下载的完整 Go module 下载缓存，可显式选择仓库已有的离线 Dockerfile：

```sh
make package-nas VERSION=v0.1.0 MODULE_CACHE="$(go env GOMODCACHE)/cache/download"
```

MODULE_CACHE 只接受现有名为 download 的下载缓存目录（不是整个模块目录或用户目录）。
依赖步骤无网络，并执行 go mod verify；基础镜像仍需已缓存或可拉取。缓存不进入运行镜像/归档。
这条路径不关闭在线 TLS 校验，不能替代依赖来源的信任验证。

VERSION 默认 HEAD 的12位 SHA；ARCH 默认 amd64。只接受 amd64/arm64；arm64 是未验候选，
需要本地 Docker 的交叉构建能力及目标机独立验证，不能据出包成功声称支持。
脚本拒绝非法版本、脏跟踪文件、错误平台及同名包；失败只清理本轮临时目录。
不会清理构建生成的 Docker 镜像/缓存。参数为空使用默认值。

产物 `asuka-nas-VERSION-linux-ARCH.tar.gz` 及同名 `.sha256`。包内只有 image.tar、
Compose、配置/凭据模板、.env（无个人路径）、部署说明、VERSION.json 和 SHA256SUMS。
构建上下文由已提交的 go.mod/go.sum/cmd/internal 白名单生成，运行镜像只含程序与基础运行库；
不复制仓库配置、数据库、下载、媒体、缓存、.git 或主机凭据。元数据含源码 commit、镜像 ID、平台。
归档与 SHA 清单不提供签名真实性；需通过可信渠道搬运，先检查校验再解包。

## 极空间 GUI 主流程（真实 NAS 未验）

先在用户电脑核对外层.sha256并解包，检查SHA256SUMS及VERSION.json的源码SHA/平台/image。再通过极空间Docker GUI镜像导入功能选择 **image.tar**；具体按钮与host模式支持未实测。标准包是linux/amd64，不能根据品牌假定NAS架构；arm64候选未验。

从config.template.json另建私密config.json，填已有授权服务和真实路径，保留events/learning/automatic_naming关闭。GUI字段按下表核对：

| GUI 项目 | 候选设置 / 要填写的值 | 边界 |
| --- | --- | --- |
| 镜像 / 容器名 | 导入image.tar；镜像名称见VERSION.json；新建独立asuka-nas实例 | 不修改现有qB/Emby/Caddy；同状态库只运行一实例 |
| 网络 | 若GUI支持Linux host，可作当前回环候选 | GUI能力未核验；只有bridge时，映射8787不能访问容器127.0.0.1，当前尚不能直接LAN管理 |
| 端口 | host不填ports映射；管理仅127.0.0.1:8787；8788/8789默认关闭 | 不设公网/LAN发布或Caddy反代来绕过鉴权 |
| 配置卷 | 已有私密config.json → /config/config.json，只读 | 若GUI只能挂目录，私密配置目录 → /config只读；所选用户须已有读取权 |
| 状态卷 | 已有独立可写状态目录 → /data/asuka-nas | 保存SQLite；须已有权限，不覆盖现有库或擅自迁移生产库 |
| 媒体卷 | 下载/库共同父目录 → /media；目标库可写 | 一次挂共同父目录；同设备且不重叠。容器路径须精确匹配qB save_path及Emby Path；示例不是实际路径断言 |
| 环境变量 | ASUKA_NAS_QB_USERNAME、ASUKA_NAS_QB_PASSWORD、ASUKA_NAS_EMBY_TOKEN，填已有凭据 | GUI环境输入与credentials.env模板是替代填法；不生成token。关闭events/learning时不必填服务身份 |
| 容器用户 | 镜像默认65532:65532；GUI如支持可选已有授权非root UID/GID | 配置读、状态写、源读和库链接权限须已具备，不自动chown/改媒体权限 |
| Entrypoint | 保留/usr/local/bin/asuka-nas | 不替换为shell或反代 |
| 首次只校验参数 | -config /config/config.json | 不加-run/-serve；不访问上游、不打开状态库；校验完成退出是正常行为 |
| 经用户审查后的服务参数候选 | -serve -listen 127.0.0.1:8787 -config /config/config.json | 会启动daemon并访问授权上游；不是无副作用UI模式，本轮不启动真实NAS |
| 重启 / 安全选项 | 首次不自动重启；GUI如支持，保留只读根、cap_drop ALL和no-new-privileges | 功能支持未核验；不开特权/root绕过失败 |

host只使监听落在NAS回环，仍无LAN登录/鉴权。若极空间GUI不支持当前条件，管理访问方案尚待父设计；本轮不扩大监听、不新增Caddy/LAN实现，不等待SSH探查。本机浏览器/已有授权SSH只是后续可选访问候选，均未验。

## 可选 NAS CLI / Compose 参考：host 与回环

以下仅在用户已有NAS CLI/Compose权限时参考，GUI导入不要求这些命令。需要Linux amd64、兼容Docker Compose v2（支持pull_policy与create_host_path）、
Linux 5.6+ openat2 及 /proc/self/fd。NAS 厂商 GUI 只支持 bridge 时不能直接采用本候选。
普通 bridge 下 8787/8788/8789 监听的是容器回环，ports 映射不可使它们可达。
host 网络让监听保持 NAS 回环，不提供 LAN 管理身份认证。端口冲突须先检查，不能停用原服务。
管理页保持精确 Host/Origin、CSRF 保护；不加反代、端口映射或更改业务监听。

```sh
sha256sum -c asuka-nas-v0.1.0-linux-amd64.tar.gz.sha256
tar -xzf asuka-nas-v0.1.0-linux-amd64.tar.gz
cd asuka-nas-v0.1.0-linux-amd64
sha256sum -c SHA256SUMS
docker load -i image.tar
cp config.template.json config.json
cp credentials.env.template credentials.env
```

部署者自行填写 .env 中已有状态目录和媒体共同父目录；填 config.json 的实际 qB/Emby URL、
下载/库路径、合法来源及 credentials.env 中已有凭据。此流程不生成 token、不修改权限。
目录必须事先存在，默认容器 UID/GID 65532 必须已有适当访问权；若不满足由部署者审查现有权限策略，
不要直接 chown 现有 qB/Emby 数据。可以将 .env 的 NAS_UID/NAS_GID 改为已有授权账号的
数字 UID/GID（非 root），使容器使用已有文件所有权；不自动修改账号或主机权限。
解包/cp 会遵循本机 umask，0600 配置只有其所有者可读，因此必须核对配置所有者与容器 UID，
不能仅凭 archive 的公开模板模式假定可读。凭据文件按现有私密文件策略保管，不随归档分享。

媒体共同父目录一次挂到 /media，downloads/library 必须同设备且不重叠。
qB 报告的 save_path 必须与 NAS 配置下载路径完全一致；Emby 返回媒体 Path 必须与库路径一致。
保留现有 qB/Emby 服务及挂载；若它们已有路径不是 /media/...，只调整本候选 Compose 的 target
与配置以匹配已有路径（允许挂共同父目录），先做路径核验。不得为了本包改已有服务。
host 网络没有 Compose 服务名 DNS；示例回环 URL 只适合已有服务确实在 NAS 回环对应端口可达，
否则部署者填写已授权可达地址。不要通过修改现有网络或开启外网监听解决。

先用默认只校验模式，既不访问服务也不开数据库：

```sh
docker compose config --quiet
docker compose run --rm --no-deps asuka-nas -config /config/config.json
# 上述均通过且已审查配置、目录/凭据/服务版本后，部署者自行决定启动。
docker compose up -d --pull never
```

Compose 只定义 asuka-nas，restart 默认 no。-serve 启动管理页与 daemon，会访问授权配置里的服务；
不是只看 UI 的无副作用模式。events/learning 默认关闭，不注入它们的身份。
需要使用时必须由父任务另行确认已有独立身份、消费者位置和安全合同；不同主机上的 agent
无法访问 NAS 回环服务。LAN 监听/鉴权扩展留给业务 owner，不在打包层绕过。

管理页可在 NAS 本机浏览器访问 http://127.0.0.1:8787。
若 NAS 已有授权 SSH 服务，可在用户电脑建立保持相同端口的回环隧道：

```sh
ssh -N -L 127.0.0.1:8787:127.0.0.1:8787 YOUR_EXISTING_NAS_SSH_ALIAS
```

电脑浏览器同样访问 http://127.0.0.1:8787，Host/Origin 与服务预期一致。
本机8787必须空闲；不能改映射端口、用 localhost 名称或暴露隧道到 LAN。
不配置/启用 SSH，也不创建 SSH 凭据。未进行真实 NAS/SSH/上游服务验收。

升级前部署者自行备份私密状态，停止旧 asuka-nas 单实例，load 新镜像，用新包明确指向原状态目录。
不要并行启动两个实例，不运行 down -v/prune，不删除旧归档或数据。数据库升级可能不兼容旧程序；
回滚须恢复升级前备份与对应镜像，不能只切旧镜像。不要覆盖已有 config/credentials 文件。
