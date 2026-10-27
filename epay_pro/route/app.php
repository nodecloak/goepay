<?php

use think\facade\Route;
use think\middleware\SessionInit;
use app\middleware\RefererCheck;
use app\middleware\ViewOutput;
use think\facade\View;

Route::get('/', 'index/index')->middleware([SessionInit::class, ViewOutput::class]);
Route::group('index', function () {
    Route::get(':page', 'index/index')->pattern(['page' => '[A-Za-z0-9_]+' ]);
})->middleware(ViewOutput::class);
Route::get('test', 'index/test');

Route::group('install', function () {
    Route::get('/', 'Install/index');
    Route::post('check_env', 'Install/checkEnv');
    Route::post('check_config', 'Install/checkConfig');
    Route::post('do_install', 'Install/doInstall');
});

Route::group('payment', function () {
    Route::post('captcha_verify', 'payment.Ajax/captchaVerify')->middleware(RefererCheck::class);
    Route::get('check_status', 'payment.Ajax/checkStatus')->middleware(RefererCheck::class);
    Route::any('submit', 'payment.Submit/index')->middleware(ViewOutput::class);
    Route::any('voice', 'payment.Voice/index');
    Route::any('cashier', 'payment.Cashier/index');
});

Route::group('transfer', function () {
    Route::get('wx', 'transfer.Weixin/index');
    Route::get('red', 'transfer.RedEnvelope/index');
    Route::post('red_receive', 'transfer.RedEnvelope/receive')->middleware(RefererCheck::class);
});

Route::get('gold', 'payment.Gold/index');

Route::any('qqbot/webhook/:uid', 'payment.Qqbot/webhook');
Route::any('qqbot/webhook', 'payment.Qqbot/webhook');

Route::group('paypage', function () {
    Route::get('/', 'qrpay.Index/index');
    Route::post('ajax', 'qrpay.Index/ajax')->middleware(RefererCheck::class);
    Route::get('success', 'qrpay.Index/success');
})->middleware(SessionInit::class);

Route::get('paypage/', 'qrpay.Index/index')->middleware(SessionInit::class);

Route::get('payok', function(){
    return View::fetch('index/payok');
})->middleware(ViewOutput::class);
Route::get('payerr', function(){
    return View::fetch('index/payerr');
})->middleware(ViewOutput::class);
Route::get('wxtoqq', function(){
    return View::fetch('index/wxtoqq');
})->middleware(ViewOutput::class);
Route::get('agreement', function(){
    return View::fetch('index/agreement');
})->middleware(ViewOutput::class);

// 开发文档页面
Route::group('doc', function () {
    Route::get('/', function(){
        return redirect('/doc/index.html');
    });
    Route::get(':page', 'index/doc')->pattern(['page' => '[A-Za-z0-9_]+' ]);
})->middleware(ViewOutput::class);

Route::any('pay/:payapi_func/:payapi_trade_no', 'payment.Gateway/index')->middleware(ViewOutput::class);

Route::get('cron/plugin', 'Cron/plugin');
Route::get('cron', 'Cron/index');

// 小程序聚合收银台
Route::group('applet/cashier', function () {
    Route::get('merchant', 'applet.Cashier/merchant');
    Route::post('create', 'applet.Cashier/create');
});

// 功能扩展插件路由
\app\lib\AddonManager::registerCommonRoutes();

Route::miss(function(){
    if (request()->isAjax()) {
        return json_response(404, 'page not found');
    }
    return View::fetch('index/404');
});