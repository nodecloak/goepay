# 商户本人联系QQ与单笔上下限

以冻结166全部1881文件逐字节校验为父版本，只在隔离工作区开发。原Pro账户设置 `public/static/assets/index-fMgyEUZ4.js` SHA256 `87b9217c066343aa61d67e4cd58866fc5a85dc661aeff50a8a7996ca4ea1d48e` 证明原字段`qq/pay_minmoney/pay_maxmoney`、元单位、空值不限制及保存设置交互。原业务插件按用户要求暂缓，本文不涵盖未接入的个人API开关、密钥登录或买家加费；它们仍在后续清单，不能只保存字段就声称生效。

当前`validateOriginalOrderAmount`已经消费profile中的个人上下限，并在新原订单/报价出生时与有效组和全局规则交集。本次复用引擎，不复制另一套比较，也不修改历史金额、费用、路由、attempt或MAC。旧订单精确重放仍返回原记录，新单不能因个人值比组宽而扩大权限。

新`GET/PATCH /console/v1/merchant/account/personal-settings`仅商户本人原持久会话；委托/管理端拒绝，query/未知/重复/大小写混淆字段拒绝。DTO为本人完整字符串ID、qq、pay_minmoney、pay_maxmoney、bounds_valid、fingerprint、can_update。PATCH仅这三个字段和expected_fingerprint，string金额精确到分、空值保留、无浮点/负数/科学计数/溢出，最大非0时不得小于最小。QQ沿用当前管理员原联系方式5–20位数字验证。

同事务merchant→用户→原session锁，CAS绑定原ID、config_revision、启用状态与原字段，写入只合并三个字段并增config_revision。非秘密审计只记字段名；审计失败回滚。最后可能等待的写入后重新按数据库clock确认原session再提交。不添加密码/MFA确认。读取旧合法非规范小数可规范显示，但fingerprint绑定原值；旧min>max显示bounds_valid=false允许本人修复，不把无效区间伪装为无约束。

先真实PG/HTTP RED：缺路径；再验证本人/他人/委托权限、CSRF、精确字段、并发同fp唯一winner、撤销/锁等待到期、审计失败、原无关profile/密钥不变、原MAC/费用不变及新单真实范围生效。前端模型/组件先RED，原字段与375/1440布局、确认/取消、busy防重复、刷新CAS冲突、完整ID、client/CSRF变更和迟到响应不串账户均需覆盖。独立审查后才可累计整合；本次不部署、不联网、不触碰生产或真实资金。

整合时发现login/session直接序列化内部identity.User，id为JSON数字，会在浏览器先丢失大于2^53的精度。追加真实本机大ID登录→原cookie会话→本人设置RED，修复仅在两个console响应使用独立字符串ID投影，保持原role/name/admin_class/CSRF/Cookie语义。内部User结构、数据库session、既有权限及历史MAC格式不变。OpenAPI分别明确定义登录和会话DTO，不把原User内部字段公开。此修复需既有登录、MFA和委托回归后才可合入。
