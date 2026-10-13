# 本机出包与 NAS 离线导入

本流程参考 asuka-agent `5e2d572293ed0074bdc5280caffe10edf18eafb0` 的
`scripts/build_docker_and_save.bat`（docker build → docker save）；该版本 Makefile 没有发布 target。
NAS 本流程只构建、出包，不推 registry、不启动服务、不更改 qB/Emby，不删除旧包/数据。

## 本机构建

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

VERSION 默认 HEAD 的12位 SHA；ARCH 默认 amd64。只接受 amd64/arm64；arm64 是未验候选，
需要本地 Docker 的交叉构建能力及目标机独立验证，不能据出包成功声称支持。
脚本拒绝非法版本、脏跟踪文件、错误平台及同名包；失败只清理本轮临时目录。
不会清理构建生成的 Docker 镜像/缓存。参数为空使用默认值。

产物 `asuka-nas-VERSION-linux-ARCH.tar.gz` 及同名 `.sha256`。包内只有 image.tar、
Compose、配置/凭据模板、.env（无个人路径）、部署说明、VERSION.json 和 SHA256SUMS。
构建上下文由已提交的 go.mod/go.sum/cmd/internal 白名单生成，运行镜像只含程序与基础运行库；
不复制仓库配置、数据库、下载、媒体、缓存、.git 或主机凭据。元数据含源码 commit、镜像 ID、平台。
归档与 SHA 清单不提供签名真实性；需通过可信渠道搬运，先检查校验再解包。

## NAS 候选：Linux host 网络、回环管理

需要 Linux amd64 NAS、兼容 Docker Compose v2（支持 pull_policy 与 create_host_path）、
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
目录必须事先存在，UID/GID 65532 必须已有适当访问权；若不满足由部署者审查现有权限策略，
不要直接 chown 现有 qB/Emby 数据。凭据文件按现有私密文件策略保管，不随归档分享。

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
