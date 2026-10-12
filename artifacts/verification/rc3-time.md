# RC3 时间修正（2026-10-12）

- 新增 tasks.timing_json，按 retry_count/attempt 保存时间、来源、首次观察时间。原有 started_at 数字保留，不推定为脚本启动；新提交以 dispatch_started_at 单独记录提交基准。
- Wrapper 在 env_setup 前记录入口并安装退出 trap；终态原子 JSON 同时保留入口、脚本开始和结束。初始化失败不伪造 execution 时间，硬退出没有终态证据则不补造结束。Launcher 为重试把 attempt 写入 run.sh；旧 attempt 的 JSON 不参与本 attempt 的判定和计时。
- elapsed_seconds 只代表脚本运行。CLI/Web/JSON 分别展示排队、调度器运行、脚本运行；详情包含总历时、UTC 原始时间和 attempt 历史。有效零值显示 0，未知显示 —，估算显示 ≈，负区间保留证据且不返回时长。
- 旧 wrapper 的明确 started_at/finished_at 只用于 attempt 0 的估算。未使用 log/directory 的 mtime 或 ctime 推定执行起点，因此复制文件时间不改变时长。
- 时间证据可在状态未变或硬终态时补齐，不重新打开终态。手动和自动重试保留旧 attempt 时间，当前 attempt 不继承旧执行起点。
- 原生 parser 支持 submitted_at/started_at/finished_at=epoch 或带时区 RFC3339。Slurm preset 扩展 squeue/sacct 时间输出；pending 的预计 Start、非终态 End 不作为实测时间。已有用户模板不自动改写，未提供原生时间的 target 显示未知。

## 内部计时审计

- scheduler.Queue.MarkRunning 的 StartedAt 仍是内部 dispatch/lease 时间，展示不读取它；恢复时优先从 dispatch_started_at 还原，旧记录仍使用原始 StartedAt。
- Timeout 传递给 runqd/调度器的配置保持原语义。
- Heartbeat 仍以文件最近活动判断静默；无文件活动时增加明确的 wrapper/execution/submit/dispatch 基准。它不推定 execution_finished_at。
- orphan/unknown 恢复仍由已有 external id、attempt、generation 和状态证据矩阵决定，不根据新的展示时长判断终态。

## 验证

完整 Go 回归通过；Web 16 文件、85 测试通过，类型检查和 dashboard 构建通过。
新增测试覆盖长排队短运行、缓慢 setup、setup 失败、TERM、硬退出、只见终态、迟到时间、零/负时长、重试历史、恢复 dispatch 基准、原生 parser 与远端时区。wrapper 测试使用实际本地 shell；runqd 与 HPC 共用该 wrapper，协议差异继续由已有 runqd/HPC 回归覆盖。未执行真实集群提交。

原生语义核对：[squeue](https://slurm.schedmd.com/squeue.html)、[sacct](https://slurm.schedmd.com/sacct.html)。SLURM_TIME_FORMAT 支持 strftime；%S 可能是 pending 的预计启动，必须结合状态过滤。
