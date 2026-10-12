# 任务书：runq 配置模型 v2 与功能收束（v0.6.0rc2）

> 2026-10-12 修订：按研究员的自用场景，choices 直接约束，取消迁移工程；
> 提交 target 固定为显式 -t > job > project；同时精简 web 和后端的对称功能。
> rc2 代码实施已完成（2026-10-12）；本地回归与独立页面 fixture 已验收，尚未安装到运行服务。

先读 `artifacts/design/v0.6-overhaul.md`、`artifacts/design/config-model-v2.md`、
`CHANGELOG.md`。本轮后续基础设施工作见 `task-infra-reliability-v0.6.md`（连接、时间、W&B）。
现行 agent 操作规则在 `internal/skill/runq/SKILL.md`。

## 目标与边界

一个代码库一个 project，一次实验一个 job.yaml。agent 用文件和 CLI 准备实验；
研究员用 dashboard 看进度、日志、结果，并执行停止、重试、归档。
YAML 只保留提交生效的配置，消融参数默认经 `ctx.params` 进入代码。

以完整使用路径决定保留范围。删除功能要覆盖入口、路由、handler、专用后台任务、类型和测试；
共享执行逻辑按剩余调用方保留。CLI 通过同一套 HTTP API 工作，不能按“位于 dashboard 包”判断可删。

## 一、配置决策

1. job 新增 `command`、`target`、`preflight`；资源覆盖仍叫 `overrides`。
   保留现有 `name`、`note`、`description`、`fixed_params`、`sweep`。
2. `command_template` 改为 `command`。project 的 YAML/JSON 接受旧名作为简单别名，
   两个键同时出现时报错，序列化只输出 `command`。job 使用新名。
3. `choices` 只要非空就强制校验，旧文件也采用同一语义。`strict` 删除，
   即使旧文件带 `strict: false` 也不能关闭 choices 约束。空列表不约束。
4. 删除参数元数据 `default/min/max/include/glob/strict`；旧字段容忍读取但不生效、不写回。
   不建 web-presets、不做 suggestions、不加 schema_version、不写自动迁移系统。
   原来只作建议的 choices 由使用者按需删掉；升级说明直接写清这个变化。
5. 参数默认值放代码里：`ctx.params.get("lr", 1e-3)`。`params.json` 记录传入值，
   不宣称包含代码内部补齐的全部默认值。`params` 声明可选，仅保留 type/scope/style/choices 等提交语义。
6. 未出现在命令中的参数照常写入 params.json；dry-run 输出一条信息提示可经 ctx.params 读取。
   显式占位符缺值仍报错；scheduler 参数不作为未消费参数提示。
7. 已注册 project、历史 job 和运行任务保留原身份，不自动合并或删除。
   保留必要的旧 command JSON 别名读取，不为旧 UI 字段保留语义。

## 二、target：project 给默认，提交时覆盖

唯一提交优先级：**显式 `-t/--target` > job.target > project.target**。

- `project.target` 继续表示代码同步源，同时作为默认执行 target；不新增 source/default 字段。
- 日常 job 省略 target。临时换预约用 `runq submit job.yaml -t tsubame-ar9094`；
  要把目标记在实验文件里才写 job.target。
- `RUNQ_TARGET`、`.active-target` 和全局 default_target 不参与已注册 project 的提交选址。
  查询命令暂保留现有 target 过滤行为；`target use` 的 help 要明确它不覆盖实验提交目标。
- 缺少 project.target 的新 project 在注册时明确报错，让使用者补上；不静默选另一台机器。
- 覆盖执行 target 不修改 project 的同步源，也不在 `--project-file` 注册时把 -t 写成 source。
- 最终 target 必须是 source 或 project.targets 中的目标，且有对应配置。
  target 的合法性与配置完整性在预览、提交两条路径都检查。
- remote CLI forward 是固定的目标边界：先解析同一优先级，再校验结果等于 forward 所属 target；
  不允许 guard 静默改写 job.target 或 project 默认目标。跨边界请求明确拒绝。
- dry-run、JSON、提交记录与 dashboard 展示最终 target 及来源（cli/job/project）。

## 三、统一提交规划

先解析 target，再构造每次提交独立的生效配置，再预检、渲染、生成计划。
预检、预览、执行和提交快照使用同一套配置解析逻辑；不改写共享 project 对象。

- command：job > project，两者都空时报错。允许 project 无 command、由每个 job 提供。
- 修正 `internal/project/registry.go` 的无 command 文件回退判断；注册、JSON 缓存、YAML 重载都支持该模式。
- preflight：字段缺省继承；显式 false 关闭；`hf: []` 清空；`extra_run: ""` 清空。
  使用能区分未提供与显式空值的覆盖类型，并验证 CLI YAML → API JSON 后语义不丢失。
- 当前预检 `renderSampleCommand` 也读取 project command，必须同步使用合并后的 command。
- 命令参数占位符继续支持 `{{x}}`、`{{args}}`；`{{param.*}}` 属于调度器模板及相应预检配置，
  不把它描述成当前命令渲染器已经支持的语法。
- Plan 增加排序去重的 `UnconsumedParams`，文本和 JSON 都展示；这是一条信息，不是失败或警告。
- 保存实际 command 模板、target 与合并配置快照；task 保留各自渲染后的执行命令。

提交后端保留一条规划主路径：`submitplan.Build` 加目标相关的脚本渲染。
CLI `--dry` 统一走服务端 preview，本地/runqd/HPC 均能返回公共计划，HPC 额外返回 run.sh/submit line。
移除向导专用 `POST /jobs/plan`、`PlanJob` 和独立 ResolveNote 接口；note 解析留在公共规划内部。
清除 CLI 本地 DryRun fallback 及对应无剩余调用方的适配层，避免两套校验和 target 规则。

preview 不提交、不同步或创建远端任务目录。跨 target 的代码同步仍在实际提交时发生；
preview 应报告尚未同步这一事实，不能把旧目标代码的预检结果当成同步后代码已验证。
`--project-file` 目前先 upsert 再 preview：help 明确它会写 project 配置，不能称整条命令完全无副作用。

## 四、web：保留观察与必要操作

| 处理 | 功能 | 实施范围 |
|---|---|---|
| 删除 | 提交向导、从旧 job 打开提交表单 | 删除 submit 页面、路由、导航、LivePlan、参数生成器和表单专用状态 |
| 删除 | project 新建/编辑、target 编辑、全局运行配置编辑 | 配置统一由文件/CLI 管理；清除编辑器与表单专用类型 |
| 删除 | Webhook 配置表单 | 当前仅存 localStorage，测试明确未支持；删除这个未闭环入口 |
| 删除 | 上传任意日志的通用 LogViewer | 删除页面、log-sessions API、临时存储和 cleanupLoop |
| 删除 | web pause/resume | rc1 已删 CLI 入口；清理 API、Backend/Proxy 方法和无调用方的暂停逻辑 |
| 保留 | Overview、project jobs、job/task 详情 | 状态、命令、target、参数差异、W&B 跳转 |
| 保留 | 任务日志、服务日志、现有曲线与结果表 | tail/grep/分页/stream 按现有消费者保留，不扩展通用分析 |
| 保留 | 停止 job/task、重试 task、归档/恢复归档 | 这些是研究员看页面时有用的操作，保留确认与错误反馈 |
| 保留 | job 数据目录树与路径复制 | 继续按 job.target 读取，保留 fs/list；不扩展成通用远程文件管理器 |
| 精简 | 设置与 target 展示 | 设置只保留外观/语言等显示偏好；target 状态沿用 Overview，删除独立配置编辑页 |

### 本地任务确认与归档展示（2026-10-12 补充）

用户允许在 web 状态管理复杂时进一步收束为本地任务确认视图。
这里的“本地 task”指本机 controller 数据库中已记录的任务，包含远端 target 的任务，
不是只显示 local target，也不靠扫描远端目录重新发现 project/job/task。

- Overview、列表和 job/task 状态详情读取本地持久化快照；后台 backend 负责更新证据。
  展示最后观测时间与 stale/unavailable，断网时保留已有记录，不把未确认状态当成最新事实。
  用户主动查看远端日志或目录仍可按需访问，控制操作继续走所属 target。
- 默认入口聚焦未归档的 job/task。project 只作筛选与分组；若维护完整 project 导航过于复杂，
  允许用任务列表代替，不要求为所有已注册或历史 project 保留常驻卡片与前端状态。
- archived project/job 默认不进主列表、侧栏和状态汇总。历史入口按需加载、分页，
  不在进入 Overview 时预取全部归档记录，不为已归档历史启动周期状态探测。
- 归档继续表示隐藏而非删除，保留按需历史查询、详情与恢复归档；不自动删除数据库记录或目录。
  归档/恢复后统一失效相关列表缓存，避免主列表与侧栏出现两套可见性。

主题、语言、密度和现有个性化选项保留；显示偏好与项目分组改为浏览器 localStorage，
删除服务端 GET/PUT /ui 及跨机器偏好同步。原有 ui.json 不作迁移、不删除用户文件。
删除提交页专用参数分组；显示偏好不进入提交配置。

job/task 的能力判断、刷新节奏必须取所属 target，修正当前从 config.currentTarget（全局默认）读取的行为。
历史 target generation 的任务仍按其保存的所属信息处理，不能由全局默认冒充。

## 五、后端一起删除和保留

删除已确认仅服务已删入口的接口：

- `POST /targets/{name}/fs/parse-script`、`GET /targets/{name}/python-envs`、`GET /targets/{name}/fs/glob`。
- `GET /targets/{name}/fs/read`：当前前端调用方都在 submit/project-edit；随它们删除。
- `GET /targets/presets`、target 表单 PUT/DELETE/check、全局配置 PUT，及这些 HTTP 表单的 If-Match/错误适配层。
  保留 CLI 的本地 config 校验、starter preset、文件保存与运行时热加载；删除 HTTP 层不等于删除共享实现。
- GET/PUT /ui、全部 log-sessions、jobs pause/resume、向导 jobs/plan。
- 为这些功能专设且已无消费者的 DTO、能力位、后台循环和测试；清理 remote_guard 对应分支。

保留：CLI 所需的 project 注册/更新 API、submit/preview、状态/结果/日志读取、kill/retry/clean、
归档、target connect/disconnect/refresh、健康检查、任务目录读取，以及底层 rfs/SSH 复用。
`config_edit.go` 同时含 connect/disconnect/refresh，不能整文件盲删。

保留 target 热加载、generation 归属、取消意图持久化、重启恢复、SSH 重连、remote forward guard。
它们服务运行中任务的正确性。删除功能后的接口自然变小；不为“更小”另造一套插件/能力框架。
历史 paused 记录保持可读；移除暂停入口不能在升级时自动恢复任务派发。

## 六、后续独立工作：收束后台探测（v0.6 正式版前）

当前 `dashboard.Server.reconcileLoop` 与 `SSHBackend.sensorLoop` 都发起 scheduler probe，
两处均有用户活跃度相关分支；sensorLoop 休眠时仍有 marker/heartbeat 路径，不能说后台完全停止。

下一项工作把探测归 backend 的单一循环所有：有待执行/运行/待取消任务时按有界频率工作，
无任务时休眠；开关页面不决定状态正确性。删除 dashboard 的探测 ticker 与重复活动门控。
手动 refresh 向同一循环发请求，保留频率限制。验证关页面后完成、硬退出、取消重放和槽位释放。
这项涉及生命周期，单独实现、单独验证；本 rc2 不夹带 accounting 真相源或完成契约改造。
连接可靠性、时间字段分离与 W&B 最小目录契约纳入本轮后续 rc，按 `task-infra-reliability-v0.6.md` 验收。

## 七、文档、验证与交付

同步 `docs/configuration.md`、`docs/hpc.md`、`docs/cli.md`、README 和内置 runq skill：

- project 是代码库，实验差异进 job；默认 ctx.params，命令行可选。
- project 已存在就用 --project-file upsert，不另建名字。
- 展示 project 默认 target + `runq submit job.yaml -t ...` 的常用路径。
- W&B init 后 link；SSH_AUTH_SOCK 取服务启动 shell，换环境后 restart。
- CHANGELOG 写 choices 强制约束、旧 UI 字段删除、target 顺序、web/API 收束。
- release 的 SDK 版本校验要求 pyproject.toml 与 runq/__init__.py 同步为 0.6.0rc2。

验收：

1. 一个 project 下 train/smoke/换预约三个 job 预览成功；无 project command 的文件重载生效。
2. `-t > job > project` 冲突矩阵通过；RUNQ_TARGET/active/global default 不干扰提交；
   preview 与 submit 都拒绝未允许的 target；remote forward 校验最终目标，不能绕过或静默替换。
3. 未消费参数进入 params.json，dry-run 显示提示；缺少显式占位符值报错。
4. choices 无 strict、strict:false、strict:true 都强制约束；取消旧建议语义。
5. preflight 覆盖 command、false、空列表、空字符串在 YAML/JSON 往返后生效；保存快照可追溯。
6. 本地/runqd/HPC 预览走公共规划路径；相同输入的计划与提交使用相同配置。
7. 删除路由和后台任务确实消失；保留页面可导航，无已删 API 请求或死链接；CLI 注册/提交/读取/控制回归通过。
8. Go 全量测试、`go test ./internal/cli -run TestSkill`、SDK pytest、前端 build/vitest/oxlint 通过。
   新测试验证行为边界，删除实现后应失败；删功能的测试替换成接口消失与保留路径的回归检查。
9. 页面检查 Overview、project、job、task、偏好设置；包括 job target 与全局默认不同的场景。
10. 大量 archived project/job 不撑大默认页面，也不被默认全量加载或轮询；历史查询与恢复仍可用。
    远端断开时状态页面能读取本地已记录的远端 task，并明确观测时间/新鲜度；
    归档/恢复后主列表、分组与历史视图一致，不删除任务记录或文件。

按“配置模型 → 公共提交规划 → web 与专用 API 删除”拆提交，每步能验证。
实现时新建 codex/ 前缀分支；不 push、不打 tag。无需为本轮设计修订创建分支或提交。
本轮不操作真实运行状态、不改研究仓库、不 SSH 集群、不真实 submit。
后续本机安装使用先 rm 再 cp，随后 restart；安装与运行验证单列汇报。

最终汇报列出变更提交、验收证据、删掉的 API/后台任务、保留的共享依赖，以及未完成项。


## rc2 实施与验收记录（2026-10-12）

已完成配置模型、公共提交规划和 web／专用 API 收束。源码位于 `codex/rc2-config-consolidation`。
配置模型与服务路径分别提交；web、SDK 版本和文档作为收束提交。没有 push 或打 tag。

- 配置：job command/target/preflight，project command 旧名读取，choices 强制约束，旧参数表单字段忽略。
  空值覆盖经过 YAML → JSON → 计划验证；未消费参数仍入 params.json，并在 preview 作信息提示。
- 规划：所有 target 使用服务端 preview；与实际提交共享 Build、目标许可和配置快照。
  显式 target 与查询过滤分离；forward 校验最终目标，并在后端最终解析时再次约束，避免二次解析越界。
- 收束：删除提交／project／target／全局运行配置表单、Webhook 和通用日志上传页面；
  删除 /ui、log-sessions（含 cleanupLoop）、jobs/plan、pause/resume 和表单专用 target/fs API。
  保留 CLI 注册／配置文件校验、SSH 与 generation 归属、取消／重启恢复、任务日志与目录读取。
  scheduler 的历史 paused 派发门禁继续用于恢复，升级不恢复这些任务。
- 页面：状态从 controller 数据库读取，远端失联或 target 已移除时仍可确认本地记录；
  所属 generation 能力随详情返回，展示 target／来源、观测时间与未确认提示。
  archived project/job 默认隐藏，打开历史入口才加载，每页 25 条；可查询与恢复，记录／文件不删除。
  显示偏好和分组使用 localStorage，已有 ui.json 不删除、不迁移。

验收证据：

- `go test ./...` 全量通过，含临时本地 HTTP／Unix socket 测试；内置 skill 校验通过。
- SDK pytest：91 passed；版本文件和 uv.lock 均为 0.6.0rc2。
- 前端：build、15 个 Vitest 文件／84 项测试、oxlint 通过。
- dashboard 标签构建成功；独立二进制报告 `v0.6.0rc2`。
- 独立本地 fixture 检查 Overview、project、job、task、Settings；61 个归档 project 不进入默认主列表，
  历史第一页 25 条，第三页 11 条。测试中的默认 target 与 job 所属 target 不同。
  [job 详情截图](../verification/rc2/job-detail.png)显示所属 target、来源和观测状态。

验证使用 fake/mock、临时本地文件与数据库；未 SSH 集群、未真实 submit、未安装或重启运行服务。
连接可靠性与 backend 单一探测循环、时间语义、W&B 目录契约仍按基础设施任务书依序实施。
