# RC2 连接可靠性与本机迁移交接

2026-10-12。本轮只完成连接阶段；时间修正、W&B 目录闭环仍按后续顺序推进。

## 已完成

- 持久 controller ID；controller + target 的短 socket 路径和独立 env。
- socket 父目录 0700、socket/env/归属记录 0600；103 字节路径上限。
- 仅清理归属本会话且明确拒绝连接的陈旧 socket；活动 listener 报冲突；OpenSSH 隐藏 errno 时用远端只读 python3 检查，仍只有 ECONNREFUSED 可清理；替换前等待旧 supervisor 退出；旧退出不删除新路径。
- TCP/SSH handshake、SFTP 初始化、文件操作、bind、health、keepalive 有截止时间。取消关闭实际 transport 并等待操作返回；会话槽等待可取消。
- SSH channel 缺失 deadline 会阻塞 Go HTTP 响应结束：为 forward 的 accepted connection 补上支持 deadline 的 pipe。
- `connect` 只有远端 health 核对 controller、target、版本后退出 0；安装/启动/就绪错误分阶段报告。
- 跨平台安装支持 `--remote-binary`；正式发行文件下载校验 SHA-256；唯一临时文件验证版本后原子替换，失败保留旧二进制。
- 三个 SSH 构造入口统一 `ssh -G`。ProxyJump/ProxyCommand/HostKeyAlias 明确拒绝。Agent 每次列出密钥/签名重新连接选定 socket。
- 删除 dashboard 独立探测 ticker 和页面活跃门控；每个 lane 的 sensor loop 持续观察未完成任务。已完成任务的 external ID 不再保持 sensor 活跃。手动 refresh 进入同一 loop，用请求周期号防止旧 pass 满足新请求。
- observation 文件操作继承 pass/request context。done-dir listing 移到 lifecycle 锁外；状态应用和 attempt 文件重置仍保留原有 fence。

## 验证

- 全套 `go test ./...` 通过。
- rfs/backend/remote/dashboard/app/cli 的 race 测试通过；最终文件上传与刷新改动再运行相关包 race 测试通过。
- Fake SSH/SFTP 覆盖 handshake 黑洞、SFTP 初始化挂起、文件请求取消、bind 期间关闭、同 HOME 双 target/双 controller、活动 socket 冲突、旧关闭不删新 socket、断线重连和 backoff 取消。
- 当前受限 TCP 链路大批量写入会返回 result too large；上传采用串行 SFTP 和底层小块写入，安装使用显式总时间预算；24.1 MiB Linux CLI 已在实际链路上传成功并验证版本/原子替换/health 就绪。
- 断线后的已接受 Exec 保留 outcome-unknown，不能作为自动重提条件。
- SQLite 迁移脚本在独立临时数据库验证了执行记录保持不变。
- 本机安装 `v0.6.0rc2-dev+connections`，Unix health 返回 200。实际集群 tsubame、tsubame-ar9055、tsubame-ar9094、tsubame-c0-evaluation 四个 target 的 connect 均退出 0，确认 controller/target/version；重复连接验证了 OpenSSH 隐藏 errno 的陈旧 socket 恢复。
- 未进行真实笔记本睡眠/切换网络实验；该部分以故障注入覆盖，不能将其写成已完成的实机测试。

## 数据库迁移

正常停止旧服务后，执行 `artifacts/migrations/rc2_connections.py`：SQLite backup API 包含 WAL，备份完整性检查通过。事务内只修改 projects/jobs 的 config_json；其余所有列及其余表逐行哈希完全一致。

最终停服时数据库有 30 个 project、62 个 job、169 个 task。迁移了 30 个项目的 `command_template` → `command`，62 个历史 job 根据 jobs.target 补充 config.target。原始任务命令、参数、环境、状态、external ID、retry_count、target_generation 及所有指标记录保留。历史 target_source/effective_config 无法追溯，未用当前配置伪造。

备份（私有、位于忽略目录）：

- `/Users/miaozean/Desktop/workspace/JobScheduler/.cache/upgrades/rc2-connections-20261012/runq-before.db`
- 同目录 `config-before.yaml`、`manifest.json`。
- 旧二进制：`/Users/miaozean/Desktop/workspace/JobScheduler/.cache/upgrades/connection-binary-before/runq-rc1`。

备份后的新任务仍会继续写入当前 DB；恢复历史备份前需要先保留当前数据，不能直接用旧 DB 覆盖新历史。

## 给运行中的 agent

本机已经升级，现有实验不要重新 submit。继续按 job/task ID 查询、跟踪和取消；连接异常不能证明实验未提交。

项目使用 `command`；旧 `command_template` 仍可读，但两者不能同时出现。填写 `project.target`（代码源和默认执行 target）。新提交优先级是 `-t > job.target > project.target`；`RUNQ_TARGET` 只影响查询作用域。参数值全部显式放入 job 的 sweep/fixed_params，不再依赖项目参数定义中的 default/min/max/include/glob/strict。

远端机器的 CLI 仍共享 `~/.runq/bin/runq`，但各 shell 必须 source 自己的 env。controller 为 `79eabc06b6cb`，HOME 为 `/home/2/un05102`：

| target | source 命令 |
| --- | --- |
| tsubame | `source /home/2/un05102/.runq/f/79eabc06b6cb/12762a04/env` |
| tsubame-ar9055 | `source /home/2/un05102/.runq/f/79eabc06b6cb/03850640/env` |
| tsubame-ar9094 | `source /home/2/un05102/.runq/f/79eabc06b6cb/9743db5e/env` |
| tsubame-c0-evaluation | `source /home/2/un05102/.runq/f/79eabc06b6cb/fb2ea2c3/env` |

替换旧 shell rc 中的 `source ~/.runq/env`。每个 env 指向本机 controller、对应 target 的独立 socket，后续连接其他 target 不会改掉它。不要手动删除其他会话的 socket，也不要根据网络失败重新提交正在跟踪的实验。
