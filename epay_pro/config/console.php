<?php
// +----------------------------------------------------------------------
// | 控制台配置
// +----------------------------------------------------------------------
return [
    // 指令定义
    'commands' => [
        'reset' => 'app\command\Reset',
        'cron' => 'app\command\Cron',
        'async' => 'app\command\Async',
        'orderquery' => 'app\command\OrderQuery',
        'notify' => 'app\command\Notify',
        'refund' => 'app\command\Refund',
    ],
];
