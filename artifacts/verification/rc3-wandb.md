# RC3 W&B 目录契约（2026-10-12）

## 行为

- `RUNQ_WANDB_CONFIG_FILE` 由计划中是否配置 W&B 决定，不再以 controller 的 os.Stat 判断远端文件。配置仍由 WriteFS 写在目标任务目录。
- `workspace.TaskEnv` 统一 preview、持久化和 wrapper 的路径。默认输出 `<task_dir>/outputs`、checkpoint `<task_dir>/checkpoints`、W&B 根 `<task_dir>`；显式 project/job env 覆盖保留，相对路径基于项目 working_dir。
- 提交前通过实际目标 FS 创建并检查根目录；计算节点 wrapper 再检查一次，失败留下终态和错误日志，避免 SDK 静默退回临时目录。项目代码仍是 cwd。
- SDK 增加 `ctx.output_dir`、`ctx.wandb_dir`，`wandb_like_cfg.dir` 使用已解析的 W&B 根，不额外拼 `wandb`。任务详情显示三种根目录；Data tab 浏览任务目录中的实际文件。
- `ctx.link_wandb(run)` 只在全局主进程记录非空真实 run.url，允许显式 global_rank；缺 URL 的 offline/disabled run 不改旧链接。Web 仅打开已记录的 URL，不猜 custom-id 地址。Core SDK 无需导入/安装 W&B。
- 同 task 重试保留输出、checkpoint、offline 文件，默认 id 不变；online resume 与 offline bundle/sync 分开处理。共享 cache、artifact staging/download 是 W&B 独立配置；runq 不增加上传队列或自动清理。
- Python SDK、pyproject、uv.lock 协调为 0.6.0rc3；Go 安装构建盖版本 v0.6.0rc3。

## 验证分层

- 完整 Go 回归通过，目录覆盖/拒绝无效根、preview 无写入、Prepare 与 preview 覆盖一致、retry 保留文件等新增集成测试通过。
- Python 94 tests passed：配置、目录、显式覆盖、全局 rank（LOCAL_RANK=0 但 RANK=8）、真实 URL mock、缺 URL 不写、无 wandb 导入等。
- Web 16 文件 / 85 tests passed，类型检查和 production dashboard 构建通过。
- **真实 offline**：隔离环境固定 `wandb==0.23.1`。实际 wandb-core 运行两个任务、同 task 再运行、显式根覆盖和全局 rank 8 disabled；run 文件在根下的 wandb 子目录，两 task 隔离，旧 offline bundle 和 checkpoint 保留，项目 cwd 没有 wandb 子目录。实际 Artifact.add_file/log_artifact 使用 task 的 WANDB_DATA_DIR staging，并保留单独共享 WANDB_CACHE_DIR。
- 可复现脚本 `artifacts/verification/rc3_wandb_offline.py`；私有输出 `.cache/verification/rc3-wandb-offline-native/report.json`（含五个运行结果）。首次 sandbox 尝试无法启动 wandb-core 本机 IPC；允许本机 IPC 后通过。
- **Mock**：online run 的实际 URL、自定义 id 与主进程记录规则，未连接真实 W&B online 服务。
- **尚未覆盖**：真实 online resume、online URL 服务链路、artifact download 与官方 sync 上传。本轮未提交真实集群训练任务，也未上传任何 W&B 实验。

核对固定版本原生公开实现：[init](https://github.com/wandb/wandb/blob/v0.23.1/wandb/sdk/wandb_init.py)、[settings](https://github.com/wandb/wandb/blob/v0.23.1/wandb/sdk/wandb_settings.py)、[env](https://github.com/wandb/wandb/blob/v0.23.1/wandb/env.py)。默认 root 下 wandb 子目录、WANDB_DIR 和独立 artifact 设置与实际 offline 结果一致。
