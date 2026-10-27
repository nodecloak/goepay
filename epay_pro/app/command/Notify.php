<?php

declare(strict_types=1);

namespace app\command;

use app\service\OrderNotifyService;
use think\console\Input;
use think\console\input\Option;
use think\console\Output;
use think\facade\Db;

class Notify extends BaseCommand
{
    protected function configure()
    {
        $this->setName('notify')
            ->addOption('uid', 'u', Option::VALUE_OPTIONAL, '商户ID，为空则不限商户')
            ->addOption('hours', null, Option::VALUE_OPTIONAL, '时间范围（小时），默认查询24小时内的订单', '24')
            ->setDescription('订单异步通知重试');
    }

    protected function execute(Input $input, Output $output): int
    {
        $uid = trim((string)($input->getOption('uid') ?? ''));
        $hours = max(1, (int)($input->getOption('hours') ?: 24));

        $endTime = date('Y-m-d H:i:s');
        $startTime = date('Y-m-d H:i:s', time() - $hours * 3600);

        $output->writeln(sprintf(
            '开始订单异步通知重试：商户=%s，时间范围=%s ~ %s',
            $uid !== '' ? $uid : '全部',
            $startTime,
            $endTime
        ));

        $query = Db::name('order')
            ->where('status', '>', 0)
            ->where('notify', '<>', 0)
            ->where('addtime', '<=', $endTime)
            ->where('addtime', '>', $startTime);

        if ($uid !== '') {
            $query->where('uid', (int)$uid);
        }

        $orders = $query->order('addtime', 'asc')->select()->toArray();
        if (empty($orders)) {
            $output->writeln('<comment>没有需要重试通知的订单</comment>');
            return 0;
        }

        $output->writeln('待通知数量：' . count($orders));

        $success = 0;
        $fail = 0;

        foreach ($orders as $order) {
            $service = new OrderNotifyService($order);
            if ($service->doNotify()) {
                Db::name('order')->where('trade_no', $order['trade_no'])
                    ->update(['notify' => 0, 'notifytime' => null]);
                $success++;
                $output->writeln($order['trade_no'] . ' 重新通知成功');
            } else {
                $fail++;
                $output->writeln($order['trade_no'] . ' 重新通知失败');
            }
        }

        $output->writeln("<info>成功{$success}，失败{$fail}</info>");
        return 0;
    }
}
