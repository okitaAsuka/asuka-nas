# 管理 HTTPS / 登录合同（功能 lane，2026-10-15）

状态：本文件先冻结前后端合同；实现与实际验证结果见 FEATURE_CLOSURE_EVIDENCE.md。不是部署记录。既有回环模式保持兼容；UI owner 独立实现登录/退出界面。

## 模式与配置

`management` 缺省或 `enabled:false` 时，`-serve -listen 127.0.0.1:8787` 保持原本机单用户模式；无登录要求，不允许非回环监听。`-run` 不启动管理服务。单纯配置校验不读取秘密、不监听或联网。

认证模式只有 `management.enabled:true` **且** `-serve` 才启动。此模式拒绝同时显式传入 `-listen`，避免默默忽略另一地址来源。部署者需自己提供可信 TLS 证书/私钥与独立的人类登录凭据；不得以浏览器忽略证书警告作为操作步骤，不使用/修改 Caddy。

```json
{
  "management": {
    "enabled": true,
    "listen": "0.0.0.0:8787",
    "origin": "https://nas.example.test:8787",
    "username": "admin",
    "password_hash_env": "ASUKA_NAS_MANAGEMENT_PASSWORD_HASH",
    "tls_cert_file": "/run/asuka-tls/server.crt",
    "tls_key_file": "/run/asuka-tls/server.key"
  }
}
```

以上只是公开配置片段，不是完整部署配置或凭据。listen 必须明确给出 IP 字面量及非零端口，允许回环、私有地址或容器内通配地址；拒绝公网 IP。origin 必须是规范 HTTPS origin（无路径/查询/用户信息），精确匹配浏览器访问的 Host；监听地址与浏览器地址可不同以支持部署者自行配置的 Docker 映射。不信任 Forwarded/X-Forwarded-*；不按请求动态推断 origin。TLS 最低 1.2，证书须在有效期内、适用于 origin 主机且允许 serverAuth。部署者须让客户端正常信任证书链；本服务不签发证书或安装信任。

password_hash_env 引用部署者已有 PBKDF2-HMAC-SHA256 verifier，格式 `pbkdf2-sha256$600000$<salt>$<derived-key>`；salt/derived-key 使用**无 padding 标准 Base64**，salt 16–64 字节，derived-key 32 字节，迭代数 600000–1000000。用户名为 1–64 字节 `[A-Za-z0-9][A-Za-z0-9_.-]*`，登录密码为 12–1024 字节有效 UTF-8。不提供创建实际凭据的命令/接口。环境引用及实际密码必须独立于 event/learning bearer 身份；启动验证拒绝相同引用或相同实际密码。没有注册、重设密码或权限管理接口。

启动仅输出固定 `startup_failed: management_*` 码：`listen_invalid`/`origin_invalid` 修正明确地址；`listen_conflict` 移除重复 `-listen` 来源；`credentials_invalid` 核对用户名/env 引用；`credentials_unavailable` 核对部署者提供的 verifier；`identity_reused` 使用独立人类凭据；`tls_required`/`tls_unavailable` 核对既有证书/私钥路径与读取权限；`tls_invalid` 核对主机名、期限、serverAuth；`listen_unavailable` 核对显式地址可绑定性。每个后缀前都有 `management_`。不打印秘密、路径或系统原始错误。恢复配置后重新启动，所有旧会话失效；不提供 HTTP 降级或证书验证绕过。

## 浏览器接口（精确合同）

所有响应 `Cache-Control:no-store`。所有 POST 都要求精确 `Origin`、`Content-Type:application/json`，并拒绝 `Authorization`；其他 API 的 CSRF/CAS/完整 preview-confirm 语义不变。Host/Origin 检查先于任何鉴权；TLS 模式拒绝明文请求，不从 forwarded headers 接受 TLS 声明。

| 请求 | 成功响应 | 无会话 |
| --- | --- | --- |
| `GET /api/session` | 200，见下方 DTO | 200，只返回认证模式/未登录状态，无 csrf |
| `POST /api/login`，`{"username":"…","password":"…"}` | 200，与已登录 session DTO 相同；设置会话 cookie | 不需要已有 cookie/CSRF；精确 Origin + JSON 是登录 CSRF 门禁 |
| `POST /api/logout`，`{}` | 200 `{"ok":true}`；清除 cookie 并撤销该会话 | 401 `authentication_required` |
| 其他 `/api/*` | 沿用既有合同 | 401 `authentication_required`；不返回任务/配置/nonce |
| `GET /`、`/app.js`、`/style.css` | 200，可在登录前加载；只含公开静态资源 | 同左 |

三个 access endpoint 不接受 query 参数。`/api/login` 仅 POST，`/api/logout` 仅 POST，`/api/session` 仅 GET；错误方法 405 `method_rejected`。回环模式 session DTO 增加字段但保留原 `csrf`：

```json
{"mode":"local_single_user","authenticated":true,"csrf":"<process nonce>"}
```

认证模式未登录：

```json
{"mode":"authenticated_https","authenticated":false}
```

认证模式登录成功 / 有效 session：

```json
{"mode":"authenticated_https","authenticated":true,"csrf":"<session nonce>","expires_at":1790000000}
```

`expires_at` 为 Unix 秒，固定创建后 12 小时，读取/活动不延长。无用户名/口令/verifier/session id 回传。`csrf` 只保存在页面内存中；退出、401、刷新或重启后重新取得当前 session，不能在 localStorage/sessionStorage 保存秘密。无会话返回没有 csrf 字段；UI 必须停止自动重试敏感 API、清除旧预览批准状态并显示登录入口。登录成功后重新读取当前任务/版本并重新预览，不能自动重放之前的 POST。

会话 cookie：`__Host-asuka_session`，随机 32 字节（hex），`Path=/; Secure; HttpOnly; SameSite=Strict`，不设 Domain；Max-Age 与 Expires 均为固定 12 小时期限。服务器仅保存 cookie 的 SHA256 索引、独立随机 CSRF nonce 与到期时间，不写入数据库/磁盘。最多 16 个有效会话；已登录浏览器重新登录会原子替换并撤销旧会话。达到上限时不驱逐其他会话、不授予新会话。重启全部失效。丢失 cookie 只能重新登录或等待旧会话到期；不授予绕过容量的恢复权限。

`POST /api/logout` 和所有其他业务 POST 必须提供当前会话的 `X-CSRF-Nonce`。旧会话/另一会话/本机模式的 nonce 无效。退出会话立即拒绝后续请求；已经通过鉴权并开始的业务操作保持原不可变意图及恢复语义，不回滚已授权效果。不同浏览器会话互不注销。

## 固定错误与节流

| HTTP | `error` | UI 行为 |
| --- | --- | --- |
| 400 | `invalid_request` | 修正请求；不显示服务器原始异常 |
| 401 | `authentication_failed` | 用户名/密码不匹配统一提示；不区分账号是否存在 |
| 401 | `authentication_required` | 清除当前授权/批准状态，转登录 |
| 403 | `origin_rejected` / `csrf_rejected` / `https_required` | 停止重试，核对页面 origin/会话/HTTPS |
| 404 | `not_found` | 本机模式不提供 login/logout；未知静态路径拒绝 |
| 405 | `method_rejected` | 方法错误 |
| 429 | `login_throttled` | 尊重 Retry-After 秒数，不自动循环登录 |
| 429 | `session_limit` | 尊重 Retry-After；先从其他已登录浏览器退出或等待会话到期 |
| 503 | `authentication_unavailable` | 暂不可登录；不要打印输入或 verifier |

login 请求体最多 4096 字节，严格 JSON/未知字段拒绝。全局每分钟最多 20 次、每个真实 remote IP 每分钟最多 5 次登录尝试，成功和失败均计数；不信任 forwarded IP。最多保存 256 个 IP bucket，满时拒绝新 bucket，不驱逐未到期 bucket。最多一个 PBKDF2 验证在途，多余立即 429；不会创建无界等待队列。429 的 Retry-After 是整数秒、至少 1。节流在进程内，重启清空；不声称跨重启防爆破持久化。

## UI owner 所需 hooks

1. 先 GET session，再决定登录页或工作台；匿名也能加载三份静态资源。
2. login 使用 JSON、同源 credentials；密码仅存在当前表单/请求，成功/离开立即清空。不得发送为 URL 参数或 Authorization。
3. 登录后把返回 csrf 放内存，所有业务 POST/logout 附加 X-CSRF-Nonce。
4. session/业务 401 后使在途页面 generation、已批准预览失效；登录后重新获取事实。不得自动重发确认。
5. logout 成功清除内存会话/任务批准状态；取消旧请求；回到登录页。403/429/503 按上表显示。
6. 本机模式仍直接进入既有工作台；无需密码，也不显示无效 logout。

## 交给父任务整合的共享文档新增内容

- STATUS.md：默认回环入口保留；新增显式 opt-in、部署者提供可信 TLS/独立口令 verifier 的 HTTPS 管理模式。只完成 synthetic/CI 验证，真实 NAS/GUI/证书信任待验。
- LOCAL_MANAGEMENT.md：回环访问限制仍适用于默认模式；认证模式改指本合同，禁止直接暴露旧回环入口或用反向代理绕过鉴权。session/login/logout DTO 见本合同。
- PACKAGING.md / 模板：仅由父任务决定是否加入公开 management 配置片段；实际证书、私钥、verifier、密码绝不进入包。不得建议接受证书警告；Docker 映射和客户端信任仍由部署者完成并验证。
