# 原商户每日交易统计实现计划

**依据：** 本机Pro商户index-B5ldFjlS.js；只筛支付方式，近7天三系列为订单金额/分成金额/退款金额，原七列表格day/realmoney/getmoney/refundmoney/orderCount/successCount/successRate，页脚合计按笔数计算加权成功率。

**实现：** 复用已验证original daily聚合，商户固定自身session ID、七个北京时间创建业务日期与type，拒绝uid/channel（包括空或自己）；channels原视图仍仅管理员。原getmoney显示已付订单的原merchant_amount总额，退款另列，明确原净额不等于当前可用余额。金额/计数与合计精确字符串，不用JS Number加资金；成功率按总成功/总订单四舍五入。原默认GET/CSV报表接口保留。

**隐私：** 商户SQL不连接referral_profit_costs，不返回profitmoney、missing_costs、上游费率/成本或管理员汇总；不能把管理员原报表直接开放给商户。已有任意租户查询保护维持403，原始条件严格单值。读取RO快照/限并发/超时，CSV相同七天与type、合计最后一行、成功后一次下载审计，无新增资金操作。

**文件：** original_order_statistics.go扩展原净额聚合及角色DTO；新merchant_daily_statistics_ux_test.go真实PG先RED。前端OriginalOrderStatistics.vue按角色恢复原七列/图例/页脚与type，helper增加角色校验和三系列，不显示管理员字段。ReportPage只daily切到两角色原视图。两个原通用商户UI测试随需求更新为原七天/方式/CSV职责，旧默认API仍由原合同覆盖；新增桌面/375和隐私、失败/迟到响应测试。

- [ ] 真实PG两商户/支付方式、10%商户手续费快照、部分退款、原净额/总计/66.67%、uid/channel拒绝与无成本字段先失败；正向CSV精确范围及一次审计。
- [ ] 商户报表在隔离测试schema临时不可访问上游成本表时仍可只读返回，证明没有读取私有成本。
- [ ] 实现管理员原视图行为保持、商户原默认接口保持；前端先RED再实现原七列/三系列/合计和两种尺寸。
- [ ] 固定源完整前端、准确双平台受影响后台、全仓vet/build与基线区分，本地备份恢复部署和实际只读验收；其它原UX差异继续。

真实PG先以原商户403失败，再验证10%手续费快照、部分退款与100.50/90.45/12.00及2/3=66.67合计、跨商户拒绝、原默认API、CSV一次审计；另在随机测试schema重命名私有成本表后仍返回精确大金额，均已通过。两个helper与桌面/375原七列/三系列/页脚/过滤/CSV先RED后GREEN。原通用UI的两个测试因需求改变不再适用，保留原失败并更新为原七列/方式/附件范围职责；旧API合同保留。更新导出fixture最初少附件头且BOM为转义文本，客户端正确拒绝，失败保留后仅修正fixture真实附件头+BOM，应用保护未改。完整固定源回归/实机尚待完成。
