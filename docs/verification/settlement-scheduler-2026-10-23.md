# 60独立结算计划执行器验证

2026-10-23，`E:\GOEPAY`。本增量为独立scheduler包、命令及Docker/Compose接入，没有修改web、API、payment核心或既有核对worker。只用本地随机schema PostgreSQL（专用55434实例）、合成凭据与本地容器；无生产、真实密钥或provider访问，无服务部署。

## 行为

`cmd/settlement-scheduler`仅当`GOEPAY_SETTLEMENT_SCHEDULER_ENABLED=true`才连接数据库并运行；缺省或false时明确记录disabled并退出0，连主密钥也不解析。其他启用值拒绝。命令只使用bootstrap数据库/Store，不加载付款provider配置、平台私钥或渠道凭据。

Scheduler.Tick只调用Store.ScheduleSettlementPlans，创建/恢复已授权计划原due run与job，不做金额生成。Handler只接受kind=`settlement-plan.generate`及唯一JSON字段`run_id`，值必须规范正int64十进制字符串；拒绝重复、未知字段、null、数值ID、数组、额外根对象、超长载荷和溢出。通过后只调用GenerateSettlementPlanRun(ctx,id)，没有provider接口或管理员actor。

Runner仅Claim此kind。默认30秒扫描、空队列等待1秒；每个任务1分钟租约、20秒续约、5分钟处理时限。续约使用原token CAS，独立请求deadline不超过min(租约/2,5秒)；失败取消工作，不用失效tokenComplete/Retry。成功完成或失败重试同样使用原CAS，重试只记录固定原因而非核心异常/受款信息。核心负责原申请幂等和每商户事务；任务重领不创建新资金意图。

经root明确授权，额外极小修正共享jobs.Queue的exhausted预清理：原SQL会改动其他worker kind的过期耗尽任务；现加kind=ANY($1::text[])及kinds参数。claim、provider渠道过滤、Renew/Complete/Retry其他字段均未改。旧核对worker不增加生成任务kind。

Dockerfile新增`/out/settlement-scheduler`。Compose独立settlement-scheduler服务默认false，仅DATABASE_URL、GOEPAY_MASTER_KEY和enabled三个环境项，依赖postgres/migrate，无端口、provider配置或密钥文件挂载。启用服务并不替代计划原授权或原申请审批/出款。

## 红绿与最终命令

纯payload、默认禁用、kind隔离、租约丢失、固定重试原因与默认命令启动测试先在stub失败后通过。续约deadline测试先证明错误地沿用5分钟工作上下文，加入独立短deadline后通过。真实PG队列范围测试先观察到scheduler与notification都被标dead，修复后只清理指定kind，保留原red日志。

| 检查 | 结果 |
| --- | --- |
| `go test -p 1 ./internal/platform/jobs ./internal/settlementscheduler ./cmd/settlement-scheduler -count=1` | jobs 1.536s、scheduler 9.216s、cmd 0.940s，全部exit0，真实PG55434 |
| `go vet ./internal/settlementscheduler ./internal/platform/jobs ./cmd/settlement-scheduler` | exit0，无诊断 |
| `GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -trimpath ... ./cmd/settlement-scheduler` | exit0 |
| `docker compose --env-file`本增量合成env`config --format json` | 仅3项scheduler环境、默认false、正确独立entrypoint及DB依赖 |
| Linux静态二进制，已缓存nginx:alpine容器，`--pull never --network none --read-only`，无效DB/key且未设置enabled | exit0，只输出disabled，0数据库/生成工作 |

尝试相同隔离运行使用alpine:3.22时本机未缓存该镜像，Docker exit125；没有拉取，改用已缓存nginx:alpine并覆盖entrypoint运行同一静态二进制。此项是Linux禁用入口烟测，不宣称完整生产Dockerfile镜像构建或部署验收。

真实PG测试用公开Store方法创建本地商户、脱敏收款配置、显式本人opt-in和disabled计划。仅授权准备阶段使用WithFundsActionGuard+同Tx合成唯一grant表；worker运行阶段不伪造管理员、不使用guard或付款凭据。8个并发Tick只有一个原run/job且无新增journal；生成一次申请/预算use，过期job重领同ID，旧token Renew/Complete/Retry均ErrLeaseLost，重放不重复reserve；grant仍只消费1次，其他pending任务不被领取。另以阻塞生成器观察真实DB lease被续期后由原token完成，attempt仅1。没有将这些结果替代60核心/整体固定候选验收。

原始日志和Linux测试二进制位于`.tools/verification/settlement-scheduler-2026-10-23/`。go-tests、go-vet、linux-build、compose-config、linux-disabled-run-existing-image为绿色证据；queue-scope-red、renew-deadline-red为红证据。source-hashes.json归档12个变更源文件SHA256；主候选最终manifest仍由root固定。
