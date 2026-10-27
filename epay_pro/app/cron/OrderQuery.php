<?php

declare(strict_types=1);

namespace app\cron;

use app\common\AbstractCronTask;
use app\lib\Channel;
use app\lib\Plugin;
use app\service\OrderProcessService;
use think\facade\Db;
use Throwable;

class OrderQuery extends AbstractCronTask
{
    public static function identifier(): string
    {
        return 'order_query';
    }

    public static function name(): string
    {
        return '未支付订单查询补单';
    }

    public static function parameterDefinitions(): array
    {
        return [
            'unpaid_minutes' => [
                'label' => '未支付订单超过多少分钟后进行查单',
                'type' => 'number',
                'required' => true,
                'min' => 10,
                'default' => 60,
            ],
        ];
    }

    public function execute(array $params = []): string
    {
        $minutes = max(1, (int)($params['unpaid_minutes'] ?? 60));
        $endTime = date('Y-m-d H:i:s', time() - $minutes * 60);
        $startTime = date('Y-m-d H:i:s', time() - $minutes * 60 - 6 * 3600);

        $orders = Db::name('order')
            ->where('status', 0)
            ->where('channel', '>', 0)
            ->where('notify', 0)
            ->where('addtime', '<=', $endTime)
            ->where('addtime', '>', $startTime)
            ->order('addtime', 'asc')
            ->limit(50)
            ->select()
            ->toArray();

        if (empty($orders)) {
            return '没有需要补单的订单';
        }

        $paid = 0;
        $unpaid = 0;
        $skip = 0;
        $fail = 0;

        foreach ($orders as $row) {
            $channel = $row['subchannel'] > 0
                ? Channel::getSub((int)$row['subchannel'])
                : Channel::get((int)$row['channel'], Db::name('user')->where('uid', $row['uid'])->value('channelinfo'));
            if (!$channel) {
                $skip++;
                $this->markQueried($row['trade_no']);
                echo '[未支付订单查单] ' . $row['trade_no'] . " 通道不存在\n";
                continue;
            }
            if (!Plugin::isquery($channel['plugin'])) {
                $skip++;
                $this->markQueried($row['trade_no']);
                continue;
            }

            try {
                $data = Plugin::query($row);
            } catch (Throwable $e) {
                exception_log($e, '未支付订单查单');
                $fail++;
                $this->markQueried($row['trade_no']);
                echo '[未支付订单查单] ' . $row['trade_no'] . ' 查单失败：' . $e->getMessage() . "\n";
                continue;
            }

            if (isset($data['status']) && (int)$data['status'] === 1) {
                $gid = Db::name('user')->where('uid', $row['uid'])->value('gid');
                if ($gid !== null) {
                    mergeGroupConfig($gid);
                }
                try {
                    (new OrderProcessService($channel, $row))->processNotify(
                        $data['api_trade_no'],
                        $data['buyer'] ?? null,
                        $data['bill_trade_no'] ?? null,
                        $data['bill_mch_trade_no'] ?? null,
                        $data['endtime'] ?? null
                    );
                    $paid++;
                    echo '[未支付订单查单] ' . $row['trade_no'] . " 补单成功\n";
                } catch (Throwable $e) {
                    exception_log($e, '未支付订单补单');
                    $fail++;
                    $this->markQueried($row['trade_no']);
                    echo '[未支付订单查单] ' . $row['trade_no'] . ' 补单失败：' . $e->getMessage() . "\n";
                }
            } else {
                $unpaid++;
                $this->markQueried($row['trade_no']);
            }
        }

        return "[未支付订单查单] 补单{$paid}，未支付{$unpaid}，跳过{$skip}，失败{$fail}";
    }

    private function markQueried(string $tradeNo): void
    {
        Db::name('order')->where('trade_no', $tradeNo)->where('status', 0)->update(['notify' => 1]);
    }
}
