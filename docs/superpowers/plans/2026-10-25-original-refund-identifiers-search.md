# 原退款编号、表头与搜索字段

原Pro退款列表模块index-CaPsjcRn.js的筛选明确为refund_no（退款单号）、out_refund_no（商户退款单号）、trade_no（关联订单号）；表头退款单号下面同时展示商户退款单号。当前Go控制台只给出综合搜索并将f.refund_no展示为退款单号，原字段语义不清。

先明确已有公共支付协议事实：provider_http.go返回refund_no=Refund.ID的十进制字符串、out_refund_no=Refund.RefundNo；后者在refunds.refund_no存储商户原请求号。支付机构请求使用RF+ID作为上游命名空间，与原公共退款号不同，不能把RF前缀当作平台显示编号。公共支付接口与已有控制台refund_no字段保持原行为，控制台新增platform_refund_no=f.id::text及out_refund_no=f.refund_no，前端原双行表头用明确新增字段展示，不改写任何历史号。

为退款列表、过滤CSV和统计复用同一固定SQL字段搜索：refund_no按平台ID规范正int64文本匹配，out_refund_no按原商户请求号精确匹配，trade_no按原关联订单号精确匹配；无选择时保留旧综合搜索并补入平台退款ID。值一律绑定，不接受任意列/SQL，保留独立角色租户谓词。现有订单搜索原十字段不变。summary允许同样search_field，既有时间/状态/方式/通道筛选合并；计数和金额口径不变。

前端恢复三个原选项，原column/value及uid链接按退款专用字段映射并拒绝歧义/重复/越权；选定精确字段不偷偷trim原商户请求号，默认综合搜索仍trim。表格复用DataTable，以显式refunds模式局部增强，旧无新字段数据保留原主显示、未知副字段不编造。统计和CSV携带完全相同的筛选字段，不因原链接失败退回全平台查询。

先真实PG/HTTP RED覆盖两类原号、双商户同一商户请求号/平台号隔离、旧控制台字段保留、新字段字符串、列表/统计/CSV一致与非法字段/ID；前端URL映射、精确值、双行显示及两后台/375实际浏览器测试。冻结后完整前端、受影响双平台server/db/合同/race/vet/build、本地增量部署和33来源检查。无退款操作、支付机构调用或资金变化。
