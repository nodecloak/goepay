# 原合单子订单的历史读取

范围是原管理/商户订单详情中的“查看子订单”，不把历史声明转成新的微信合单执行权。Pro 可读前端和安装结构确认父单 combine 标记、子号、上游号、金额、NULL 退款、原状态与结算条件；加密控制器没有被冒称已还原。

schema_version=1 的脱敏导入增加可选 combined_orders。每组明确原父号/商户、source_plugin、CNY、原 charged_money、父 settle、1..50 个子断言。子金额精确求和等于明确实收声明；不要求等于普通父单标称金额，不生成假的104收费报价。子退款 NULL、0 与部分退款保留，子状态0..4、结算0/1。新字段不存在时旧 raw digest、旧退款和订单 MAC 保持原规则。

116 新建独立 export/parent/child HMAC 清单和出生见证；显式新导出才写 version1。关系、数量、金额、顺序摘要与原导入 receipt 同事务封存，不能事后挂接。子断言不成为普通 orders，不重复余额开账，不创建 provider attempt、退款、通知或队列。只读来源是历史断言，始终 read_only=true、verified_payment_source=false。

GET /console/v1/{admin|merchant}/orders/{trade}/suborders 不接受查询权威或任何写方法；商户范围来自当前持久会话，继承合法父单代登录读取。普通/外商户/缺失关系404，清单缺损409，不返回空成功。详情只在验证当前关系后新增 can_view_suborders=true 与 order.combine=1；其它订单明确 false/0。弹窗继承原列、条件结算列、状态与部分退款文案，并处理焦点、Esc、父弹窗 inert、取消和身份切换。

已固定验证包含实际 PostgreSQL Validate/Apply、旧schema1 raw hash/MAC兼容、完整性篡改、回放冲突、单连接读取、实际HTTP租户/代登录/能力观察及组件/浏览器交互。完整套件、升级与部署验收另以源清单和测试收据为准，文档不代表整项目已完成。

真正直连WX V3合单须独立实现首次发送、完整子回执聚合、部分成功/未知关闭与子退款资金图；不能复用204的历史声明当付款证据。全量历史清单验证的读取成本仍须纳入最终规模测试。
