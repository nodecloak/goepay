# 双后台全部页面UX复核来源清单

用户澄清：订单只是例子，两个后台全部页面必须review。本表逐页绑定原菜单、原视图/编译模块和当前真实路由组件；静态来源核对不等于按钮已实现或交互验收通过。完整候选动作、字段、状态文本和来源SHA保存在同名JSON。

原源码全部在本地读取，未访问生产或发起资金交易。原控制器加密的行为不能从按钮文案推定。

| 后台/页面 | 原组件模块 | 当前实际页面 | 原动作标签候选（完整见JSON） |
|---|---|---|---|
| admin/dashboard · 仪表盘 | index-BayLJ6d8.js | dashboard | 刷新数据、清理缓存、修改密码、取消 |
| admin/trade/order · 订单管理 | index-lAIAtQd1.js | orders | 搜索字段、搜索内容、结算状态、关闭、退款时间、拉黑、重新通知(异步)、重新通知(同步)、查看子订单、编辑接口订单号、取消、添加黑名单 - 、提交、导出 、改未完成 (可执行 、改已完成 (可执行 、API退款 (可执行 、冻结订单 (可执行 、解冻订单 (可执行 、删除订单 (可执行 、重新通知 (可执行 、确认结算 (可执行 、手动补单 (可执行 、拉黑IP、拉黑账号、手动退款确认、确认结算、改未完成、API退款、手动退款、冻结订单、重新通知、删除订单、改已完成、退款记录、解冻订单、手动补单、批量API退款确认、API退款确认、确认退款、退款金额、刷新间隔 |
| admin/trade/refund · 退款记录 | index-CaPsjcRn.js | refunds | 搜索字段、退款单号、搜索内容、退款中、退款金额、退款统计、关闭、退款总笔数、退款总金额 |
| admin/trade/blacklist · 黑名单管理 | index-Dd_3yCyx.js | resource / blacklist | 搜索、添加时间、添加、导出、删除、添加黑名单、取消、确认、拉黑类型、拉黑内容 |
| admin/trade/stat/daily · 每日交易统计 | index-C2O4Lmyd.js | report | 退款金额 |
| admin/trade/stat/user · 商户交易统计 | index-CylHtmhC.js | report | 查询日期、查看方式 |
| admin/trade/stat/channel · 通道数据统计 | index-hT-pj3fY.js | report | 查询日期、退款金额 |
| admin/trade/stat/province · 地区数据统计 | index-DF-qDpRw.js | report | 需检查导入组件；不推断无操作 |
| admin/trade/stat/buyer · 支付用户统计 | index-_M2b8ir_.js | report | 查询日期、取消 |
| admin/profitsharing/receiver · 分账规则 | index-ydfe_neb.js | channel-sharing | 搜索字段、搜索内容、新增、删除、新增分账规则、修改分账规则、关闭、保存 |
| admin/profitsharing/order · 分账记录 | index-BcN7mYkh.js | channel-sharing | 提交分账、取消分账、搜索字段、搜索内容、删除、导出 、查询结果、取消 |
| admin/profitsharing/internal · 站内分账规则 | index-CxvCqzdZ.js | resource / profitsharing-rules | 搜索字段、搜索内容、添加时间、新增、删除、新增站内分账规则、修改站内分账规则、关闭、保存 |
| admin/transfer/list · 付款记录 | index-CYTHVGS4.js | transfers | 查询结果、转账付款、批量转账、查询余额、转账金额、转账标题、转账备注、确认创建、关闭、导入Excel、导出付款记录、取消、导出、提交时间、导出表格模板、搜索字段、搜索内容、确认、查询完成、删除、批量操作、导出记录、删除记录、复制、查询状态、撤销结果、撤销、复制链接 |
| admin/transfer/stat · 付款账号统计 | index-D0BdOeFT.js | report | 查询日期 |
| admin/transfer/channel-stat · 通道数据统计 | index-WNu3YHg3.js | report | 查询日期 |
| admin/transfer/user-stat · 商户代付统计 | index-CDCxaHWS.js | report | 查询日期、查看方式 |
| admin/settle/list · 结算管理 | index-Cpj26fBW.js | workflow | 搜索、结算账号/姓名、结算方式、结算金额/实际到账、删除、批量操作、删除记录、手动、确认、修改结算信息、取消、保存、结算账号、结算统计、复制链接、关闭 |
| admin/settle/batch · 批量结算 | index-5nUERs6a.js | settlement-batches | 转账处理完成、批量转账、结算账号、结算姓名、确认、批量结算操作、生成结算批次 、生成时间、结算列表、生成结算批次、取消 |
| admin/merchant/merchant-list · 商户列表 | index-DzbjAsrt.js | resource / merchants | 添加商户、关闭、登录密码、结算信息、结算方式、结算账号、结算账号姓名、结算权限、重置登录密码、复制、重置商户MD5密钥、重置商户RSA密钥对、生成商户RSA密钥对、搜索备注或ID、新增 、编辑、删除、测试、新增子通道、修改子通道、保存、取消、新增一行、复制私钥、测试支付、代付记录、结算记录、手动提现、登录日志、搜索、搜索字段、结算姓名、搜索内容、结算状态正常、结算状态关闭、登录时间倒序、登录时间正序、结算账号/姓名、添加商户 、导出、结算、充值、导出用户、修改用户组、设置日期、查看实名认证信息 |
| admin/merchant/group-list · 用户组设置 | index-CCjDRx0B.js | resource / groups | 新增用户组、取消、保存、关闭、代付通道、转账方式、手动转账、结算配置、结算开关、结算周期、结算手续费、代付配置、代付功能、代付手续费、代付是否扣除余额、新增 、编辑、删除 |
| admin/merchant/group-buy · 用户组购买 | index-B2FwSy03.js | group-sales | 保存全部、编辑简介、编辑简介（GID:、取消、保存 |
| admin/merchant/record · 资金明细 | index-WFOFLbSD.js | ledger | 搜索字段、搜索内容、导出 、代付、代付退回、关闭、导出资金明细、取消、导出 |
| admin/merchant/internal-transfer · 站内转账 | index-DgvJvE3U.js | workflow | 发起方、发起方商户ID、提交时间、拒绝转账、取消、拒绝原因、审核通过、新增转账、通过、拒绝、新增站内转账、确认转账、转账金额 |
| admin/merchant/domain · 授权域名 | index-BltpJOJD.js | resource / domains | 拒绝、添加时间、审核时间、通过、删除、添加、审核中、添加授权支付域名、取消、确认 |
| admin/merchant/invite-code · 邀请码管理 | index-ChX3aBWp.js | invitations / invitation-codes | 搜索邀请码、添加时间、生成个数必须大于0、生成邀请码、清空全部、清空已使用、删除、取消、生成、生成个数、生成结果、复制全部、关闭 |
| admin/payapi/pay-channel · 支付通道 | index-DNfqr2Hp.js | resource / channels | 新增支付通道、修改支付通道、关闭、保存、刷新本地插件列表、下载、删除、绑定微信公众号：、绑定微信小程序：、绑定QQ互联应用：、退款金额、发起订单数、取消、新增一行、搜索、批量操作、新增 、绑定微信公众号、绑定微信小程序、编辑、测试、复制 、测试支付、批量绑定微信公众号、批量绑定微信小程序 |
| admin/payapi/pay-type · 支付方式 | index-NXVshjHq.js | resource / pay-types | 新增支付方式、新增 、修改支付方式、编辑、取消、删除、关闭、保存 |
| admin/payapi/pay-roll · 支付通道轮询 | index-Cr4lev2n.js | resource / pay-rolls | 新增轮询组、新增 、修改轮询组、编辑、取消、删除、关闭、保存 |
| admin/payapi/pay-weixin · 公众号小程序 | index-DGzFU5v8.js | resource / weixin-apps | 搜索、新增公众号/小程序、新增 、修改公众号/小程序、编辑、取消、删除、测试、关闭、保存 |
| admin/applyments/channel · 进件渠道配置 | index-DVcJ9Zy9.js | resource / onboarding-channels | 新增 、编辑、删除、新增进件渠道、修改进件渠道、关闭、保存 |
| admin/applyments/merchant · 进件商户管理 | index-DocsuwmU.js | resource / onboarding-applications | 添加已进件商户、关闭、提交、绑定本站UID、搜索类型、搜索内容、审核中、修改审核中、查询完成、修改绑定用户UID、取消、批量操作、新增商户进件 、改已完成、批量删除、修改备注、修改、详情、查询订单、修改绑定本站UID、删除、手动添加已进件商户、测试、测试支付、发起测试 |
| admin/applyments/subchannel · 子通道管理 | index-C5U5W_96.js | resource / subchannels | 搜索、新增 、编辑、删除、测试、新增子通道、修改子通道、取消、保存、关闭、测试支付 |
| admin/applyments/mchrisk · 商户管控记录 | index-9OC_cO5i.js | resource / merchant-risk | 设置商户违规通知回调、关闭、搜索字段、搜索内容、批量删除、导出、删除 |
| admin/applyments/form · 商户进件表单 | index-DhN7Ku4q.js | resource / onboarding-applications | 需检查导入组件；不推断无操作 |
| admin/complain/list · 投诉记录 | index-Cl6xa9dJ.js | resource / complaints | 关闭、手动获取、设置投诉通知回调地址、删除投诉通知回调地址、删除、上传中...、添加图片、查询中...、刷新、退款、查看订单、取消、通过其他方式退款、上传图片、拒绝退款、拒绝原因、API退款确认、退款金额、搜索字段、搜索内容、批量删除、批量处理投诉、批量处理完成、导出、详情、提交 |
| admin/complain/charts · 投诉统计 | index-CHYeaio0.js | report | 需检查导入组件；不推断无操作 |
| admin/complain/task · 同步任务 | index-Ce2eNQYC.js | resource / complaint-tasks | 同步状态、批量编辑、批量删除、编辑、同步任务已删除、删除、编辑投诉同步任务、取消、保存 |
| admin/other/risk · 风控记录 | index-CfnvDEoO.js | resource / risk-records | 搜索字段、搜索内容 |
| admin/other/get-token · 获取用户标识 | index-BySTvbcv.js | identity-lookup | 需检查导入组件；不推断无操作 |
| admin/other/onecode · 聚合收款码样式 | index-Dc9Owjji.js | collection-styles / collection-styles | 刷新、新增样式 、编辑、删除 |
| admin/other/onecode/edit · 编辑收款码样式 | index-C_AO16KG.js | collection-styles / collection-styles | 编辑收款码样式、新增收款码样式、上传图片、同步图片尺寸、保存 |
| admin/app/app-store · 应用市场 | index-CNLq1l1m.js | unavailable（用户指定暂缓） | 充值、充值已完成、取消、确认购买、确认更新、确认安装、下载安装、刷新 、充值 、解绑账号、绑定授权平台账号 、搜索应用名称 / 标识、搜索、重置、详情、删除、下载、关闭、充值余额、确认充值、充值金额、下载量：、转账方式： |
| admin/app/plugin-list · 本地插件 | index-zEWI8e72.js | operation | 搜索插件名称 / 标识、搜索、重置、刷新本地插件列表 、详情、取消、删除、转账方式： |
| admin/app/template · 模板配置 | index-CSBB45Vm.js | template / template-configs | 关闭、保存、取消、设置  |
| admin/app/addon · 扩展配置 | index-DS4XI152.js | resource / addon-configs | 需检查导入组件；不推断无操作 |
| admin/system/settings · 系统设置 | index-C6la0DtP.js | settings / settings | 登录页副标题、测试支付、测试支付收款商户ID、测试支付开启验证码、登录开启验证码、取消、保存、新增一行、删除、关闭、登录&注册配置、拉黑3天、拉黑5天、拉黑7天、关闭后自动重新下单、结算规则配置、结算总开关、关闭结算功能、结算周期、手动结算自动转账、结算列表自动转账、结算手续费、结算手续费最小、结算手续费最大、转账付款配置、手动转账、转账场景信息字段说明、代付配置、代付手续费、结算通知、登录提醒、结算通知模板ID、结算通知模板参数、结算账号、结算金额、结算时间、登录通知模板ID、登录通知模板参数、登录帐号、登录时间、登录平台、转账金额、转账时间、保存配置、复制、绑定状态、开通地址、新增进件页面公告、启用状态、修改绑定模板ID、下载代理API源码、测试连通性、确认修改、测试Redis连通性、结算规则、转账付款 |
| admin/system/cron · 计划任务 | index-CQrUK7ri.js | operation | 确认修改、取消、刷新任务列表、编辑执行参数、复制 |
| admin/system/anounce · 公告配置 | index-DdUbXxev.js | resource / announcements | 需检查导入组件；不推断无操作 |
| admin/system/admin-user · 管理员列表 | index-XE51Re1u.js | resource / admin-users | 编辑管理员、添加管理员、取消、保存修改、添加、取消全选、重置登录密码、登录密码、重置支付密码、关闭、搜索字段、搜索内容、添加时间、编辑 |
| admin/system/log · 登录日志 | index-CABO95hu.js | operation | 搜索字段、搜索内容 |
| admin/system/runtime-log · 运行日志 | index-EblIiRjx.js | operation | 取消、确认清除 |
| admin/system/clean · 数据清理 | index-DAJcazKr.js | operation | 需检查导入组件；不推断无操作 |
| admin/system/update · 系统更新 | index-3p1ouazc.js | operation | 需检查导入组件；不推断无操作 |
| admin/system/change-password · 修改密码 | index-7OFMkBOP.js | account | 取消、修改管理员密码、确认新密码、修改支付密码、重置、关闭、复制密钥 |
| merchant/dashboard · 仪表盘 | index-l9QPFu6k.js | dashboard | 结算管理 |
| merchant/account/api-info · API 信息 | index-CgFnG600.js | account | 需检查导入组件；不推断无操作 |
| merchant/account/settings · 账户设置 | index-fMgyEUZ4.js | account | 关闭确认、取消、解绑确认、绑定邮箱、修改、登录密码、登录日志、重置、关闭、代付API接口、保存设置 、结算通知、登录通知、复制、绑定状态、通过聚合收款码收款时的语音播报开关、解绑、登录方式、修改登录密码、确认密码、复制密钥、修改邮箱、修改手机号码 |
| merchant/account/certificate · 实名认证 | index-D_Mjs5yy.js | unavailable（用户指定暂缓） | 提交认证、查看认证信息 |
| merchant/account/recharge · 余额充值 | index-DTbArJ8N.js | commerce | 充值金额 |
| merchant/account/groupbuy · 购买会员 | index-Dx_Ne5Vo.js | commerce / groups | 需检查导入组件；不推断无操作 |
| merchant/account/deposit · 保证金 | index-BRVOjsJa.js | commerce | 充值保证金、充值金额 |
| merchant/account/internal-transfer · 站内转账 | index-dO26ItYn.js | workflow | 提交时间、新增转账、新增站内转账、取消、提交、转账金额 |
| merchant/trade/order · 订单管理 | index-Dk-66xD5.js | orders | 搜索字段、搜索内容、关闭、退款时间、重新通知(异步)、重新通知(同步)、查看子订单、结算状态、重新通知、手动补单、批量操作完成、批量退款中止、导出 、查看详情、退款、退款记录、取消、刷新间隔、批量退款确认、退款确认、确认退款、退款金额、登录密码 |
| merchant/trade/refund · 退款记录 | index-KNIUL4x0.js | refunds | 搜索字段、退款单号、搜索内容、退款中、退款金额、退款统计、关闭、退款总笔数、退款总金额 |
| merchant/trade/daily-stat · 每日交易统计 | index-B5ldFjlS.js | report | 退款金额 |
| merchant/trade/province-stat · 地区数据统计 | index-DhcBuQBV.js | report | 需检查导入组件；不推断无操作 |
| merchant/settle/list · 结算记录 | index-DLnTxwvH.js | workflow | 搜索、结算账号/姓名、结算方式、结算账号、结算金额/实际到账、手动、结算统计、复制链接、关闭 |
| merchant/settle/settings · 结算设置 | index-DHh5rhKO.js | resource / settlement-settings | 需检查导入组件；不推断无操作 |
| merchant/settle/apply · 申请提现 | index-DsthpoT6.js | workflow | 需检查导入组件；不推断无操作 |
| merchant/transfer · 代付管理 | index-Cn-r1JA8.js | transfers | 查询结果、转账付款、批量转账、转账金额、转账标题、转账备注、登录密码、确认创建、关闭、导入Excel、提交转账、提交时间、代付功能已关闭、搜索字段、搜索内容、查询完成、新增付款、确认收款、复制、查询状态、复制链接 |
| merchant/record · 资金明细 | index-ChSQvhrR.js | ledger | 搜索字段、搜索内容、代付、代付退回、关闭 |
| merchant/domain · 授权域名 | index-llNNw-18.js | resource / domains | 拒绝、添加时间、添加域名、审核中、删除、添加授权支付域名、取消、确认 |
| merchant/complain · 交易投诉 | index-BSWogy41.js | resource / complaints | 删除、上传中...、添加图片、查询中...、刷新、关闭、退款、查看订单、取消、通过其他方式退款、上传图片、拒绝退款、拒绝原因、退款确认、退款金额、登录密码、搜索字段、搜索内容、导出、详情 |
| merchant/applyments/merchant · 进件商户管理 | index-iwaK_B1G.js | resource / onboarding-applications | 搜索类型、搜索内容、审核中、修改审核中、查询完成、新增商户进件 、修改、详情、查询订单、删除、测试、测试支付、取消、发起测试 |
| merchant/applyments/create · 新增商户进件 | index-DuKpkIHz.js | resource / onboarding-applications | 添加已进件商户、关闭、提交、手动添加已进件商户 |
| merchant/applyments/mchrisk · 商户管控记录 | index-Bh5aco2B.js | resource / merchant-risk | 搜索字段、搜索内容 |
| merchant/applyments/form · 商户进件表单 | index-VU6rHS2G.js | resource / onboarding-applications | 需检查导入组件；不推断无操作 |
| merchant/profitsharing/receiver · 分账规则 | index-CCRqtej7.js | channel-sharing | 搜索、新增、删除、新增分账规则、修改分账规则、关闭、保存 |
| merchant/profitsharing/order · 分账记录 | index-B3PsZvos.js | channel-sharing | 提交分账、取消分账、搜索字段、搜索内容、导出 、查询结果、取消 |
| merchant/subchannel · 支付通道 | index-WhNAEuqO.js | resource / subchannels | 搜索、测试、取消、保存、设置通道权重、关闭、复制链接、测试支付、发起测试 |
| merchant/onecode · 聚合收款 | index-DVR_b5pg.js | collection-generator / collection-codes | 需检查导入组件；不推断无操作 |
| merchant/invite · 邀请返现 | index-Ch8Og0zy.js | referral | 需检查导入组件；不推断无操作 |

## 当前复核约束

- 表内动作是静态候选，必须再核对按钮的状态/权限条件和真实处理方法；同义文案不自动判断缺失。
- 通用资源页的详情/编辑/删除不代表原商户/通道/设置的所有专用动作已实现。
- 本表绑定当前工作源码；实际部署、测试与剩余差异以对应验收记录和全页面整改表为准，静态来源不冒充部署后验收。
- 其它业务插件按用户指定暂缓，其余页面不能因为缺少插件而自动排除。
