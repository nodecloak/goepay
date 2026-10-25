# 本地127：双后台原退款统计

独立模拟预览8187/API8186已从126更新为固定127，schema88、202表。恢复原两个退款列表的520px“退款统计”弹窗及六项字段：退款总笔数/金额、成功笔数/金额、处理中笔数/金额；查询点击时的当前筛选全部记录，不受分页限制。金额与计数全程字符串；处理中包含pending/unknown，失败仅计筛选总额，不当作退款成功。固定SQL搜索/筛选构造器与退款列表共用，独立认证租户谓词；拒跨商户、重复/未知/分页/导出参数，4并发与10秒DB期限，单条只读聚合，无支付机构调用和额外审计。

完整402单元、247浏览器通过，0失败/重试，另9个独立实机条件用例未在该命令启用。127完整server/db、账务烟测、退款/账户/登录/矩阵/概览/CSV相关HTTP合同Linux race及全仓vet/build通过；同源Windows完整server/db与新增合同、全仓vet/build也通过。原非零成功/处理中/待核实/失败、两商户、日期/方式/状态/搜索过滤由隔离真实PostgreSQL合同验证，不用空预览冒充所有状态验收。最初夹具复用退款号导致冲突，修正夹具后取得旧路径返回列表的真实RED；首次GREEN中错误假定夹具审计为0，改为严格比较读取前后审计/账务基线，未放宽实际不写约束，失败记录保留。

固定120的完整两平台长回归已完成：实际清单38包，36测试包通过，2无测试文件包为cmd/import-legacy与internal/bootstrap；test/vet/build均退出0，Linux实际race。Linux2160、Windows2163个测试通过事件包含子测试，不当作独立业务功能数。长回归均显式关闭12个PHP互操作，它们已在固定120的独立断网PHP验证全部通过；Linux另一个可选Node客户端用例未在长回归启用，已在固定127单独开启并通过本地微信/支付宝TLS+Go HTTP+PG互操作，Windows长回归也已执行该Node用例。120与127的898个命令、非server内部包、db及Go依赖文件逐字节一致；新增server/HTTP/前端由127受影响回归覆盖。没有把完整120结果改写为“127全仓逐项重跑”，也不把测试通过称为全部原UX完成。

实机管理员/商户、1280/375的原六指标、筛选快照、无分页参数、宽度及Escape关闭回焦通过。预览退款为空，只验证真实精确零；没有为制造数据创建支付或退款。首次实机全表审计哈希失败，经定位为正常商户登录创建的merchant-notice-events审计；保留初次FAILED记录，改为登录后建立每角色基线，逐项确认统计API/UI期间0新增业务审计、所有旧审计行不变、唯一新增审计确属正常登录通知。应用代码未为此次失败改动。最终0业务POST/外部请求/页面或HTTP错误，订单/退款/凭证/分录/账户均不变。其后全部82页观察通过，最终33项账务来源检查全0；观察不等同全部条件行为验收。

固定1540文件，manifest `c93522e8e30d2e7b2ea11854a15cd9f3fb10bcb318e43e6df8c9589df2e2daf2`。202表完整备份恢复、所有行/列/序列与88迁移checksum一致，恢复及更新后33零检查；备份SHA256 `796e95e9df386c54bcdde50f70c7fe6a1964adaca2f0de35dc03e88991801202`，恢复库/dump保留。实际8个Go二进制、3个HTTP静态资源符合固定构建。runtime `sha256:e9e589378e3e49667c10a3025a2b4204fd804f5ccf97d4e57b86bcf9a93bdb2f`，web `sha256:2f0f091732f89e5b4fb37afbcf4931f4f34dc25ebabd7da473971f14b4f30f38`。

证据位于`.tools/deployment-ux103-5a97d6ad12ef4df68a7a5be53e39832a/updates/127-9d53ca0476384a9b805a6029eb61a244`的PROMOTED、BACKUP_RESTORE、IMAGE_ACTUAL、NATIVE_WINDOWS、REFUND_SUMMARY_LIVE.json（首轮失败）、REFUND_SUMMARY_LIVE.verified.json（校正后通过）、pages/RESULT和post-pages-reconcile。前端`.tools/ux127-front-0b17ec40aa16443b85e659f444130a4f`，Linux`.tools/ux127-refunds-07c4fba4a53246a5b82793c5b228defb`，Windows`.tools/ux127-windows-14167387979b4c3dba7d91dc4235375b`，Node`.tools/ux127-mini-c8d21c2826b04df6991f77ebc2884836`，长回归与898文件证明`.tools/full120-final-summary.json`。

原8185固定98保留。其它业务插件按用户要求暂缓，原完整用户组、通道、绑定和其它专用/批量状态动作继续核对；真实通道、导入、外发和自动任务仍关闭，没有访问生产或真实资金交易。
