<?php

use think\facade\Route;
use think\middleware\SessionInit;
use app\middleware\UserRefererCheck;
use app\middleware\AuthUser;
use app\middleware\ViewOutput;


Route::group('user', function () {
    // 通用接口
    Route::group('common', function () {
        Route::get('captcha', 'user.Common/captcha');
        Route::get('menuList', 'user.Common/getMenuList')->middleware(AuthUser::class);
    })->middleware([SessionInit::class, UserRefererCheck::class]);

    // 测试支付
    Route::group('testpay', function () {
        Route::get('init', 'user.TestPay/init');
        Route::get('success', 'user.TestPay/success');
        Route::post('submit', 'user.TestPay/submit');
    })->middleware([SessionInit::class, UserRefererCheck::class]);

    // 认证接口
    Route::group('auth', function () {
        Route::get('sysinfo', 'user.Auth/sysinfo');
        Route::get('checkLogin', 'user.Auth/checkLogin');
        Route::post('login', 'user.Auth/login');
        Route::post('totp', 'user.Auth/totp');
        Route::post('wxaopenid', 'user.Auth/wxaopenid');
        Route::post('wxalogin', 'user.Auth/wxalogin');
        Route::post('connect', 'user.Auth/connect');
        Route::get('wxLogin', 'user.Auth/wxLogin');
        Route::get('alipayLogin', 'user.Auth/alipayLogin');
        Route::post('sendcode', 'user.Auth/sendcode');
        Route::post('reg', 'user.Auth/reg');
        Route::post('findpwd', 'user.Auth/findpwd');
        Route::post('logout', 'user.Auth/logout');
    })->middleware([SessionInit::class, UserRefererCheck::class]);

    // 商户进件
    Route::group('applyments', function () {
        Route::get('channelList', 'user.Applyments/channelList');
        Route::post('merchantList', 'user.Applyments/merchantList');
        Route::post('addMerchant', 'user.Applyments/addMerchant');
        Route::post('delMerchant', 'user.Applyments/delMerchant');
        Route::post('setPayStatus', 'user.Applyments/setPayStatus');
        Route::post('query', 'user.Applyments/query');
        Route::get('image', 'user.Applyments/image');
        Route::get('subchannel_list', 'user.Applyments/subchannel_list');
        Route::get('mySubchannelList', 'user.Applyments/mySubchannelList');
        Route::post('setChannelStatus', 'user.Applyments/setChannelStatus');
        Route::get('getalipayauth', 'user.Applyments/getalipayauth');
        Route::get('getSubChannelMoney', 'user.Applyments/getSubChannelMoney');
        Route::post('merchantPayStat', 'user.Applyments/merchantPayStat');
        Route::post('testpay', 'user.Applyments/testpay');
        Route::get('testSubmit', 'user.Applyments/testSubmit');
        Route::get('getRollType', 'user.Applyments/getRollType');
        Route::post('setRollType', 'user.Applyments/setRollType');
        Route::post('setSubchannelWeight', 'user.Applyments/setSubchannelWeight');
        Route::post('setSubchannelSetting', 'user.Applyments/setSubchannelSetting');
        Route::post('getFormData', 'user.Applyments/getFormData');
        Route::post('submit', 'user.Applyments/submit');
        Route::post('getContractUrl', 'user.Applyments/getContractUrl');
        Route::post('queryContractStatus', 'user.Applyments/queryContractStatus');
        Route::post('uploadImage', 'user.Applyments/uploadImage');
        Route::post('uploadFile', 'user.Applyments/uploadFile');
        Route::post('checkBankCard', 'user.Applyments/checkBankCard');
        Route::post('getBankList', 'user.Applyments/getBankList');
        Route::post('getBankBranchList', 'user.Applyments/getBankBranchList');
        Route::post('riskList', 'user.Applyments/riskList');
        Route::any('form', 'user.Applyments/form')->middleware(ViewOutput::class);
        Route::get('file', 'user.Applyments/file');
    })->middleware([SessionInit::class, UserRefererCheck::class, AuthUser::class]);

    // 实名认证
    Route::group('certificate', function () {
        Route::get('info', 'user.Certificate/info');
        Route::post('submit', 'user.Certificate/submit');
        Route::post('geturl', 'user.Certificate/geturl');
        Route::post('reset', 'user.Certificate/reset');
        Route::post('query', 'user.Certificate/query');
    })->middleware([SessionInit::class, UserRefererCheck::class, AuthUser::class]);

    // 邀请返现
    Route::group('invite', function () {
        Route::get('inviteInfo', 'user.Invite/inviteInfo');
        Route::post('inviteList', 'user.Invite/inviteList');
    })->middleware([SessionInit::class, UserRefererCheck::class, AuthUser::class]);

    // 购买会员
    Route::group('group', function () {
        Route::get('grouplist', 'user.Group/grouplist');
        Route::post('groupinfo', 'user.Group/groupinfo');
        Route::post('groupbuy', 'user.Group/groupbuy');
    })->middleware([SessionInit::class, UserRefererCheck::class, AuthUser::class]);

    // 授权支付域名
    Route::group('domain', function () {
        Route::post('domainList', 'user.Domain/domainList');
        Route::post('addDomain', 'user.Domain/addDomain');
        Route::post('delDomain', 'user.Domain/delDomain');
    })->middleware([SessionInit::class, UserRefererCheck::class, AuthUser::class]);

    // 投诉处理
    Route::group('complain', function () {
        Route::get('complainSummary', 'user.Complain/complainSummary');
        Route::post('list', 'user.Complain/list');
        Route::get('export', 'user.Complain/export');
        Route::post('info', 'user.Complain/info');
        Route::post('uploadImage', 'user.Complain/uploadImage');
        Route::post('feedbackSubmit', 'user.Complain/feedbackSubmit');
        Route::post('replySubmit', 'user.Complain/replySubmit');
        Route::post('supplementSubmit', 'user.Complain/supplementSubmit');
        Route::post('refundProgressSubmit', 'user.Complain/refundProgressSubmit');
        Route::post('complete', 'user.Complain/complete');
        Route::get('wximg', 'user.Complain/wximg');
    })->middleware([SessionInit::class, UserRefererCheck::class, AuthUser::class]);

    // 分账
    Route::group('profitsharing', function () {
        Route::get('subchannelList', 'user.ProfitSharing/subchannelList');
        Route::post('receiverList', 'user.ProfitSharing/receiverList');
        Route::get('get_receiver', 'user.ProfitSharing/get_receiver');
        Route::post('add_receiver', 'user.ProfitSharing/add_receiver');
        Route::post('edit_receiver', 'user.ProfitSharing/edit_receiver');
        Route::post('del_receiver', 'user.ProfitSharing/del_receiver');
        Route::post('set_receiver', 'user.ProfitSharing/set_receiver');
        Route::post('orderList', 'user.ProfitSharing/orderList');
        Route::get('orderExport', 'user.ProfitSharing/orderExport');
        Route::post('submit', 'user.ProfitSharing/submit');
        Route::post('query', 'user.ProfitSharing/query');
        Route::post('unfreeze', 'user.ProfitSharing/unfreeze');
        Route::post('return', 'user.ProfitSharing/return_');
        Route::post('statistics', 'user.ProfitSharing/statistics');
    })->middleware([SessionInit::class, UserRefererCheck::class, AuthUser::class]);

    // 充值余额
    Route::group('recharge', function () {
        Route::get('info', 'user.Recharge/info');
        Route::post('submit', 'user.Recharge/submit');
    })->middleware([SessionInit::class, UserRefererCheck::class, AuthUser::class]);

    // 保证金
    Route::group('deposit', function () {
        Route::get('info', 'user.Deposit/info');
        Route::post('recharge', 'user.Deposit/recharge');
        Route::post('withdraw', 'user.Deposit/withdraw');
    })->middleware([SessionInit::class, UserRefererCheck::class, AuthUser::class]);

    // 订单
    Route::group('order', function () {
        Route::post('list', 'user.Order/list');
        Route::get('export', 'user.Order/export');
        Route::post('statistics', 'user.Order/statistics');
        Route::get('detail', 'user.Order/detail');
        Route::get('subOrders', 'user.Order/subOrders');
        Route::post('queryOrder', 'user.Order/queryOrder');
        Route::post('notify', 'user.Order/notify');
        Route::post('printOrder', 'user.Order/printOrder');
        Route::post('refund_query', 'user.Order/refund_query');
        Route::post('refund_submit', 'user.Order/refund_submit');
        Route::get('payTypeList', 'user.Order/payTypeList');
        Route::post('refundList', 'user.Order/refundList');
        Route::post('refundStatistics', 'user.Order/refundStatistics');
        Route::post('dailyStat', 'user.OrderStat/dailyStat');
        Route::post('provinceStats', 'user.OrderStat/provinceStats');
    })->middleware([SessionInit::class, UserRefererCheck::class, AuthUser::class]);

    // 结算管理
    Route::group('settle', function () {
        Route::post('list', 'user.Settle/list');
        Route::post('statistics', 'user.Settle/statistics');
        Route::get('result', 'user.Settle/result');
        Route::get('withdraw', 'user.Settle/withdrawInfo');
        Route::post('withdraw', 'user.Settle/withdrawSubmit');
    })->middleware([SessionInit::class, UserRefererCheck::class, AuthUser::class]);

    // 代付/转账
    Route::group('transfer', function () {
        Route::post('list', 'user.Transfer/list');
        Route::post('statistics', 'user.Transfer/statistics');
        Route::get('result', 'user.Transfer/result');
        Route::get('query', 'user.Transfer/query');
        Route::post('queryBalance', 'user.Transfer/queryBalance');
        Route::get('detail', 'user.Transfer/detail');
        Route::post('proof', 'user.Transfer/proof');
        Route::get('copyInfo', 'user.Transfer/copyInfo');
        Route::post('submit', 'user.Transfer/submit');
        Route::post('redSubmit', 'user.Transfer/redSubmit');
        Route::post('batchSubmit', 'user.Transfer/batchSubmit');
    })->middleware([SessionInit::class, UserRefererCheck::class, AuthUser::class]);

    // 站内转账
    Route::group('internalTransfer', function () {
        Route::get('info', 'user.InternalTransfer/info');
        Route::post('list', 'user.InternalTransfer/list');
        Route::post('add', 'user.InternalTransfer/add');
    })->middleware([SessionInit::class, UserRefererCheck::class, AuthUser::class]);

    // 资金明细
    Route::group('record', function () {
        Route::post('list', 'user.Record/list');
        Route::post('stats', 'user.Record/stats');
    })->middleware([SessionInit::class, UserRefererCheck::class, AuthUser::class]);

    // 账户设置
    Route::group('account', function () {
        Route::get('userinfo', 'user.Account/userinfo');
        Route::get('settings', 'user.Account/accountSettings');
        Route::get('apiinfo', 'user.Account/apiinfo');
        Route::get('checkBind', 'user.Account/checkBind');
        Route::get('onecodeInfo', 'user.Account/onecodeInfo');
        Route::post('sendcode', 'user.Account/sendcode');
        Route::post('verifycode', 'user.Account/verifycode');
        Route::post('editSettle', 'user.Account/editSettle');
        Route::post('editRemainMoney', 'user.Account/editRemainMoney');
        Route::post('completeinfo', 'user.Account/completeinfo');
        Route::post('editInfo', 'user.Account/editInfo');
        Route::post('editKeyType', 'user.Account/editKeyType');
        Route::post('editEventSubscribe', 'user.Account/editEventSubscribe');
        Route::post('editVoice', 'user.Account/editVoice');
        Route::post('editPrint', 'user.Account/editPrint');
        Route::post('editChannelInfo', 'user.Account/editChannelInfo');
        Route::post('editMsgConfig', 'user.Account/editMsgConfig');
        Route::post('editQqbotConfig', 'user.Account/editQqbotConfig');
        Route::post('editBind', 'user.Account/editBind');
        Route::post('unbind_connect', 'user.Account/unbindConnect');
        Route::post('resetKey', 'user.Account/resetKey');
        Route::post('createRsaPair', 'user.Account/createRsaPair');
        Route::post('editPwd', 'user.Account/editPwd');
        Route::post('editCodename', 'user.Account/editCodename');
        Route::get('getTotpStatus', 'user.Account/getTotpStatus');
        Route::post('totpGenerate', 'user.Account/totpGenerate');
        Route::post('totpBind', 'user.Account/totpBind');
        Route::post('totpClose', 'user.Account/totpClose');
        Route::post('logList', 'user.Account/logList');
    })->middleware([SessionInit::class, UserRefererCheck::class, AuthUser::class]);

    // 控制台首页
    Route::get('index/dashboard', 'user.Index/dashboard')->middleware([UserRefererCheck::class, AuthUser::class]);

    // 实名认证回调/跳转页
    Route::get('alipaycert', 'user.Certificate/alipayCert');
    Route::get('alipaycertok', 'user.Certificate/alipayCertOk');

    // 快捷登录回调/跳转
    Route::get('connect', 'user.OAuth/connect')->middleware([SessionInit::class]);
    Route::get('oauth/douyin', 'user.OAuth/douyin');
    Route::get('oauth/alipay', 'user.OAuth/alipay')->middleware([SessionInit::class]);
    Route::get('oauth/wechat', 'user.OAuth/wechat')->middleware([SessionInit::class]);
    Route::get('oauth/telegram', 'user.OAuth/telegram')->middleware([SessionInit::class]);
    Route::group('openid', function () {
        Route::get('wechat', 'user.Openid/wechat')->middleware([SessionInit::class]);
        Route::get('alipay', 'user.Openid/alipay')->middleware([SessionInit::class]);
        Route::get('douyin', 'user.Openid/douyin')->middleware([SessionInit::class]);
        Route::get('qrcode', 'user.Openid/qrcode')->middleware([SessionInit::class]);
        Route::get('getopenid', 'user.Openid/getopenid')->middleware([SessionInit::class]);
    });

    // 付款码支付
    Route::post('scanpay/jsconfig', 'user.Scanpay/jsconfig')->middleware([AuthUser::class]);
    Route::any('scanpay', 'user.Scanpay/submit')->middleware([AuthUser::class]);

    Route::get('test', 'user.TestPay/index');

    // 功能扩展插件路由
    \app\lib\AddonManager::registerUserRoutes();

});