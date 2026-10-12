# 本机 RC3 安装验收（2026-10-12）

功能提交：
- `7df64b6` 时间：按 attempt 分离排队、资源占用和脚本执行。
- `71b9b3e` W&B：统一任务目录和配置传递，SDK 0.6.0rc3，迁移/固定版本 offline 验证脚本。

## 已安装

- 本机 `/opt/homebrew/bin/runq`：`v0.6.0rc3`，内嵌 production dashboard，controller PID 81429。
- 本机 SDK 测试环境：runq-sdk 安装元数据与源码均为 `0.6.0rc3`。
- 远端共享 CLI `/home/2/un05102/.runq/bin/runq` 已升级到 `v0.6.0rc3`；实际 24.2 MiB 上传成功。tsubame、tsubame-ar9055、tsubame-ar9094、tsubame-c0-evaluation 的 connect 均返回 0，完成远端 socket round trip、版本与身份验证。
- controller ID 仍为 `79eabc06b6cb`，四条 forward 的原路径保持不变、health 全部 up。
- 远端执行器 runqd 和正在运行的训练未停止、未重新提交。其他 agent 的训练环境 SDK 与训练代码迁移由用户协调；本次没有向它们发送消息。

## 数据保护

正常停止旧 controller 后运行 `artifacts/migrations/rc3_timing.py`。
SQLite backup API 包含 WAL；backup 和迁移后 integrity_check 都返回 ok。
只添加 `tasks.timing_json TEXT`，未改写旧 started_at；事务中所有旧表旧列的行数和 SHA256 完全一致。
迁移重复运行测试通过（另一个备份目录），历史 retry/external_id/started_at/status 保持原值。

备份目录：`/Users/miaozean/Desktop/workspace/JobScheduler/.cache/upgrades/rc3-20261012/`：
- `runq-before.db`：30 projects、62 jobs、169 tasks 的完整旧数据库。
- `config-before.yaml`、`runq-before`：旧配置和二进制。
- `manifest.json`：各表迁移前后行数/旧列 SHA256。
- `post-start-check.json`、`health-after.json`、`binaries.json`：启动后验收与二进制校验值。

启动并重新 connect 后再次核对：169 个 task 的 id、job_id、project_name、retry_count、external_id、target、target_generation 全部与备份一致。当前运行任务仍为 tk4c6f47c8 / external_id 8980605；后台已补入该任务旧 wrapper 的估算入口证据。

## 用户测试入口

刷新已有 dashboard 页面即可测试 RC3。本机真实任务列表和详情已通过浏览器检查：Queued / Scheduler run / Script time 分列，旧 wrapper 显示 ≈ 和 `legacy_wrapper` 来源；详情有三个目录字段。浏览器无 warning/error。归档入口未展开。
新提交使用 RC3 wrapper 才有精确的本次脚本开始/结束；历史字段不足的记录保持未知或约值。

完整 Go 回归通过；Web 16 文件 / 85 tests 和构建通过；SDK 94 tests passed；W&B 0.23.1 真实 offline 验证通过。真实 online resume / URL 服务链路 / artifact download / official sync 上传尚未实测，见 rc3-wandb.md。

本次仅本地提交与安装，没有 push 或发布 tag。Archify 保持暂停。
