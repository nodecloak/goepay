# 商户运行子通道与原账户恢复（168B，migration98）

本候选以冻结166的1881文件逐字节校验复制为基线，独立目录和合成本机PG运行，不含生产配置、私密导入包或外部机构请求。实现只针对已接入的微信V2/V3和支付宝真实适配器，原业务插件及无法证明的服务商进件不伪造支持。

原依据是本地Pro Channel.php getSub和-2分支：受信父配置仅声明的逻辑槽位可被子账户替换；真实有效组的该支付方式为-2、本人owner、父子可用、订单money上下界共同筛选；随机/子账户毫秒顺序/权重/首个四种选择，无候选不回退父通道。子账户usetime与169父全局head独立。

Typed账户头和immutable AEAD版本分别保存；AAD和原HMAC绑定owner、child、版本、parent、schema与真实账户/产品身份。只有原super可新增、编辑参数或归档；本人原merchant可设置状态、权重、单笔界限和轮询。owner不可转移，参数更新保留原版本。停用缺失父模板的原对象仍可执行，启用须重新证明当前资格。逻辑归档保留原单恢复，不物理删除历史。

-2在排序的真实parent及child锁内批量重读，原assignment、子账户source/head、费用quote同事务封存，最后数据库clock在所有写入后校验会员期限；失败全部回滚。96父费率身份保留，98原执行身份另存；provider_attempt.channel_id仍是原parentID，不注册虚构child providerID。v1无child的编码保持，v2携带child；nil/v1 golden有本地收据。修改当前政策不重封历史报价。

所有标准支付、浏览器/聚合收款、充值、OAuth、mini、付款码、预授权、capture/recovery、Notify、Query、Close、Refund/RefundQuery和分账原订单恢复使用统一原账户来源。未具备child产品或OAuth实际能力时拒绝，不能借父cap。动态总门在clone后仍追原server，交换中停用也不继续发送。未知支付只查询原意图，不换账户或新号。

原birth独立MAC与原fee/child来源共同核对。98 schema/登记/route/quote/terms被删除或合法旧头恢复，不能降为pre98借父执行或父只读能力；真实pre96兼容分支仅在既有出生/迁移来源均未要求新格式时保留。96父政策、98child头及商户轮询以append-only审计次数锚定修订，169父head与childhead以同策略immutable来源最大DB毫秒核对，同毫秒合法源等价；不回填旧历史，不人为增加ID次序。

页面按原专用列与原角色动作消费实际DTO，所有ID字符串、整数分、BigInt比例；字段只公开是否存在。新建/修改提交后响应丢失或DTO目标不符，清理私密输入并锁住重复提交，必须显式读取核对。会话/CSRF/owner变化及迟到响应不能回显旧秘密。GET不写、不创建支付材料、不触发provider。列表count/page精确同筛选；订单/CSV按真实child FK且保原owner。

首页通道费率复用下单effectiveRuntimeFee纯函数，按本人已分配真实parent批量读取政策与审计。费用关闭0，组0继承父、正组优先；无法确认null，界面显示未知。多个父不同费率不合成为0，旧paid fee保持。财务汇总仍原单SQL；费率是另外的本地一致读事务，未声称与财务数字同一SQL快照。

## 最终集成合同（本分支不复制99/100）

本166分支自洽包含96、97、98，root须与已审99父全局限制合成。-2候选每真实parent接CheckRuntimeChannelLimitsTx，子单笔交集比较原money；chosen admission同route事务seal，finalDeadline取组与时段最小。v1/v2共同验证可选GlobalLimits，不能版本早返绕开任一域。旧nil/nil96字节不变。最后DBclock必须在childsource/head、quote、parenthead/counter等所有等待写入之后。与100成功realmoney口径合成时，子页面成功金额应消费可信realmoney，不能顺手把单笔money比较改成实付或净额。

root173/175的App、dashboard公告/欢迎和session ID等不同改动由root三方保留；本候选不覆盖其源码。migration连续链95→96→97→98后还须root组合99/100整链与both-on延迟写回滚实测。此候选和收据不是部署声明。
