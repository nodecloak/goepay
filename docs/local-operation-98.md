# 固定98本地运行说明

2026-10-25，项目目录 `E:\GOEPAY`。当前本地Docker项目为 `goepay-local-final-bf15d7cd`，运行固定98/schema83；完整本地验收已完成，结果与兼容边界以[98集成记录](verification/integrated-candidate-98-2026-10-25.md)为准。

| 用途 | 本机地址 |
|---|---|
| 管理后台 | http://127.0.0.1:8185/admin/dashboard |
| 商户后台 | http://127.0.0.1:8185/merchant/dashboard |
| API健康检查 | http://127.0.0.1:8184/healthz |
| 数据库 | 127.0.0.1:55448 |

前端通过同源`/console/v1`访问后台API；商户支付兼容接口在API8184。8180/8181是旧环境，未作本轮部署切换。

仅有`postgres`、`api`、`web`三个服务默认运行，真实支付/OAuth/邮件/打印/通知外发、所有定时worker及自动资金执行均关闭。当前为sandbox/simulation，页面中的支付和退款只用于本地模拟。模拟闭环通过不代表已开通真实机构产品或已完成生产验收。

当前合成管理员用户名为`admin`，合成商户用户名为`merchant`；密码保留在本机验收目录`.tools/live98-77a185006dd14a9087cb1ce7b60b309e/browser-fixture.env`的`GOEPAY_SEED_PASSWORD`，不复制进报告。该文件只供本地合成账户使用。登录密集测试后需等待原15分钟限流窗口自然恢复，恢复流程不清空限流表或审计。

在PowerShell中查看本项目状态：

```powershell
Set-Location -LiteralPath 'E:\GOEPAY'
docker compose --env-file '.tools/deployment74-bf15d7cdc2c9445da7465641360cb661/synthetic.env' -f '.tools/deployment74-bf15d7cdc2c9445da7465641360cb661/compose.yaml' -p goepay-local-final-bf15d7cd ps
```

Docker重新启动后，使用同一个compose文件、项目名和现有卷启动这三个服务：

```powershell
docker compose --env-file '.tools/deployment74-bf15d7cdc2c9445da7465641360cb661/synthetic.env' -f '.tools/deployment74-bf15d7cdc2c9445da7465641360cb661/compose.yaml' -p goepay-local-final-bf15d7cd up -d --no-build --pull never postgres api web
```

数据卷为`goepay-local-final-bf15d7cd_pgdata`。保留此卷及原合成配置；重新建空库、使用新项目名或切回早期compose不能等同于恢复当前实例。升级与恢复验收使用独立脚本和逐表哈希，日常启动命令不执行这些操作。

应用源码快照、完整测试和部署镜像相互绑定；当前1394源码文件SHA均与固定98一致。运行配置及所有验收原始日志在`.tools`下，不随镜像公开；最终全表恢复归档路径和SHA在98集成记录中记录。生产切换、真实密钥导入、真实历史导入或启用外部能力需另行设计和验收，本说明不执行这些操作。
