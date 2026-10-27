<?php

use think\facade\Route;
use think\middleware\SessionInit;
use app\middleware\RefererCheck;
use app\middleware\AuthAdmin;
use app\middleware\ViewOutput;

$adminPath = env('admin_path', 'admin');

Route::group($adminPath, function () {
    Route::group('auth', function () {
        Route::post('login', 'admin.Auth/login');
        Route::post('totp', 'admin.Auth/totp');
        Route::get('checkLogin', 'admin.Auth/checkLogin');
        Route::get('getPublicKey', 'admin.Auth/getPublicKey');
        Route::post('logout', 'admin.Auth/logout');
    });
})->middleware([SessionInit::class, RefererCheck::class]);

Route::group($adminPath, function () {
    //控制台
    Route::group('index', function () {
        Route::get('getDashboardData', 'admin.Index/getDashboardData');
    });

    //公共与密码修改
    Route::group('common', function () {
        Route::get('getUserInfo', 'admin.Common/getUserInfo');
        Route::get('getMenuList', 'admin.Common/getMenuList');
        Route::get('getSystemInfo', 'admin.Common/getSystemInfo');
        Route::post('changeAdminPassword', 'admin.Common/changeAdminPassword');
        Route::post('changePayPassword', 'admin.Common/changePayPassword');
        Route::get('getTotpStatus', 'admin.Common/getTotpStatus');
        Route::post('totpGenerate', 'admin.Common/totpGenerate');
        Route::post('totpBind', 'admin.Common/totpBind');
        Route::post('totpClose', 'admin.Common/totpClose');
        Route::post('clearCache', 'admin.Common/clearCache');
    });

    //系统设置
    Route::group('settings', function () {
        Route::post('set', 'admin.Settings/set');
        Route::get('anounceList', 'admin.Settings/anounceList');
        Route::post('addAnounce', 'admin.Settings/addAnounce');
        Route::post('editAnounce', 'admin.Settings/editAnounce');
        Route::get('setAnounce', 'admin.Settings/setAnounce');
        Route::get('delAnounce', 'admin.Settings/delAnounce');
        Route::get('iptype', 'admin.Settings/iptype');
        Route::post('testproxy', 'admin.Settings/testproxy');
        Route::post('generate_wxa_link', 'admin.Settings/generate_wxa_link');
        Route::get('getTokenOptions', 'admin.Settings/getTokenOptions');
        Route::get('getSettings', 'admin.Settings/getSettings');
        Route::post('uploadLogo', 'admin.Settings/uploadLogo');
        Route::post('uploadIcon', 'admin.Settings/uploadIcon');
        Route::get('getSettingsOptions', 'admin.Settings/getSettingsOptions');
        Route::post('testMail', 'admin.Settings/testMail');
        Route::get('proxyapi', 'admin.Settings/proxyapi');
        Route::get('smsChannelList', 'admin.Settings/smsChannelList');
        Route::get('smsChannelInfo', 'admin.Settings/smsChannelInfo');
        Route::post('saveSmsChannelConfig', 'admin.Settings/saveSmsChannelConfig');
        Route::get('getSmsRateConfig', 'admin.Settings/getSmsRateConfig');
        Route::get('mailChannelList', 'admin.Settings/mailChannelList');
        Route::get('mailChannelInfo', 'admin.Settings/mailChannelInfo');
        Route::post('saveMailChannelConfig', 'admin.Settings/saveMailChannelConfig');
        Route::get('getMailRateConfig', 'admin.Settings/getMailRateConfig');
        Route::get('getAdvancedSettings', 'admin.Settings/getAdvancedSettings');
        Route::post('saveAdminPath', 'admin.Settings/saveAdminPath');
        Route::post('saveCacheConfig', 'admin.Settings/saveCacheConfig');
        Route::post('testRedis', 'admin.Settings/testRedis');
        Route::post('saveAsyncConfig', 'admin.Settings/saveAsyncConfig');
    });

    //计划任务
    Route::group('cron', function () {
        Route::get('info', 'admin.Cron/info');
        Route::post('saveConfig', 'admin.Cron/saveConfig');
        Route::post('saveTaskParams', 'admin.Cron/saveTaskParams');
        Route::post('setTaskStatus', 'admin.Cron/setTaskStatus');
        Route::post('runTaskNow', 'admin.Cron/runTaskNow');
    });

    //模板
    Route::group('template', function () {
        Route::get('templateList', 'admin.Template/templateList');
        Route::post('setTemplate', 'admin.Template/setTemplate');
        Route::get('templateConfig', 'admin.Template/templateConfig');
        Route::post('saveTemplateConfig', 'admin.Template/saveTemplateConfig');
        Route::post('uploadTemplateImage', 'admin.Template/uploadTemplateImage');
    });

    //功能扩展
    Route::group('addon', function () {
        Route::get('addonList', 'admin.Addon/addonList');
        Route::get('addonConfig', 'admin.Addon/addonConfig');
        Route::post('saveAddonConfig', 'admin.Addon/saveAddonConfig');
        Route::post('addonOperation', 'admin.Addon/addonOperation');
    });

    //进件管理
    Route::group('applyments', function () {
        Route::get('getApplyTypeList', 'admin.Applyments/getApplyTypeList');
        Route::post('channelList', 'admin.Applyments/channelList');
        Route::get('getChannel', 'admin.Applyments/getChannel');
        Route::post('saveChannel', 'admin.Applyments/saveChannel');
        Route::post('delChannel', 'admin.Applyments/delChannel');
        Route::post('setChannel', 'admin.Applyments/setChannel');
        Route::post('merchantList', 'admin.Applyments/merchantList');
        Route::get('subchannel_list', 'admin.Applyments/subchannel_list');
        Route::post('setChannelStatus', 'admin.Applyments/setChannelStatus');
        Route::post('getFormData', 'admin.Applyments/getFormData');
        Route::post('submit', 'admin.Applyments/submit');
        Route::post('getContractUrl', 'admin.Applyments/getContractUrl');
        Route::post('queryContractStatus', 'admin.Applyments/queryContractStatus');
        Route::post('query', 'admin.Applyments/query');
        Route::get('image', 'admin.Applyments/image');
        Route::post('uploadImage', 'admin.Applyments/uploadImage');
        Route::post('uploadFile', 'admin.Applyments/uploadFile');
        Route::post('checkBankCard', 'admin.Applyments/checkBankCard');
        Route::post('getBankList', 'admin.Applyments/getBankList');
        Route::post('getBankBranchList', 'admin.Applyments/getBankBranchList');
        Route::post('setPayStatus', 'admin.Applyments/setPayStatus');
        Route::post('removeMerchant', 'admin.Applyments/removeMerchant');
        Route::post('delMerchant', 'admin.Applyments/delMerchant');
        Route::post('changeUid', 'admin.Applyments/changeUid');
        Route::post('changeStatus', 'admin.Applyments/changeStatus');
        Route::get('getalipayauth', 'admin.Applyments/getalipayauth');
        Route::post('setRemark', 'admin.Applyments/setRemark');
        Route::post('merchantPayStat', 'admin.Applyments/merchantPayStat');
        Route::post('merchant_op', 'admin.Applyments/merchant_op');
        Route::post('addMerchant', 'admin.Applyments/addMerchant');
        Route::any('form', 'admin.Applyments/form')->middleware(ViewOutput::class);
        Route::get('file', 'admin.Applyments/file');
    });

    //商户管控记录
    Route::group('mchrisk', function () {
        Route::post('riskList', 'admin.MchRisk/riskList');
        Route::get('export', 'admin.MchRisk/export');
        Route::get('getRiskChannels', 'admin.MchRisk/getRiskChannels');
        Route::post('setNotifyUrl', 'admin.MchRisk/setNotifyUrl');
        Route::post('mchrisk_op', 'admin.MchRisk/mchrisk_op');
    });

    //黑名单管理
    Route::group('blacklist', function () {
        Route::post('list', 'admin.BlackList/list');
        Route::get('export', 'admin.BlackList/export');
        Route::post('add', 'admin.BlackList/add');
        Route::post('del', 'admin.BlackList/del');
        Route::post('batchdel', 'admin.BlackList/batchdel');
        Route::post('buyerStat', 'admin.BlackList/buyerStat');
        Route::post('buyerPayMoney', 'admin.BlackList/buyerPayMoney');
    });

    //投诉管理
    Route::group('complain', function () {
        Route::post('list', 'admin.Complain/list');
        Route::get('export', 'admin.Complain/export');
        Route::post('info', 'admin.Complain/info');
        Route::get('getChannels', 'admin.Complain/getChannels');
        Route::post('refreshNewList', 'admin.Complain/refreshNewList');
        Route::post('setNotifyUrl', 'admin.Complain/setNotifyUrl');
        Route::post('setAutoTask', 'admin.Complain/setAutoTask');
        Route::post('taskList', 'admin.Complain/taskList');
        Route::post('setTaskStatus', 'admin.Complain/setTaskStatus');
        Route::post('saveTask', 'admin.Complain/saveTask');
        Route::post('saveTasks', 'admin.Complain/saveTasks');
        Route::post('delTask', 'admin.Complain/delTask');
        Route::post('delTasks', 'admin.Complain/delTasks');
        Route::get('delComplain', 'admin.Complain/delComplain');
        Route::post('operation', 'admin.Complain/operation');
        Route::post('batch_reply', 'admin.Complain/batch_reply');
        Route::post('batch_complete', 'admin.Complain/batch_complete');
        Route::post('uploadImage', 'admin.Complain/uploadImage');
        Route::post('feedbackSubmit', 'admin.Complain/feedbackSubmit');
        Route::post('replySubmit', 'admin.Complain/replySubmit');
        Route::post('supplementSubmit', 'admin.Complain/supplementSubmit');
        Route::post('refundProgressSubmit', 'admin.Complain/refundProgressSubmit');
        Route::post('complete', 'admin.Complain/complete');
        Route::get('wximg', 'admin.Complain/wximg');
        Route::post('charts_list', 'admin.Complain/charts_list');
        Route::get('chartsSummary', 'admin.Complain/chartsSummary');
    });

    //用户组
    Route::group('group', function () {
        Route::get('getGroup', 'admin.Group/getGroup');
        Route::get('delGroup', 'admin.Group/delGroup');
        Route::post('saveGroup', 'admin.Group/saveGroup');
        Route::post('saveGroupPrice', 'admin.Group/saveGroupPrice');
        Route::post('saveGroupDesc', 'admin.Group/saveGroupDesc');
        Route::post('groupList', 'admin.Group/groupList');
        Route::post('groupFullList', 'admin.Group/groupFullList');
        Route::post('groupOptions', 'admin.Group/groupOptions');
        Route::post('groupOrderStat', 'admin.Group/groupOrderStat');
    });

    //邀请码管理
    Route::group('invitecode', function () {
        Route::post('inviteCodeList', 'admin.InviteCode/inviteCodeList');
        Route::post('addInviteCode', 'admin.InviteCode/addInviteCode');
        Route::post('delInviteCode', 'admin.InviteCode/delInviteCode');
        Route::post('clearInviteCode', 'admin.InviteCode/clearInviteCode');
    });

    //订单管理
    Route::group('order', function () {
        Route::get('payTypeList', 'admin.Order/payTypeList');
        Route::post('list', 'admin.Order/list');
        Route::get('export', 'admin.Order/export');
        Route::post('statistics', 'admin.Order/statistics');
        Route::post('riskList', 'admin.Order/riskList');
        Route::get('setStatus', 'admin.Order/setStatus');
        Route::get('detail', 'admin.Order/detail');
        Route::post('editApiTradeNo', 'admin.Order/editApiTradeNo');
        Route::get('subOrders', 'admin.Order/subOrders');
        Route::post('operation', 'admin.Order/operation');
        Route::post('getmoney', 'admin.Order/getmoney');
        Route::post('refund', 'admin.Order/refund');
        Route::post('apirefund', 'admin.Order/apirefund');
        Route::post('freeze', 'admin.Order/freeze');
        Route::post('unfreeze', 'admin.Order/unfreeze');
        Route::post('notify', 'admin.Order/notify');
        Route::post('queryOrder', 'admin.Order/queryOrder');
        Route::post('fillorder', 'admin.Order/fillorder');
        Route::post('alipaydSettle', 'admin.Order/alipaydSettle');
        Route::post('alipayPreAuthPay', 'admin.Order/alipayPreAuthPay');
        Route::post('alipayUnfreeze', 'admin.Order/alipayUnfreeze');
        Route::post('alipayRedPacketTansfer', 'admin.Order/alipayRedPacketTansfer');
        Route::post('refundList', 'admin.Order/refundList');
        Route::post('refundStatistics', 'admin.Order/refundStatistics');

        Route::post('dailyStat', 'admin.OrderStat/dailyStat');
        Route::post('channelStat', 'admin.OrderStat/channelStat');
        Route::post('provinceStats', 'admin.OrderStat/provinceStats'); 
    });

    //支付接口
    Route::group('payapi', function () {
        Route::get('payTypeList', 'admin.PayApi/payTypeList');
        Route::post('channelList', 'admin.PayApi/channelList');
        Route::get('getPayType', 'admin.PayApi/getPayType');
        Route::get('setPayType', 'admin.PayApi/setPayType');
        Route::get('delPayType', 'admin.PayApi/delPayType');
        Route::post('savePayType', 'admin.PayApi/savePayType');
        Route::get('getPlugin', 'admin.PayApi/getPlugin');
        Route::get('getPluginsByType', 'admin.PayApi/getPluginsByType');
        Route::get('getChannel', 'admin.PayApi/getChannel');
        Route::get('getChannelsByPlugin', 'admin.PayApi/getChannelsByPlugin');
        Route::get('getSubChannels', 'admin.PayApi/getSubChannels');
        Route::get('setChannel', 'admin.PayApi/setChannel');
        Route::get('delChannel', 'admin.PayApi/delChannel');
        Route::post('batchChannel', 'admin.PayApi/batchChannel');
        Route::post('batchBindWeixin', 'admin.PayApi/batchBindWeixin');
        Route::post('saveChannel', 'admin.PayApi/saveChannel');
        Route::post('saveChannelAdvanced', 'admin.PayApi/saveChannelAdvanced');
        Route::get('getChannelInfo', 'admin.PayApi/getChannelInfo');
        Route::post('saveChannelInfo', 'admin.PayApi/saveChannelInfo');
        Route::post('uploadCert', 'admin.PayApi/uploadCert');
        Route::get('downloadCert', 'admin.PayApi/downloadCert');
        Route::get('rollList', 'admin.PayApi/rollList');
        Route::get('getRoll', 'admin.PayApi/getRoll');
        Route::get('setRoll', 'admin.PayApi/setRoll');
        Route::get('delRoll', 'admin.PayApi/delRoll');
        Route::post('saveRoll', 'admin.PayApi/saveRoll');
        Route::get('rollInfo', 'admin.PayApi/rollInfo');
        Route::post('saveRollInfo', 'admin.PayApi/saveRollInfo');
        Route::get('getChannelMoney', 'admin.PayApi/getChannelMoney');
        Route::get('getSubChannelMoney', 'admin.PayApi/getSubChannelMoney');
        Route::get('getTypeMoney', 'admin.PayApi/getTypeMoney');
        Route::get('getSuccessRate', 'admin.PayApi/getSuccessRate');
        Route::post('channelPayStat', 'admin.PayApi/channelPayStat');
        Route::post('testpay', 'admin.PayApi/testpay');
        Route::get('testSubmit', 'admin.PayApi/testSubmit');
    });

    //分账管理
    Route::group('profitsharing', function () {
        Route::post('receiverList', 'admin.ProfitSharing/receiverList');
        Route::get('get_receiver', 'admin.ProfitSharing/get_receiver');
        Route::post('add_receiver', 'admin.ProfitSharing/add_receiver');
        Route::post('edit_receiver', 'admin.ProfitSharing/edit_receiver');
        Route::post('del_receiver', 'admin.ProfitSharing/del_receiver');
        Route::post('set_receiver', 'admin.ProfitSharing/set_receiver');
        Route::get('channelList', 'admin.ProfitSharing/channelList');
        Route::post('orderList', 'admin.ProfitSharing/orderList');
        Route::post('submit', 'admin.ProfitSharing/submit');
        Route::post('query', 'admin.ProfitSharing/query');
        Route::post('unfreeze', 'admin.ProfitSharing/unfreeze');
        Route::post('return', 'admin.ProfitSharing/return_');
        Route::post('editmoney', 'admin.ProfitSharing/editmoney');
        Route::post('operation', 'admin.ProfitSharing/operation');
        Route::post('statistics', 'admin.ProfitSharing/statistics');
        Route::post('internalList', 'admin.PsInternal/list');
        Route::get('internalGet', 'admin.PsInternal/get');
        Route::post('internalAdd', 'admin.PsInternal/add');
        Route::post('internalEdit', 'admin.PsInternal/edit');
        Route::post('internalDel', 'admin.PsInternal/del');
        Route::post('internalSet', 'admin.PsInternal/setStatus');
    });

    //结算管理
    Route::group('settle', function () {
        Route::post('list', 'admin.Settle/list');
        Route::post('statistics', 'admin.Settle/statistics');
        Route::get('setStatus', 'admin.Settle/setStatus');
        Route::post('opslist', 'admin.Settle/opslist');
        Route::post('result', 'admin.Settle/result');
        Route::post('setResult', 'admin.Settle/setResult');
        Route::post('saveAccount', 'admin.Settle/saveAccount');
        Route::get('paypwd_check', 'admin.Settle/paypwd_check');
        Route::post('paypwd_input', 'admin.Settle/paypwd_input');
        Route::get('paypwd_reset', 'admin.Settle/paypwd_reset');
        Route::get('batchList', 'admin.Settle/batchList');
        Route::get('create_batch', 'admin.Settle/create_batch');
        Route::post('deleteBatch', 'admin.Settle/deleteBatch');
        Route::post('complete_batch', 'admin.Settle/complete_batch');
        Route::post('batchTransferList', 'admin.Settle/batchTransferList');
        Route::get('transferChannels', 'admin.Settle/transferChannels');
        Route::post('transfer', 'admin.Settle/transfer');
        Route::get('export', 'admin.Settle/export');
    });

    //付款管理
    Route::group('transfer', function () {
        Route::post('list', 'admin.Transfer/list');
        Route::get('detail', 'admin.Transfer/detail');
        Route::post('statistics', 'admin.Transfer/statistics');
        Route::get('result', 'admin.Transfer/result');
        Route::get('query', 'admin.Transfer/query');
        Route::post('setStatus', 'admin.Transfer/setStatus');
        Route::post('delete', 'admin.Transfer/delete');
        Route::post('refund', 'admin.Transfer/refund');
        Route::post('cancel', 'admin.Transfer/cancel');
        Route::post('proof', 'admin.Transfer/proof');
        Route::post('operation', 'admin.Transfer/operation');
        Route::get('getChannels', 'admin.Transfer/getChannels');
        Route::get('copyInfo', 'admin.Transfer/copyInfo');
        Route::post('submit', 'admin.Transfer/submit');
        Route::post('redSubmit', 'admin.Transfer/redSubmit');
        Route::post('queryBalance', 'admin.Transfer/queryBalance');
        Route::post('batchSubmit', 'admin.Transfer/batchSubmit');
        Route::post('accountStat', 'admin.Transfer/accountStat');
        Route::post('channelStat', 'admin.Transfer/channelStat');
        Route::get('export', 'admin.Transfer/export');
    });

    //用户管理
    Route::group('user', function () {
        Route::post('userList', 'admin.User/userList');
        Route::get('export', 'admin.User/export');
        Route::post('getUser', 'admin.User/getUser');
        Route::post('addUser', 'admin.User/addUser');
        Route::post('editUser', 'admin.User/editUser');
        Route::post('edit_keytype', 'admin.User/edit_keytype');
        Route::post('resetKey', 'admin.User/resetKey');
        Route::post('createRsaPair', 'admin.User/createRsaPair');
        Route::post('editUserChannelInfo', 'admin.User/editUserChannelInfo');
        Route::post('editOrdernameRules', 'admin.User/editOrdernameRules');
        Route::post('delUser', 'admin.User/delUser');
        Route::post('setUser', 'admin.User/setUser');
        Route::post('setUserGroup', 'admin.User/setUserGroup');
        Route::get('resetUser', 'admin.User/resetUser');
        Route::get('certInfo', 'admin.User/certInfo');
        Route::post('recharge', 'admin.User/recharge');
        Route::get('checkuid', 'admin.User/checkuid');
        Route::post('withdraw', 'admin.User/withdraw');

        Route::post('internalTransferList', 'admin.InternalTransfer/list');
        Route::post('internalTransferAdd', 'admin.InternalTransfer/add');
        Route::post('internalTransferAudit', 'admin.InternalTransfer/audit');

        Route::post('recordList', 'admin.UserRecord/recordList');
        Route::post('record_stats', 'admin.UserRecord/record_stats');
        Route::get('delRecord', 'admin.UserRecord/delRecord');
        Route::get('recordExport', 'admin.UserRecord/export');

        Route::post('subChannelManageList', 'admin.UserSubChannel/manageList');
        Route::post('subChannelList', 'admin.UserSubChannel/subChannelList');
        Route::get('getChannels', 'admin.UserSubChannel/getChannels');
        Route::get('getSubChannel', 'admin.UserSubChannel/getSubChannel');
        Route::get('setSubChannel', 'admin.UserSubChannel/setSubChannel');
        Route::get('delSubChannel', 'admin.UserSubChannel/delSubChannel');
        Route::post('saveSubChannel', 'admin.UserSubChannel/saveSubChannel');
        Route::post('subChannelInfo', 'admin.UserSubChannel/subChannelInfo');
        Route::post('saveSubChannelInfo', 'admin.UserSubChannel/saveSubChannelInfo');

        Route::post('userPayStat', 'admin.UserStat/userPayStat');
        Route::post('userTransferStat', 'admin.UserStat/userTransferStat');
        Route::post('merchantOrderChart', 'admin.UserStat/merchantOrderChart');
        Route::post('merchantOrderStat', 'admin.UserStat/merchantOrderStat');
    });

    Route::group('weixin', function () {
        Route::get('weixinList', 'admin.Weixin/weixinList');
        Route::get('getWeixin', 'admin.Weixin/getWeixin');
        Route::get('delWeixin', 'admin.Weixin/delWeixin');
        Route::post('saveWeixin', 'admin.Weixin/saveWeixin');
        Route::post('testweixin', 'admin.Weixin/testweixin');
    });

    // 聚合收款码样式
    Route::group('onecode', function () {
        Route::get('styleList', 'admin.Onecode/styleList');
        Route::get('getStyle', 'admin.Onecode/getStyle');
        Route::post('saveStyle', 'admin.Onecode/saveStyle');
        Route::post('setStatus', 'admin.Onecode/setStatus');
        Route::post('delStyle', 'admin.Onecode/delStyle');
        Route::post('uploadImage', 'admin.Onecode/uploadImage');
    });

    // 授权域名
    Route::group('domain', function () {
        Route::post('domainList', 'admin.Domain/domainList');
        Route::post('addDomain', 'admin.Domain/addDomain');
        Route::post('setDomainStatus', 'admin.Domain/setDomainStatus');
        Route::post('delDomain', 'admin.Domain/delDomain');
        Route::post('operation', 'admin.Domain/operation');
    });

    //管理员管理
    Route::group('system', function () {
        Route::post('adminUserList', 'admin.System/adminUserList');
        Route::get('getAdminUser', 'admin.System/getAdminUser');
        Route::post('addAdminUser', 'admin.System/addAdminUser');
        Route::post('editAdminUser', 'admin.System/editAdminUser');
        Route::post('delAdminUser', 'admin.System/delAdminUser');
        Route::post('setAdminUserStatus', 'admin.System/setAdminUserStatus');
        Route::get('getFullMenuList', 'admin.System/getFullMenuList');
        Route::post('logList', 'admin.System/logList');
        Route::get('runtimeLog', 'admin.System/runtimeLog');
        Route::post('cleanRuntimeLog', 'admin.System/cleanRuntimeLog');
        Route::post('cleanData', 'admin.System/cleanData');
    });

    //系统更新
    Route::group('update', function () {
        Route::get('checkUpdate', 'admin.Update/checkUpdate');
        Route::post('doUpdate', 'admin.Update/doUpdate');
    });

    //插件管理
    Route::group('pluginmanage', function () {
        Route::get('pluginList', 'admin.PluginManage/pluginList');
        Route::get('refreshPlugins', 'admin.PluginManage/refreshPlugins');
        Route::post('deletePlugin', 'admin.PluginManage/deletePlugin');
    });

    //应用市场
    Route::group('appstore', function () {
        Route::get('getAccountInfo', 'admin.AppStore/getAccountInfo');
        Route::post('oauthLogin', 'admin.AppStore/oauthLogin');
        Route::post('unbindAccount', 'admin.AppStore/unbindAccount');
        Route::get('getAppList', 'admin.AppStore/getAppList');
        Route::post('buyApp', 'admin.AppStore/buyApp');
        Route::post('recharge', 'admin.AppStore/recharge');
        Route::post('downloadApp', 'admin.AppStore/downloadApp');
        Route::post('deleteTemplate', 'admin.AppStore/deleteTemplate');
    });

    // 功能扩展插件路由
    \app\lib\AddonManager::registerAdminRoutes();
    
})->middleware([SessionInit::class, RefererCheck::class, AuthAdmin::class]);

Route::group($adminPath, function () {

    Route::any('payapi/pluginPage', 'admin.PayApi/pluginPage')->middleware(ViewOutput::class);
    Route::get('appstore/oauthCallback', 'admin.AppStore/oauthCallback');
    Route::get('user/sso', 'admin.User/sso');

})->middleware([SessionInit::class, AuthAdmin::class]);
