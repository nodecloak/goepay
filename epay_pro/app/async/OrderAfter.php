<?php

declare(strict_types=1);

namespace app\async;

use app\common\AsyncTaskInterface;
use app\service\OrderProcessService;
use InvalidArgumentException;
use think\facade\Db;

/**
 * 订单支付后的后续处理（商户通知、消息提醒、邀请返现、分账等）
 */
class OrderAfter implements AsyncTaskInterface
{
    public static function identifier(): string
    {
        return 'order_after';
    }

    public function handle(array $payload): void
    {
        $order = $payload['order'] ?? null;
        $channel = $payload['channel'] ?? null;
        if (!is_array($order) || $order === [] || !is_array($channel) || $channel === []) {
            throw new InvalidArgumentException('订单后续处理任务参数不完整');
        }
        if (empty($order['trade_no'])) {
            throw new InvalidArgumentException('订单号不能为空');
        }
        $uid = $order['uid'] ?? 0;
        $gid = Db::name('user')->where('uid', $uid)->value('gid');
        if ($gid !== null) {
            mergeGroupConfig($gid);
        }

        (new OrderProcessService($channel, $order))->executeOrderAfter();
    }
}
