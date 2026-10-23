<!-- Generated; do not edit by hand. Source: .project/events.jsonl. Live view: npx slotledger web -->
# Project status

## Live decisions

| Category | Slot | Current value | Status | Source | Updated | Notes |
|---|---|---|---|---|---|---|
| Goal | goal/analysis-scope | 分析/拉表功能在消融研究结束前冻结；例外是 views：进度汇报加上论文表/图（aggregate、pivot、highlight、baseline、6 种图模板、LaTeX/Markdown/CSV、图片和动画导出） | proposed | AI | 2026-10-23 08:14 |  |
| Goal | goal/job-append-into | 需要 runq submit --into &lt;job&gt; 往已有 job 里补格子：一个实验 = 一个 job；已成功、在跑、排队的格子跳过，失败的默认也跳过，--rerun-failed 才重跑；逻辑合并（merge）不做 | decided | user | 2026-10-23 08:35 |  |
| Goal | goal/product-positioning | runq 给“让 AI agent 跑消融的研究员”用：省 token、好用优先，web 只负责看，配置以文件为准，不再为了 CLI/web 对称扩大 scope；同一件事只保留一种做法 | proposed | AI | 2026-10-23 08:14 |  |
| Approach | approach/chart-stack | Chart.js 画的任务指标图、旧结果图和手写 SVG 的 view 论文图，系列颜色统一用 chartGeom 的 Okabe–Ito 顺序（深色主题下黑色换成浅灰，超出 8 条用虚线），不再借用项目色；所有折线 tension 0 | decided | user | 2026-10-23 08:35 |  |
| Approach | approach/consumer-result-writeback | consumer 在 rc6 保持不变；rc8 换成基于文件的结果写回：&lt;task_dir&gt;/results.&lt;producer&gt;.jsonl，不需要网络鉴权；研究员接受失去“scorer 还活着”这个信号 | proposed | AI | 2026-10-23 08:35 |  |
| Approach | approach/docs-source | 设计、计划、审计、验证、交接这类过程文档不再写成文件，统一由 slotledger 管理；docs/ 可以保留，但内容必须由 slotledger 自动生成，不作为参考依据；README 这类介绍文档保留 | decided | user | 2026-10-23 08:35 |  |
| Approach | approach/draft-submit-two-stage | 两阶段提交：runq draft job.yaml 创建服务端草稿 dr…（输出承担原 --dry 预览），runq submit dr… 冻结成新 jb…（幂等，带 rev 校验）；submit 不再接受 YAML，--dry 删除；草稿每个 project 只保留最新 5 个 open 的，不设过期时间；人用 $EDITOR、agent 用 draft edit -f，和 dashboard 走同一个 PUT 并按 rev 校验 | proposed | AI | 2026-10-23 08:35 |  |
| Approach | approach/forward-socket-publish | remote CLI 转发先绑定新 generation socket，再用 SFTP posix-rename 原子发布到稳定路径；仍有应用层响应或属于其他 owner 的端点不替换，owner 文件用硬链接独占创建且重连时不改写 | proposed | AI | 2026-10-23 07:55 |  |
| Approach | approach/honest-display | 展示只呈现记录过的事实，画图准确比好看重要：所有曲线用直线连接（不用样条），curves 不插值，步数不一致就画各 task 的原始线 | decided | user | 2026-10-23 08:35 |  |
| Approach | approach/job-results-tab | job 页 Results 标签：有 view 时按这个 job 渲染各 view；没有 view 时退回旧 JobResultsCard。旧卡片不再按指标名猜最优方向，由读者点表头声明（未设定 → 越大越好 → 越小越好），没声明的列不标最好值、不给 Δ 着色 | decided | user | 2026-10-23 08:35 |  |
| Approach | approach/job-yaml-tasks-rows | job.yaml 二选一：sweep: 生成，或 tasks: 显式列出命名 run（run 必填唯一，needs_code 只是注记，二者都不进 params）；plan: {baseline, footer}；runq export jb… 输出完整展开的 tasks: 行（不还原 grid），可直接作为 runq draft 的输入，往返一致 | proposed | AI | 2026-10-23 08:35 |  |
| Approach | approach/legacy-config-keys | 旧配置键：忽略后不影响执行的，警告一行后继续；忽略后会改变实际执行的，报错并说明替代写法；不做迁移工程 | proposed | AI | 2026-10-23 08:35 |  |
| Approach | approach/number-format | 数字在所有地方都以完整十进制显示，只有显式要求时（sci0..3、key_formats）才用科学计数 | proposed | AI | 2026-10-23 08:14 |  |
| Approach | approach/param-passing | 参数默认按工程规范走命令行（argparse 等，job 里用 {{x}}/{{args}} 传）；ctx.params 是一次性消融的捷径。判别方法：要随研究发布、给别人用的代码走 CLI；研究员说“临时测试一下”的可以用 ctx.params；判断不了时优先 flag 或问研究员 | decided | user | 2026-10-23 08:35 |  |
| Approach | approach/principle-capabilities-probed | （设计哲学 C2）能力是探测出来的事实，不是模式的推论：每个主体自述能力（Backend.Capabilities()），每个检查自己探测前提，探测不到就如实 skip；不写 if mode == hpc，不写站点特定代码 | proposed | AI | 2026-10-23 08:35 |  |
| Approach | approach/principle-cli-output | （设计哲学 X1）CLI 输出为两种读者优化、永不混合：人读的部分一行一事实、对齐、无废话；机器读的走 --json 全量结构；报错信息本身就是文档（例如 pause: not supported in hpc mode，非零退出） | proposed | AI | 2026-10-23 08:35 |  |
| Approach | approach/principle-density | （设计哲学 U4）高级 = 信息密度，不是装饰：参数值全 monospace、表格可排序、图表克制并带数值 legend、一屏有效数字多；dashboard 是给人第一千次打开还不烦的 | proposed | AI | 2026-10-23 08:35 |  |
| Approach | approach/principle-one-truth-two-personas | （设计哲学 C3）数据结构只有一份（capabilities、preflight 报告、占位符词表、展开语义），CLI 和 GUI 按各自的用户翻译：风格可以分叉，数据不分叉；前后端不各维护一份逻辑 | proposed | AI | 2026-10-23 08:35 |  |
| Approach | approach/principle-preview-is-truth | （设计哲学 U1）预览即真相：“将要发生的事”用已经算出来的结果展示，预览与提交同源同构（例如 draft 的预览就是真实展开，任务数公式 kl(3) × seed(2) = 6） | proposed | AI | 2026-10-23 08:35 |  |
| Approach | approach/principle-shape-by-capability | （设计哲学 U2）界面形状由能力决定：不支持的功能整块不渲染、不置灰；preflight 的 skip 项灰色中性，不用黄色警告 | proposed | AI | 2026-10-23 08:35 |  |
| Approach | approach/principle-side-effects-secrets | （设计哲学 C5）副作用分级、secret 不落盘：零副作用的操作（预览、check）可以随便调；有副作用的操作必须显式、可预期；消耗集群 points 的验证永不自动；secret 走进程注入而不是模板插值，脱敏只在展示层 | proposed | AI | 2026-10-23 08:35 |  |
| Approach | approach/principle-user-permissions | （设计哲学 C4）一切 user 权限、方言归用户：无 root、无常驻特权进程，compute node 只写文件；SLURM/PBS/SGE 方言全在用户模板里，runq 只做插值和一个正则；需要管理员配合的功能默认就是设计错了 | proposed | AI | 2026-10-23 08:35 |  |
| Approach | approach/principle-zero-learning-default | （设计哲学 U5）默认路径零学习，深度藏在一层折叠后：每个界面有一条不读文档就能走通的默认路径，高级能力放在次级控件里（例如指标选择器、参数面板默认折叠） | proposed | AI | 2026-10-23 08:35 |  |
| Approach | approach/project-colors | 项目色只用状态色以外的色相：蓝、紫、粉、棕、品红、靛（6 色，超过后循环），不用绿/红/琥珀/青/灰；job 列表左边的项目色条只在混合多个项目的列表（Overview）里显示，项目自己的页面不显示 | decided | user | 2026-10-23 08:35 |  |
| Approach | approach/project-explicit-upsert | runq project [file] 显式 upsert；runq 不再隐式读取或写回 project.yaml，数据库里的注册配置是唯一依据；起草稿时磁盘文件和注册时的 sha 不同就给出警告 | proposed | AI | 2026-10-23 08:35 |  |
| Approach | approach/ssh-diagnostics | forward 状态附带 phase/reason/hint/next_retry_at/transport/debug_command/服务的 SSH_AUTH_SOCK，在 runq doctor 和 Targets 页展示；SSH 可达性（含文件浏览成功）与 CLI 隧道分两栏观测；认证失败 30s 重试，网络失败 2s→10s 退避 | proposed | AI | 2026-10-23 07:56 |  |
| Approach | approach/ssh-exit-255-unknown | OpenSSH 会话退出码 255 一律视为传输错误/结果未知（ErrExecInterrupted），连接中断时已提交的命令永不重放 | proposed | AI | 2026-10-23 07:55 |  |
| Approach | approach/ssh-transport | 用户 target 的连接、命令和 SFTP 全部交给系统 OpenSSH：每个 transport 用 /tmp 下私有 ControlMaster socket，ControlPersist=no，BatchMode=yes，复用客户端 -F none 且 ProxyCommand=false 防止回落；原生 Go ssh 只留给注入/测试传输 | proposed | AI | 2026-10-23 07:55 |  |
| Approach | approach/status-color-grammar | 状态色全站只有一套，收口在 statusGrammar.ts；红色只留给真故障，view 不能自己配状态色，非状态含义的配色要和状态语义色分开 | proposed | AI | 2026-10-23 08:14 |  |
| Approach | approach/target-connect-semantics | POST /targets/{name}/connect（Dashboard 的 Connect 按钮）对所有 SSH target 先通过 lane 执行 true 做 10s SSH 检查；只有 remote_cli: true 时才重建 CLI 转发 | proposed | AI | 2026-10-23 07:55 |  |
| Approach | approach/tsubame-group-param | TSUBAME 预设的 submit_template 改为 qsub -g {{param.group}}，计费组作为 scheduler 作用域参数写在 job.yaml 的 fixed_params.group，不再依赖登录 shell 里的 $TSUBAME_GROUP | proposed | AI | 2026-10-23 07:55 |  |
| Approach | approach/view-delta-coloring | Δ 按列声明的方向判断变好/变差：变好用 Okabe–Ito 蓝、变差用橙（深色主题提亮），加 ▲/▼ 作为第二通道；不再用状态色 success 绿/error 红；没声明方向就不着色。view 表格和旧结果卡共用 .delta-better/.delta-worse | decided | user | 2026-10-23 08:35 |  |
| Approach | approach/views-paper-figures | view 图表按论文图画：深浅主题下都是白纸，3.3in/6.9in 单双栏，Okabe–Ito 配色加 marker/虚线，不写标题，导出 SVG/300dpi PNG/PDF，除 curves 外还能导出 GIF/WebM | proposed | AI | 2026-10-23 08:14 |  |
| Environment | env/client-openssh | runq 服务所在机器必须有系统 OpenSSH 可执行文件（PATH 中的 ssh），版本 ≥ 7.6（RemoteCommand 选项的最低版本）；不再传 ForkAfterAuthentication，Ubuntu 20.04（8.2）、RHEL 8（8.0）可用 | decided | user | 2026-10-23 08:04 |  |
| Environment | env/remote-sshd-capabilities | 目标集群 sshd 需允许 Unix socket 远程转发（streamlocal）并提供带 posix-rename 扩展的 SFTP 子系统 | proposed | AI | 2026-10-23 07:55 |  |
| Assumption | assum/agent-uses-runq-for-small-runs | agent 有时会把很小的实验（比如在 iqrsh 免费节点上训练一个小模型）也自动交给 runq，即使研究员没这个意思；目前没有好的解决办法 | decided | user | 2026-10-23 08:35 |  |
| Assumption | assum/noninteractive-auth | 用户的集群可以用 ssh-agent/证书/无口令 key 做非交互认证；密码或 MFA 只在 runq target connect 的终端里交互完成，后台连接一律 BatchMode=yes | proposed | AI | 2026-10-23 07:55 |  |

## Open questions

(none)

## Tasks

| Task | Status | Verification |
|---|---|---|
| SSH 传输切换到系统 OpenSSH（含 ProxyJump、私有 ControlMaster、退出码 255 处理） | verified | run: `test -z "$(gofmt -l internal/rfs)" && ! grep -rn '"ForkAfterAuthentication=' internal/rfs/*.go && go vet ./internal/rfs/ && go test ./internal/rfs/ ./internal/app/ ./internal/cli/ -count=1` passed (darwin 27.0.0 arm64, node v22.23.0, 2026-10-23 08:05) |
| remote CLI 转发：按 generation 绑定并原子发布 socket，owner 记录防截断/防覆盖 | verified | run: `go test ./internal/rfs/ -run 'Forward' -count=1` passed (darwin 27.0.0 arm64, node v22.23.0, 2026-10-23 07:56) |
| Connect 对所有 SSH target 做连通检查，remote_cli 才重建转发 | verified | run: `go test ./internal/app/ ./internal/dashboard/ -count=1` passed (darwin 27.0.0 arm64, node v22.23.0, 2026-10-23 07:56) |
| TSUBAME 预设与示例改用 {{param.group}} | verified | run: `go test ./internal/config/ -count=1` passed (darwin 27.0.0 arm64, node v22.23.0, 2026-10-23 07:56) |
| SSH 诊断：forward 失败阶段/原因/重试时间/调试命令，显示在 doctor 与 Targets 页 | verified | run: `go test ./internal/cli/ -count=1 && cd web/dashboard && PATH="$HOME/.nvm/versions/node/v22.23.0/bin:$PATH" ./node_modules/.bin/vitest run src/views/targetsView.spec.ts && PATH="$HOME/.nvm/versions/node/v22.23.0/bin:$PATH" ./node_modules/.bin/vue-tsc --noEmit` passed (darwin 27.0.0 arm64, node v22.23.0, 2026-10-23 07:57) |
| 在真实集群（TSUBAME，含 ProxyJump 和睡眠恢复）上端到端验证系统 OpenSSH 传输 | todo | not verified |
| 旧界面数字被截断：LatestMetrics 的 4 位有效数字、JobResultsCard 的最多 4 位小数 | verified | run: `cd web/dashboard && export PATH="$HOME/.nvm/versions/node/v22.23.0/bin:$PATH" && ./node_modules/.bin/vitest run src/views/job/resultsView.spec.ts src/utils/metricFacets.spec.ts` passed (darwin 27.0.0 arm64, node v22.23.0, 2026-10-23 08:41) |
| rc6：草稿/提交两阶段、tasks: 计划表、export 往返、CLI 收束（后端 + CLI + SDK + 文档） | todo | not verified |
| submit --into：往已有 job 追加格子（含 --rerun-failed、批次记录）以及三个小项 | todo | not verified |
| rc8：consumer 换成基于文件的结果写回（results.&lt;producer&gt;.jsonl） | todo | not verified |
| docs/ 改为由 slotledger 自动生成，替换现在手写的用户手册 | todo | not verified |
| 让 agent 分辨什么时候不该用 runq（如 iqrsh 免费节点上训练一个小模型） | todo | not verified |
| 消融研究结束后的一轮：分析操作审计、checkpoint 清理、完成契约、推送代替轮询 | todo | not verified |
| 宣传文案：README 首屏改写和异议应对卡（面向课题组研究者） | todo | not verified |
| 画图准确性与配色：直线连接、统一 Okabe–Ito 系列色、Δ 改蓝/橙加 ▲▼、去掉猜方向、项目色避开状态色 | verified | run: `cd web/dashboard && export PATH="$HOME/.nvm/versions/node/v22.23.0/bin:$PATH" && ./node_modules/.bin/vue-tsc --noEmit && ./node_modules/.bin/vitest run src/utils/projectColor.spec.ts src/views/job/resultsView.spec.ts && ! grep -rn "tension: 0\.[1-9]\\|v-theme-success))\\|guessDir" src --include=*.vue --include=*.ts \| grep -v spec \| grep -i "tension\\|guessDir\\|delta"` passed (darwin 27.0.0 arm64, node v22.23.0, 2026-10-23 08:41) |
| skill 写清参数传递：默认 argparse + {{args}}，ctx.params 是一次性消融的捷径 | verified | run: `go test ./internal/cli -run TestDocs -count=1 && grep -q "a shortcut for one-off ablations" internal/skill/runq/SKILL.md` passed (darwin 27.0.0 arm64, node v22.23.0, 2026-10-23 08:41) |
| 删除 artifacts 下所有过程文档（设计、审计、验证、宣传 md），内容先迁入 slotledger | verified | run: `test "$(find artifacts -name '*.md' \| wc -l \| tr -d ' ')" = 0 && test -d artifacts/audits/runq-analysis-ops/scripts` passed (darwin 27.0.0 arm64, node v22.23.0, 2026-10-23 08:41) |

## Change history

- env/client-openssh: v1 "runq 服务所在机器必须有系统 OpenSSH 可执行文件（PATH 中的 ssh），且版本需支持 ForkAfterAuthentication 选项（OpenSSH ≥ 8.7）；当前开发机为 OpenSSH_10.3p1" → v2 "runq 服务所在机器必须有系统 OpenSSH 可执行文件（PATH 中的 ssh），版本 ≥ 7.6（RemoteCommand 选项的最低版本）；不再传 ForkAfterAuthentication，Ubuntu 20.04（8.2）、RHEL 8（8.0）可用"
- approach/honest-display: v1 "展示只呈现记录过的事实：宁可诚实地少说；图表不制造没记录过的点（curves 不插值，步数不一致就画各 task 原始线）" → v2 "展示只呈现记录过的事实，画图准确比好看重要：所有曲线用直线连接（不用样条），curves 不插值，步数不一致就画各 task 的原始线"
- approach/view-delta-coloring: v1 "view 表格的 Δ 按列声明的 better 着色：变好用主题 success 绿，变差用主题 error 红，better 没声明就不着色；LaTeX 默认不着色" → v2 "Δ 按列声明的方向判断变好/变差：变好用 Okabe–Ito 蓝、变差用橙（深色主题提亮），加 ▲/▼ 作为第二通道；不再用状态色 success 绿/error 红；没声明方向就不着色。view 表格和旧结果卡共用 .delta-better/.delta-worse"
- approach/job-results-tab: v1 "job 页 Results 标签：项目里有 view 时，把每个 view 按这个 job 渲染；没有 view 时退回旧的 JobResultsCard（行标签模板、对比 base 的 Δ、按指标名正则猜方向来标最好值、Chart.js 折线图）" → v2 "job 页 Results 标签：有 view 时按这个 job 渲染各 view；没有 view 时退回旧 JobResultsCard。旧卡片不再按指标名猜最优方向，由读者点表头声明（未设定 → 越大越好 → 越小越好），没声明的列不标最好值、不给 Δ 着色"
- approach/chart-stack: v1 "图表有三套：Chart.js 画的任务指标图（MetricsChart，6 色 Tailwind，tension 0.3）和旧结果图（ResultsChart，借用项目身份色 --project-N），都跟随主题；手写 SVG 画的 view 论文图（Okabe–Ito）；手写 SVG 画的任务活动曲线" → v2 "Chart.js 画的任务指标图、旧结果图和手写 SVG 的 view 论文图，系列颜色统一用 chartGeom 的 Okabe–Ito 顺序（深色主题下黑色换成浅灰，超出 8 条用虚线），不再借用项目色；所有折线 tension 0"
