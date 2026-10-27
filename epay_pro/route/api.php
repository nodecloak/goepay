<?php

use think\facade\Route;
use app\middleware\ApiVerify;


Route::group('api', function () {
    // 支付接口 - 无需中间件签名校验
    Route::group('pay', function () {
        Route::any('submit', 'api.Pay/submit');
        Route::any('create', 'api.Pay/create');
    });

    // 支付接口 - 需要 API 签名校验中间件
    Route::group('pay', function () {
        Route::post('query', 'api.Pay/query');
        Route::post('refund', 'api.Pay/refund');
        Route::post('refundquery', 'api.Pay/refundquery');
        Route::post('close', 'api.Pay/close');
    })->middleware(ApiVerify::class);

    // 代付接口 - 需要 API 签名校验中间件
    Route::group('transfer', function () {
        Route::post('submit', 'api.Transfer/submit');
        Route::post('query', 'api.Transfer/query');
        Route::post('proof', 'api.Transfer/proof');
        Route::post('balance', 'api.Transfer/balance');
    })->middleware(ApiVerify::class);

    // 商户信息接口 - 需要 API 签名校验中间件
    Route::group('merchant', function () {
        Route::post('info', 'api.Merchant/info');
        Route::post('orders', 'api.Merchant/orders');
    })->middleware(ApiVerify::class);

    // 投诉接口 - 无需中间件签名校验
    Route::group('complain', function () {
        Route::get('image', 'api.Complain/image');
    });

    // 投诉接口 - 需要 API 签名校验中间件
    Route::group('complain', function () {
        Route::post('list', 'api.Complain/list');
        Route::post('detail', 'api.Complain/detail');
        Route::post('upload', 'api.Complain/upload');
        Route::post('feedback', 'api.Complain/feedback');
        Route::post('reply', 'api.Complain/reply');
        Route::post('refundprogress', 'api.Complain/refundprogress');
        Route::post('complete', 'api.Complain/complete');
        Route::post('supplement', 'api.Complain/supplement');
    })->middleware(ApiVerify::class);

    // 旧版API兼容接口（pid+key 明文认证）
    Route::any('obsolete', 'api.Obsolete/index');

    Route::miss(function () {
        return json(['code' => -4, 'msg' => 'No Route']);
    });
});