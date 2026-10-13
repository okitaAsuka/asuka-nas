# 人工对比 → agent候选 → 人工终审：前端交接（2026-10-13）

## 检查点与唯一写入者

基线 NAS `c190532a31e13bad0654aa02f6825826665546c9`；分支 `codex/nas-rule-proposals-20261013`，交接回复提供准确SHA。
本轮只修改后端/业务测试/文档。`internal/nas/web/*`、所有浏览器测试、Makefile和CI文件未改；前端owner可接管这些文件，后端owner不会写入。
本检查点是前端接口交接，尚非两仓里程碑验收。后端剩余验证见 RULE_PROPOSALS_EVIDENCE.md，独立前端及父集成完成后才验收。

## 页面建议与边界

在现有命名规则页加入“agent待审建议”，复用已有安全DOM、请求门、预览快照和状态归属。以候选清单 → 原始人工依据 → 规则范围/解释/例外 → 历史试算 → 批准/拒绝表达。
候选本身不会生成本次文件计划、改变下载/做种或迁移冻结文件。不要将候选提交成功写成规则已启用；只有 `approved` 才代表人批准并原子启用。
`pending` 待审、`approved` 已批准、`rejected` 已拒绝、`stale` 依据过期或已被新revision取代；状态同时有文字，不能只靠颜色。
不按模型置信度自动批准。不要自行填出未来季号或扩展episode区间；本轮保守限于已观察连续集数，模型不能表达的映射会无候选。
人工修改先保存为新revision，再重新试算；输入/候选revision/version/页面/导航/取消变化使旧试算失效。批准只能使用成功试算返回的 `epoch/digest` 和同一候选版本。显示所有依据和试算覆盖集合、冲突与范围外结果；`truncated=true` 必须显示覆盖有限，不承诺全历史或所有未来资源。
前端不得使用学习服务Bearer，不新增凭据或改变CSP/Origin/CSRF。所有文件名/标题/模型解释都是不可信字符串，按textContent渲染，不执行HTML/链接/脚本。

## 本机管理路由

沿用 `/api/session` nonce，POST须 `Content-Type: application/json`、同源Origin、`X-CSRF-Nonce`。Host/CSRF/单写门未改变。GET不需要写nonce。

| 方法与路由 | 参数 | 成功响应 |
| --- | --- | --- |
| GET `/api/proposals?after=0` | 非负audit创建seq游标 | `{items: RuleProposal[], next: number}`；每页20，next=0表示无下页 |
| GET `/api/proposal?id=…&revision=1` | id、正整数revision | RuleProposal；此读取核对依据，pending失效会持久标stale并增加version |
| GET `/api/proposal-audit?id=…` | id | `{items:[{seq,revision,kind,created_at}]}`，最近100条 |
| POST `/api/proposal-revise` | `{id,revision,version,input:ProposalInput}` | 新RuleProposal；旧版stale，revision+1/version=1 |
| POST `/api/proposal-preview` | `{id,revision,version}` | ProposalTrial，零媒体/规则写入 |
| POST `/api/proposal-approve` | `{id,revision,version,epoch,digest}` | `{ok:true}`；原子CAS/试算/规则存储/启用/审计 |
| POST `/api/proposal-reject` | `{id,revision,version}` | `{ok:true}`；不创建或启用规则 |

相同批准/拒绝请求重放幂等，version必须仍是原请求的版本。修改或过期版本拒绝。刷新后读取最新状态。

```json
{
  "id":"rp_40位集合摘要前缀","revision":1,"version":1,"state":"pending",
  "origin":"agent","created_at":1790000000,
  "input":{
    "protocol":1,
    "samples":[{"sample_id":"64位sha256","digest":"64位plan摘要","export_digest":"64位允许导出投影摘要"}],
    "rule":{
      "id":"rp_40位集合摘要前缀","revision":1,"series_id":"example","provider":"manual","order":"custom",
      "subscription":"fixture","group":"","aliases":["Example"],"season":2,
      "directory":"Example","display_name":"Example","episodes":{"start":1,"end":1}
    },
    "explanation":"本次人工确认第二季第一集；只适用已观察范围。",
    "uncertainties":["不能推断未来篇章"],"exceptions":[]
  }
}
```

Rule使用现有NamingRule字段；可选 `identity_normalization_version` 仅0/现版本1；可选 `absolute:{start,end,relative_start}` 表达有限总集映射。ID和revision由后端固定，编辑表单不允许用户改id/依据集合；只可在同一依据内修改规则字段与解释。样本1–8个、别名1–8个、说明1–2000字节、例外与不确定性各≤8条/每条≤512字节。季1–99、集1–999，连续范围最多32集且每集必须有成功人工依据。目录相对路径≤512字节、显示名单段≤200字节。禁止URL/GUID/magnet/tracker/凭据、任意regex、命令或脚本字段；未知JSON字段拒绝。

```json
{
  "id":"rp_…","revision":1,"version":1,"epoch":0,
  "coverage":[{"sample_id":"…","digest":"…","export_digest":"…"}],
  "results":[{"sample_id":"…","source":"Example [01].mkv","target":"Example/Season 02/Example - S02E01.mkv","outcome":"matched"}],
  "conflicts":0,"digest":"64位sha256","limit":100,"truncated":false
}
```

试算结果 `matched/not_matched/out_of_scope/conflict`。coverage是准确样本集合，最多2048条结果、1000条当前身份规则、100份已导出成功样本（显式依据优先，其余最新序号）。不是全数据库、NFO扫描或真实NAS兼容保证。冲突包括人工目标不一致、现有启用/停用身份规则矛盾。冲突数量>0禁止批准。试算无外部网络/文件操作。

前端依据详情：候选仅有SampleRef，需要显示原始成功依据时调用下文新增 `/api/proposal-samples`。返回LearningSample投影；原完整对比记录仍可按job_id调用既有 `/api/comparisons?id=…&after=…`。

## 授权样本详情

GET `/api/proposal-samples?id=…&revision=1` → `{items:LearningSample[]}`。每份含：

```json
{"protocol":1,"ref":{"sample_id":"…","digest":"…","export_digest":"…"},"job_id":"opaque","job_version":7,"plan_version":8,"context_version":1,"parser_version":4,
"changes":[{"semantics_resolved":true,"source":"Example [01].mkv","target":"Example/Season 02/Example - S02E01.mkv","season":2,"episode":1,"before_episode":1,"changed_fields":["target_relative_name","directory","season"]}],
"contexts":[{"subscription":"fixture","title":"Example [01]"}]}
```

可选before_season/before_episode/before_absolute省略代表未知；所有人工样本semantics_resolved=true，只含相对名与冻结来源标题。详情可查看历史冻结投影，即使候选已stale，也需显示状态，不把它当当前可批准依据。

## 错误与恢复

一般失败409 `{error:"固定码"}`；不存在404；格式错误400；忙碌429；服务不可用503；权限失败403。沿用operation_failed隐藏驱动/秘密原文。

- `proposal_invalid/proposal_unbounded/proposal_evidence_conflict`：字段/范围/依据不合法，保留编辑并解释，不强制通过。
- `proposal_stale/sample_stale`：候选或依据过期，刷新详情；不能重用旧预览。
- `proposal_trial_stale`：epoch或试算摘要变化，重新预览，禁止直接重试批准。
- `proposal_trial_limit`：当前规则数或显式依据试算超限，收窄依据或离线治理；不能截断必要依据后批准。
- `proposal_conflict`：显示试算冲突并修正规则。
- `proposal_revision_exists/proposal_replay_conflict`：重复版本或同依据不同agent正文，刷新保留历史。
- `proposal_not_found/proposal_limit`：不存在/达到10000条revision限额；不静默裁剪历史。
- `operation_busy/csrf_rejected/origin_rejected/invalid_request`：沿用现有请求恢复边界。

## 与agent的协议（前端不调用）

独立默认关闭回环服务 `127.0.0.1:8789`，独立已部署env引用；与事件token/浏览器nonce隔离。`GET /v1/learning/samples?after=0` 返回 `{protocol:1,items:[{seq,sample:LearningSample}],next_cursor}`，每页8、扫描100。只在完整成功后分配seq，早期pending不会被cursor跳过；标题/路径不合适者保守拒绝。历史样本不可变，当前有效性由服务重新验证。
`POST /v1/learning/check` `{protocol:1,samples:SampleRef[]}` → `{protocol:1,ok:true}`；推理前检查版本，成功提交/人工批准仍再次核对。
`POST /v1/learning/proposals` ProposalInput → `{protocol:1,id,revision:1,state}`。服务身份没有批准、拒绝、修改或媒体权限。同依据集合+同正文ACK重放返回原candidate；不同正文409。模型和通知链独立，agent离线不影响NAS。

## Fixtures和验收归属

可复用 `internal/nas/rule_proposals_test.go:learningFixture` 创建真实SQLite/临时文件/fake qB/Emby的成功人工记录；人工批准API已提供，不需要前端造数据库。
fixture示例源 `Example [01].mkv` → `Example/Season 02/Example - S02E01.mkv`，订阅fixture；预览/确认后仍需f.discovered=true及tick才能进入样本feed。截图若用静态数据必须与真实API链验收分开说明。
本轮前端需补浏览器候选分页、原始对比、编辑新revision、全部试算、批准/拒绝、epoch过期/冲突恢复、双击、导航晚响应、键盘/窄屏视觉检查；后端owner不编辑这些测试。
