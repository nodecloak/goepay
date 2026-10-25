# 46/47 本地验证

范围：E:\GOEPAY；临时RSA/TLS和随机PostgreSQL schema。未访问生产、未读取导入凭据、未发起真实资金交易。不是完整Pro兼容声明。

46首次订单attempt与收款公开身份HMAC同事务提交。红测重现同AppID更换MchID仍能submit/query/notify/close/refund/queryrefund，以及checkout继续展示旧二维码；修复后拒绝所有这些动作且无错误账户外呼。历史真实通道缺proof不自动接管；密钥轮换保持身份不变。普通、付款码、预授权共用入口。专项 payment18.821s/contract3.420s；广泛支付/预授权/付款码/V2回归96.431s/23.791s与定向vet通过。

47独立请求不借订单占位，原持久会话/商户/应用/公开账户/浏览器/state/code均绑定；标识AEAD保存，显式并发只读一次。与原支付web code共享不可变全局claim，mini独立domain且迁移回填旧claim。过期、撤销、代登录、跨商户、跨应用、同AppID换MchID及慢外呼后撤销均拒绝。临时TLS通过真实微信/支付宝OAuth适配器，无orders/provider_attempts/journals。联合旧OAuth专项payment22.880s/contract14.597s与vet通过；改为实际Server.Handler路由后contract5.542s通过。

前端71项单元/组件、类型检查及构建Ag1N_9gG通过，含6项独立授权测试；两平台显式创建/手工查询/显式reveal/敏感显示清除已通过真实浏览器mock。完整mock浏览器70项15.4s通过，8项live按开关跳过。首次新增e2e因output隐式status与父status匹配两个节点失败，改精确定位output后6项4.3s通过，未降低业务断言。

代理红测：未加location时identity授权路由错误返回SPA，实际断言失败。已补 `/identity/` 转发及整个identity路径日志脱敏，部署后proxy-smoke通过，包括真实授权路径拒绝与路径令牌不出日志。首次OpenAPI生成缺request_id路径参数定义失败，补定义后离线自检通过：75paths/85operations。

固定候选47-verified-candidate-2034d756-acbd-4a08-b6bd-440a84237d11含573源码文件。Windows全库+vet+cmd build exit0：payment414.023s、contract232.210s、backoffice88.094s、identity66.608s；Linux镜像goepay-race-local-47全库race+vet exit0：payment364.330s、contract211.729s、backoffice72.009s、identity63.577s。并行负载下耗时不是独占性能基准。后续48新文件不属于此快照。

Docker api/worker/migrate/web重新构建部署，原45库升级46/47，无清库。smoke、两通道simulation与proxy-smoke通过；CLI reconcile七项全0。实际Docker浏览器7项57.7s通过，含75个partial原菜单巡检；独立资金授权钱包用例本组默认skip，不借旧证据冒称本次运行。

恢复演练先因全库dump包含并发测试临时schema而失败；将dump限定应用public后，又因归档CREATE SCHEMA public与新库默认schema冲突失败。修复为只对本次创建且严格GUID校验的临时数据库使用pg_restore --clean --if-exists，随后exit0，47迁移、journal/balance/funding/subset异常均0。本次临时库清理由PG checkpoint同步等待后完成，未删除应用数据库或测试schema。此脚本增量不属于此前全库源码快照，实际恢复绿测单独列明。

46 SHA256：28C92AF776F2A1F03969985DC422E0FB4C3E01AB52FCA2ACF4ABBCADEE867274。
47 SHA256：FAFDB1973B08623893C796849EEA53C9BCFD98A50DE1529D005B24B18DA1A4C1。
迁移部署后保持冻结，后续更改须新增迁移。
