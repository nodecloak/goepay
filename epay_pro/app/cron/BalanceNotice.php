<?php

declare(strict_types=1);

namespace app\cron;

use app\common\AbstractCronTask;
use app\lib\MsgNotice;
use think\facade\Db;

class BalanceNotice extends AbstractCronTask
{
    public static function identifier(): string
    {
        return 'balance_notice';
    }

    public static function name(): string
    {
        return '商户余额不足提醒';
    }

    public function execute(array $params = []): string
    {
        if (!config_get('wxnotice_tpl_balance') && !config_get('msgconfig_balance')) {
            throw new \Exception('未配置商户余额不足提醒');
        }

        $count = 0;
        foreach (Db::name('user')->where('status', 1)->select()->toArray() as $user) {
            $config = !empty($user['msgconfig']) ? @unserialize($user['msgconfig']) : [];
            if (
                !is_array($config)
                || empty($config['balance'])
                || empty($config['balance_money'])
                || $user['money'] >= $config['balance_money']
            ) {
                continue;
            }
            if (cache('balance_notice_' . $user['uid']) == date('Ymd')) {
                continue;
            }

            MsgNotice::send('balance', $user['uid'], [
                'user' => $user['uid'],
                'time' => date('Y-m-d H:i:s'),
                'money' => $user['money'],
            ]);
            cache('balance_notice_' . $user['uid'], date('Ymd'), 86400);
            $count++;
        }

        return $count > 0
            ? '余额不足提醒已发送给' . $count . '位商户'
            : '暂无需要发送余额不足提醒的商户';
    }
}
