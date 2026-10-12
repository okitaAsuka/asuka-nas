# 运行与部署门槛（尚未执行部署）

CLI 无参数模式只做配置检查；`-run` 开始持续轮询。Dockerfile 的 CMD 现在显式 `-run -config /config/config.json`，没有 EXPOSE 或管理端口映射。需要纯校验时显式覆盖为 `-config /config/config.json`。本轮没有构建/运行镜像、没有发布镜像或实际 NAS 操作。

部署前确认 Linux 架构、openat2支持、UID65532挂载权限、下载和媒体同文件系统；qB save_path 和本服务 downloads 必须完全一致，Emby 看到的媒体路径也必须一致。目录需要由受信任宿主维护，源文件下载完成后保持只读语义。容器运行用户需能读取下载、在媒体目录创建硬链接、写数据库父目录。EXDEV只报错，不复制。

凭据由部署者通过既有环境变量提供，RSS URL放私有本地配置。不要提交 config.json、.env、SQLite、torrent blob或凭据。数据库包含私密元数据，应保管备份。服务停止后备份数据库文件（确认正常关闭/WAL检查点），不要只在运行时复制主db忽略WAL。恢复在隔离目录用同版本服务验证，不能让两个实例共用同库。

Web 首版仅回环单用户。容器中的回环端口不能直接用于远程 NAS 管理；这是尚未通过部署鉴权门的功能边界，不能通过开放监听地址来绕过。LAN Web、远程 gateway 与 Discord 待明确身份认证和独立实现后再验收。
