<?php

declare(strict_types=1);

namespace app\cron;

use app\common\AbstractCronTask;
use app\service\OrderNotifyService;
use app\service\OrderProcessService;
use think\facade\Db;
use Throwable;

class Notify extends AbstractCronTask
{
    public static function identifier(): string
    {
        return 'notify';
    }

    public static function name(): string
    {
        return '订单异步通知重试';
    }

    public function execute(array $params = []): string
    {
        $orders = Db::name('order')
            ->where('date', '>=', date('Y-m-d', strtotime('-1 day')))
            ->where('status', '>', 0)
            ->where(function ($query) {
                $query->where('notify', -2)->whereOr('notify', '>', 0);
            })
            ->where('notifytime', '<', date('Y-m-d H:i:s'))
            ->order('notifytime', 'asc')
            ->limit(20)
            ->select()
            ->toArray();

        foreach ($orders as $order) {
            if ((int)$order['notify'] === -2) {
                $this->retryOrderAfter($order);
                continue;
            }
            $this->retryNotify($order);
        }

        return 'ok!';
    }

    private function retryNotify(array $order): void
    {
        $notify = $order['notify'] + 1;
        $interval = match ($notify) {
            2 => '1 MINUTE',
            3 => '9 MINUTE',
            4 => '40 MINUTE',
            5 => '1 HOUR',
            default => null,
        };
        if ($interval === null) {
            Db::name('order')->where('trade_no', $order['trade_no'])
                ->where('notify', $order['notify'])
                ->update(['notify' => -1, 'notifytime' => null]);
            return;
        }

        $claimed = Db::name('order')->where('trade_no', $order['trade_no'])
            ->where('notify', $order['notify'])
            ->update([
                'notify' => $notify,
                'notifytime' => Db::raw("DATE_ADD(NOW(), INTERVAL {$interval})"),
            ]);
        if ($claimed === 0) {
            return;
        }

        $service = new OrderNotifyService($order);
        if ($service->doNotify()) {
            Db::name('order')->where('trade_no', $order['trade_no'])
                ->update(['notify' => 0, 'notifytime' => null]);
            echo '[订单通知重试] ' . $order['trade_no'] . ' 重新通知成功' . "\n";
            return;
        }

        echo '[订单通知重试] ' . $order['trade_no'] . ' 重新通知失败（第' . $notify . '次）' . "\n";
        if (config_get('auto_check_notify') == 1) {
            $this->checkNotifyFailure($order);
        }
    }

    private function retryOrderAfter(array $order): void
    {
        $claimed = Db::name('order')
            ->where('trade_no', $order['trade_no'])
            ->where('notify', -2)
            ->update(['notifytime' => date('Y-m-d H:i:s', strtotime('+10 minutes'))]);
        if ($claimed === 0) {
            return;
        }

        $gid = Db::name('user')->where('uid', $order['uid'])->value('gid');
        if ($gid !== null) {
            mergeGroupConfig($gid);
        }

        try {
            OrderProcessService::executeOrderAfterByOrder($order);
            echo '[订单后续处理补偿] ' . $order['trade_no'] . " 已重新执行\n";
        } catch (Throwable $e) {
            exception_log($e, '订单后续处理补偿');
            echo '[订单后续处理补偿] ' . $order['trade_no'] . ' 失败：' . $e->getMessage() . "\n";
        }
    }

    private function checkNotifyFailure(array $order): void
    {
        $maxFail = (int)config_get('check_notify_count');
        if ($maxFail <= 0) {
            return;
        }

        $user = Db::name('user')->where('uid', $order['uid'])->field('uid,email,pay')->find();
        if (!$user || $user['pay'] != 1) {
            return;
        }

        $orders = Db::name('order')
            ->where('uid', $order['uid'])
            ->where('status', '>', 0)
            ->where('date', '>=', date('Y-m-d', strtotime('-3 day')))
            ->order('trade_no', 'desc')
            ->limit($maxFail)
            ->select()
            ->toArray();
        $failCount = count(array_filter($orders, static fn(array $item): bool => $item['notify'] > 0));
        if ($failCount < $maxFail) {
            return;
        }

        Db::name('user')->where('uid', $order['uid'])->update(['pay' => 0]);
        Db::name('risk')->insert([
            'uid' => $order['uid'],
            'type' => 2,
            'content' => '连续' . $failCount . '个订单',
            'date' => date('Y-m-d H:i:s'),
        ]);

        if (config_get('check_notify_notice') == 1) {
            $siteName = config_get('sitename');
            send_mail(
                $user['email'],
                $siteName . ' - 商户支付权限关闭提醒',
                '尊敬的用户：你的商户ID ' . $user['uid'] . ' 因连续' . $failCount
                . '个订单回调通知失败，已被系统自动关闭支付权限！请自行检查你的网站是否有防CC防火墙、WAF等，导致支付回调拦截。如有疑问请联系网站客服。<br/>----------<br/>'
                . $siteName . '<br/>' . date('Y-m-d H:i:s')
            );
        }
    }
}
