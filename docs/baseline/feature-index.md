# 完整菜单与功能验收索引

由 `docs/tools/inventory.mjs` 从当前源码生成；这些条目均为已定位，未进行实现和运行验收。

菜单之外的登录、授权、弹窗、任务、插件动态功能必须联合 routes.csv 和总体设计验收。路由中的 operation 等分发动作还需要继续拆分子操作。

## 管理后台页面

| ID | 菜单层级 | 完整路径 | 隐藏 | 角色 |
|---|---|---|---|---|
| ADMIN-Dashboard | 仪表盘 | /dashboard | 否 | R_SUPER;R_ADMIN |
| ADMIN-OrderList | 交易管理 / 订单管理 | /trade/order | 否 | R_SUPER;R_ADMIN |
| ADMIN-RefundList | 交易管理 / 退款记录 | /trade/refund | 否 | R_SUPER;R_ADMIN |
| ADMIN-BlackList | 交易管理 / 黑名单管理 | /trade/blacklist | 否 | R_SUPER;R_ADMIN |
| ADMIN-DailyStat | 交易管理 / 交易统计 / 每日交易统计 | /trade/stat/daily | 否 | R_SUPER;R_ADMIN |
| ADMIN-UserTradeStat | 交易管理 / 交易统计 / 商户交易统计 | /trade/stat/user | 否 | R_SUPER;R_ADMIN |
| ADMIN-ChannelStat | 交易管理 / 交易统计 / 通道数据统计 | /trade/stat/channel | 否 | R_SUPER;R_ADMIN |
| ADMIN-ProvinceStat | 交易管理 / 交易统计 / 地区数据统计 | /trade/stat/province | 否 | R_SUPER;R_ADMIN |
| ADMIN-BuyerStat | 交易管理 / 交易统计 / 支付用户统计 | /trade/stat/buyer | 否 | R_SUPER;R_ADMIN |
| ADMIN-PsReceiver | 分账管理 / 分账规则 | /profitsharing/receiver | 否 | R_SUPER;R_ADMIN |
| ADMIN-PsOrder | 分账管理 / 分账记录 | /profitsharing/order | 否 | R_SUPER;R_ADMIN |
| ADMIN-PsInternal | 分账管理 / 站内分账规则 | /profitsharing/internal | 否 | R_SUPER;R_ADMIN |
| ADMIN-TransferList | 转账付款 / 付款记录 | /transfer/list | 否 | R_SUPER;R_ADMIN |
| ADMIN-TransferStat | 转账付款 / 付款账号统计 | /transfer/stat | 否 | R_SUPER;R_ADMIN |
| ADMIN-TransferChannelStat | 转账付款 / 通道数据统计 | /transfer/channel-stat | 否 | R_SUPER;R_ADMIN |
| ADMIN-UserTransferStat | 转账付款 / 商户代付统计 | /transfer/user-stat | 否 | R_SUPER;R_ADMIN |
| ADMIN-SettleList | 结算管理 / 结算管理 | /settle/list | 否 | R_SUPER;R_ADMIN |
| ADMIN-BatchSettle | 结算管理 / 批量结算 | /settle/batch | 否 | R_SUPER;R_ADMIN |
| ADMIN-MerchantList | 商户管理 / 商户列表 | /merchant/merchant-list | 否 | R_SUPER;R_ADMIN |
| ADMIN-GroupList | 商户管理 / 用户组设置 | /merchant/group-list | 否 | R_SUPER;R_ADMIN |
| ADMIN-GroupBuy | 商户管理 / 用户组购买 | /merchant/group-buy | 否 | R_SUPER;R_ADMIN |
| ADMIN-Record | 商户管理 / 资金明细 | /merchant/record | 否 | R_SUPER;R_ADMIN |
| ADMIN-InternalTransfer | 商户管理 / 站内转账 | /merchant/internal-transfer | 否 | R_SUPER;R_ADMIN |
| ADMIN-Domain | 商户管理 / 授权域名 | /merchant/domain | 否 | R_SUPER;R_ADMIN |
| ADMIN-InviteCode | 商户管理 / 邀请码管理 | /merchant/invite-code | 否 | R_SUPER;R_ADMIN |
| ADMIN-PayChannel | 支付接口 / 支付通道 | /payapi/pay-channel | 否 | R_SUPER;R_ADMIN |
| ADMIN-PayType | 支付接口 / 支付方式 | /payapi/pay-type | 否 | R_SUPER;R_ADMIN |
| ADMIN-PayRoll | 支付接口 / 支付通道轮询 | /payapi/pay-roll | 否 | R_SUPER;R_ADMIN |
| ADMIN-PayWeixin | 支付接口 / 公众号小程序 | /payapi/pay-weixin | 否 | R_SUPER;R_ADMIN |
| ADMIN-ApplyChannel | 进件管理 / 进件渠道配置 | /applyments/channel | 否 | R_SUPER;R_ADMIN |
| ADMIN-ApplyMerchant | 进件管理 / 进件商户管理 | /applyments/merchant | 否 | R_SUPER;R_ADMIN |
| ADMIN-SubChannelManage | 进件管理 / 子通道管理 | /applyments/subchannel | 否 | R_SUPER;R_ADMIN |
| ADMIN-MchRisk | 进件管理 / 商户管控记录 | /applyments/mchrisk | 否 | R_SUPER;R_ADMIN |
| ADMIN-ApplyForm | 进件管理 / 商户进件表单 | /applyments/form | 是 | R_SUPER;R_ADMIN |
| ADMIN-ComplainList | 交易投诉 / 投诉记录 | /complain/list | 否 | R_SUPER;R_ADMIN |
| ADMIN-ComplainCharts | 交易投诉 / 投诉统计 | /complain/charts | 否 | R_SUPER;R_ADMIN |
| ADMIN-ComplainTask | 交易投诉 / 同步任务 | /complain/task | 否 | R_SUPER;R_ADMIN |
| ADMIN-Risk | 其他功能 / 风控记录 | /other/risk | 否 | R_SUPER;R_ADMIN |
| ADMIN-GetToken | 其他功能 / 获取用户标识 | /other/get-token | 否 | R_SUPER;R_ADMIN |
| ADMIN-OnecodeStyle | 其他功能 / 聚合收款码样式 | /other/onecode | 否 | R_SUPER;R_ADMIN |
| ADMIN-OnecodeStyleEdit | 其他功能 / 编辑收款码样式 | /other/onecode/edit | 是 | R_SUPER;R_ADMIN |
| ADMIN-AppStore | 应用管理 / 应用市场 | /app/app-store | 否 | R_SUPER |
| ADMIN-PluginList | 应用管理 / 本地插件 | /app/plugin-list | 否 | R_SUPER |
| ADMIN-TemplateConfig | 应用管理 / 模板配置 | /app/template | 否 | R_SUPER |
| ADMIN-AddonConfig | 应用管理 / 扩展配置 | /app/addon | 否 | R_SUPER |
| ADMIN-SystemSettings | 系统管理 / 系统设置 | /system/settings | 否 | R_SUPER |
| ADMIN-SystemCron | 系统管理 / 计划任务 | /system/cron | 否 | R_SUPER |
| ADMIN-Anounce | 系统管理 / 公告配置 | /system/anounce | 否 | R_SUPER;R_ADMIN |
| ADMIN-AdminUser | 系统管理 / 管理员列表 | /system/admin-user | 否 | R_SUPER |
| ADMIN-SystemLog | 系统管理 / 登录日志 | /system/log | 否 | R_SUPER;R_ADMIN |
| ADMIN-RuntimeLog | 系统管理 / 运行日志 | /system/runtime-log | 否 | R_SUPER |
| ADMIN-SystemClean | 系统管理 / 数据清理 | /system/clean | 否 | R_SUPER |
| ADMIN-SystemUpdate | 系统管理 / 系统更新 | /system/update | 否 | R_SUPER |
| ADMIN-ChangePassword | 修改密码 | /system/change-password | 是 | 依后端身份/配置 |

## 用户后台页面

| ID | 菜单层级 | 完整路径 | 隐藏 | 角色 |
|---|---|---|---|---|
| USER-Dashboard | 仪表盘 | /dashboard | 否 | 依后端身份/配置 |
| USER-ApiInfo | 账户中心 / API 信息 | /account/api-info | 否 | 依后端身份/配置 |
| USER-AccountSettings | 账户中心 / 账户设置 | /account/settings | 否 | 依后端身份/配置 |
| USER-Certificate | 账户中心 / 实名认证 | /account/certificate | 否 | 依后端身份/配置 |
| USER-Recharge | 账户中心 / 余额充值 | /account/recharge | 否 | 依后端身份/配置 |
| USER-GroupBuy | 账户中心 / 购买会员 | /account/groupbuy | 否 | 依后端身份/配置 |
| USER-Deposit | 账户中心 / 保证金 | /account/deposit | 否 | 依后端身份/配置 |
| USER-InternalTransfer | 账户中心 / 站内转账 | /account/internal-transfer | 否 | 依后端身份/配置 |
| USER-OrderList | 交易管理 / 订单管理 | /trade/order | 否 | 依后端身份/配置 |
| USER-RefundList | 交易管理 / 退款记录 | /trade/refund | 否 | 依后端身份/配置 |
| USER-DailyStat | 交易管理 / 每日交易统计 | /trade/daily-stat | 否 | 依后端身份/配置 |
| USER-ProvinceStat | 交易管理 / 地区数据统计 | /trade/province-stat | 否 | 依后端身份/配置 |
| USER-SettleList | 结算管理 / 结算记录 | /settle/list | 否 | 依后端身份/配置 |
| USER-SettleSettings | 结算管理 / 结算设置 | /settle/settings | 否 | 依后端身份/配置 |
| USER-SettleApply | 结算管理 / 申请提现 | /settle/apply | 否 | 依后端身份/配置 |
| USER-Transfer | 代付管理 | /transfer | 否 | 依后端身份/配置 |
| USER-Record | 资金明细 | /record | 否 | 依后端身份/配置 |
| USER-Domain | 授权域名 | /domain | 否 | 依后端身份/配置 |
| USER-Complain | 交易投诉 | /complain | 否 | 依后端身份/配置 |
| USER-ApplyMerchant | 商户进件 / 进件商户管理 | /applyments/merchant | 否 | 依后端身份/配置 |
| USER-ApplyCreate | 商户进件 / 新增商户进件 | /applyments/create | 否 | 依后端身份/配置 |
| USER-MchRisk | 商户进件 / 商户管控记录 | /applyments/mchrisk | 否 | 依后端身份/配置 |
| USER-ApplyForm | 商户进件 / 商户进件表单 | /applyments/form | 是 | 依后端身份/配置 |
| USER-PsReceiver | 订单分账 / 分账规则 | /profitsharing/receiver | 否 | 依后端身份/配置 |
| USER-PsOrder | 订单分账 / 分账记录 | /profitsharing/order | 否 | 依后端身份/配置 |
| USER-SubChannel | 支付通道 | /subchannel | 否 | 依后端身份/配置 |
| USER-Onecode | 聚合收款 | /onecode | 否 | 依后端身份/配置 |
| USER-Invite | 邀请返现 | /invite | 否 | 依后端身份/配置 |

## admin 路由功能分组

动作名称是原代码证据；具体请求、权限和业务分支仍需契约样本。每个动作按 routes.csv 中的ID追踪。

| 控制器 | 动作 |
|---|---|
| admin.Auth | login、totp、checkLogin、getPublicKey、logout |
| admin.Index | getDashboardData |
| admin.Common | getUserInfo、getMenuList、getSystemInfo、changeAdminPassword、changePayPassword、getTotpStatus、totpGenerate、totpBind、totpClose、clearCache |
| admin.Settings | set、anounceList、addAnounce、editAnounce、setAnounce、delAnounce、iptype、testproxy、generate_wxa_link、getTokenOptions、getSettings、uploadLogo、uploadIcon、getSettingsOptions、testMail、proxyapi、smsChannelList、smsChannelInfo、saveSmsChannelConfig、getSmsRateConfig、mailChannelList、mailChannelInfo、saveMailChannelConfig、getMailRateConfig、getAdvancedSettings、saveAdminPath、saveCacheConfig、testRedis、saveAsyncConfig |
| admin.Cron | info、saveConfig、saveTaskParams、setTaskStatus、runTaskNow |
| admin.Template | templateList、setTemplate、templateConfig、saveTemplateConfig、uploadTemplateImage |
| admin.Addon | addonList、addonConfig、saveAddonConfig、addonOperation |
| admin.Applyments | getApplyTypeList、channelList、getChannel、saveChannel、delChannel、setChannel、merchantList、subchannel_list、setChannelStatus、getFormData、submit、getContractUrl、queryContractStatus、query、image、uploadImage、uploadFile、checkBankCard、getBankList、getBankBranchList、setPayStatus、removeMerchant、delMerchant、changeUid、changeStatus、getalipayauth、setRemark、merchantPayStat、merchant_op、addMerchant、form、file |
| admin.MchRisk | riskList、export、getRiskChannels、setNotifyUrl、mchrisk_op |
| admin.BlackList | list、export、add、del、batchdel、buyerStat、buyerPayMoney |
| admin.Complain | list、export、info、getChannels、refreshNewList、setNotifyUrl、setAutoTask、taskList、setTaskStatus、saveTask、saveTasks、delTask、delTasks、delComplain、operation、batch_reply、batch_complete、uploadImage、feedbackSubmit、replySubmit、supplementSubmit、refundProgressSubmit、complete、wximg、charts_list、chartsSummary |
| admin.Group | getGroup、delGroup、saveGroup、saveGroupPrice、saveGroupDesc、groupList、groupFullList、groupOptions、groupOrderStat |
| admin.InviteCode | inviteCodeList、addInviteCode、delInviteCode、clearInviteCode |
| admin.Order | payTypeList、list、export、statistics、riskList、setStatus、detail、editApiTradeNo、subOrders、operation、getmoney、refund、apirefund、freeze、unfreeze、notify、queryOrder、fillorder、alipaydSettle、alipayPreAuthPay、alipayUnfreeze、alipayRedPacketTansfer、refundList、refundStatistics |
| admin.OrderStat | dailyStat、channelStat、provinceStats |
| admin.PayApi | payTypeList、channelList、getPayType、setPayType、delPayType、savePayType、getPlugin、getPluginsByType、getChannel、getChannelsByPlugin、getSubChannels、setChannel、delChannel、batchChannel、batchBindWeixin、saveChannel、saveChannelAdvanced、getChannelInfo、saveChannelInfo、uploadCert、downloadCert、rollList、getRoll、setRoll、delRoll、saveRoll、rollInfo、saveRollInfo、getChannelMoney、getSubChannelMoney、getTypeMoney、getSuccessRate、channelPayStat、testpay、testSubmit、pluginPage |
| admin.ProfitSharing | receiverList、get_receiver、add_receiver、edit_receiver、del_receiver、set_receiver、channelList、orderList、submit、query、unfreeze、return_、editmoney、operation、statistics |
| admin.PsInternal | list、get、add、edit、del、setStatus |
| admin.Settle | list、statistics、setStatus、opslist、result、setResult、saveAccount、paypwd_check、paypwd_input、paypwd_reset、batchList、create_batch、deleteBatch、complete_batch、batchTransferList、transferChannels、transfer、export |
| admin.Transfer | list、detail、statistics、result、query、setStatus、delete、refund、cancel、proof、operation、getChannels、copyInfo、submit、redSubmit、queryBalance、batchSubmit、accountStat、channelStat、export |
| admin.User | userList、export、getUser、addUser、editUser、edit_keytype、resetKey、createRsaPair、editUserChannelInfo、editOrdernameRules、delUser、setUser、setUserGroup、resetUser、certInfo、recharge、checkuid、withdraw、sso |
| admin.InternalTransfer | list、add、audit |
| admin.UserRecord | recordList、record_stats、delRecord、export |
| admin.UserSubChannel | manageList、subChannelList、getChannels、getSubChannel、setSubChannel、delSubChannel、saveSubChannel、subChannelInfo、saveSubChannelInfo |
| admin.UserStat | userPayStat、userTransferStat、merchantOrderChart、merchantOrderStat |
| admin.Weixin | weixinList、getWeixin、delWeixin、saveWeixin、testweixin |
| admin.Onecode | styleList、getStyle、saveStyle、setStatus、delStyle、uploadImage |
| admin.Domain | domainList、addDomain、setDomainStatus、delDomain、operation |
| admin.System | adminUserList、getAdminUser、addAdminUser、editAdminUser、delAdminUser、setAdminUserStatus、getFullMenuList、logList、runtimeLog、cleanRuntimeLog、cleanData |
| admin.Update | checkUpdate、doUpdate |
| admin.PluginManage | pluginList、refreshPlugins、deletePlugin |
| admin.AppStore | getAccountInfo、oauthLogin、unbindAccount、getAppList、buyApp、recharge、downloadApp、deleteTemplate、oauthCallback |

## user 路由功能分组

动作名称是原代码证据；具体请求、权限和业务分支仍需契约样本。每个动作按 routes.csv 中的ID追踪。

| 控制器 | 动作 |
|---|---|
| user.Common | captcha、getMenuList |
| user.TestPay | init、success、submit、index |
| user.Auth | sysinfo、checkLogin、login、totp、wxaopenid、wxalogin、connect、wxLogin、alipayLogin、sendcode、reg、findpwd、logout |
| user.Applyments | channelList、merchantList、addMerchant、delMerchant、setPayStatus、query、image、subchannel_list、mySubchannelList、setChannelStatus、getalipayauth、getSubChannelMoney、merchantPayStat、testpay、testSubmit、getRollType、setRollType、setSubchannelWeight、setSubchannelSetting、getFormData、submit、getContractUrl、queryContractStatus、uploadImage、uploadFile、checkBankCard、getBankList、getBankBranchList、riskList、form、file |
| user.Certificate | info、submit、geturl、reset、query、alipayCert、alipayCertOk |
| user.Invite | inviteInfo、inviteList |
| user.Group | grouplist、groupinfo、groupbuy |
| user.Domain | domainList、addDomain、delDomain |
| user.Complain | complainSummary、list、export、info、uploadImage、feedbackSubmit、replySubmit、supplementSubmit、refundProgressSubmit、complete、wximg |
| user.ProfitSharing | subchannelList、receiverList、get_receiver、add_receiver、edit_receiver、del_receiver、set_receiver、orderList、orderExport、submit、query、unfreeze、return_、statistics |
| user.Recharge | info、submit |
| user.Deposit | info、recharge、withdraw |
| user.Order | list、export、statistics、detail、subOrders、queryOrder、notify、printOrder、refund_query、refund_submit、payTypeList、refundList、refundStatistics |
| user.OrderStat | dailyStat、provinceStats |
| user.Settle | list、statistics、result、withdrawInfo、withdrawSubmit |
| user.Transfer | list、statistics、result、query、queryBalance、detail、proof、copyInfo、submit、redSubmit、batchSubmit |
| user.InternalTransfer | info、list、add |
| user.Record | list、stats |
| user.Account | userinfo、accountSettings、apiinfo、checkBind、onecodeInfo、sendcode、verifycode、editSettle、editRemainMoney、completeinfo、editInfo、editKeyType、editEventSubscribe、editVoice、editPrint、editChannelInfo、editMsgConfig、editQqbotConfig、editBind、unbindConnect、resetKey、createRsaPair、editPwd、editCodename、getTotpStatus、totpGenerate、totpBind、totpClose、logList |
| user.Index | dashboard |
| user.OAuth | connect、douyin、alipay、wechat、telegram |
| user.Openid | wechat、alipay、douyin、qrcode、getopenid |
| user.Scanpay | jsconfig、submit |

## api 路由功能分组

动作名称是原代码证据；具体请求、权限和业务分支仍需契约样本。每个动作按 routes.csv 中的ID追踪。

| 控制器 | 动作 |
|---|---|
| api.Pay | submit、create、query、refund、refundquery、close |
| api.Transfer | submit、query、proof、balance |
| api.Merchant | info、orders |
| api.Complain | image、list、detail、upload、feedback、reply、refundprogress、complete、supplement |
| api.Obsolete | index |

## app 路由功能分组

动作名称是原代码证据；具体请求、权限和业务分支仍需契约样本。每个动作按 routes.csv 中的ID追踪。

| 控制器 | 动作 |
|---|---|
| index | index、index、test、doc |
| Install | index、checkEnv、checkConfig、doInstall |
| payment.Ajax | captchaVerify、checkStatus |
| payment.Submit | index |
| payment.Voice | index |
| payment.Cashier | index |
| transfer.Weixin | index |
| transfer.RedEnvelope | index、receive |
| payment.Gold | index |
| payment.Qqbot | webhook、webhook |
| qrpay.Index | index、ajax、success、index |
| closure | /payok、/payerr、/wxtoqq、/agreement、/doc// |
| payment.Gateway | index |
| Cron | plugin、index |
| applet.Cashier | merchant、create |

## Release 支付插件入口

下列60个入口全部保留在盘点范围，不据此推定Pro插件覆盖；逐个补充能力与差异测试。

adapay、alipay、alipaycode、alipayd、alipayg、alipayhk、alipayrp、alipaysl、allinpay、baofu、chinaums、dinpay、douyinpay、duolabao、easypay、epay、epayn、fubei、fuiou、fuiou2、fuiou3、haipay、heepay、helipay、hlpay、hnapay、huifu、huolian、jdpay、jeepay、jlpay、kuaiqian、lakala、lakalamoss、leshua、ltzf、passpay、paypal、qqpay、sandpay、shengpay、stripe、suixingpay、swiftpass、swiftpass2、umfpay、unionpay、wxpay、wxpayn、wxpayng、wxpaynp、wxpaysl、xsy、xunhupay、yeepay、yinyingtong、ysepay、yseqt、zhangyishou、zyu
