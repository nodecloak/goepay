<?php

declare(strict_types=1);

namespace app\command;

use app\lib\Order;
use think\console\Input;
use think\console\input\Option;
use think\console\Output;
use think\facade\Db;

class Refund extends BaseCommand
{
    protected function configure()
    {
        $this->setName('refund')
            ->addOption('uid', 'u', Option::VALUE_OPTIONAL, '商户ID，为空则不限商户')
            ->addOption('channel', 'c', Option::VALUE_OPTIONAL, '通道ID，为空则不限通道')
            ->addOption('subchannel', 's', Option::VALUE_OPTIONAL, '子通道ID，为空则不限子通道')
            ->addOption('hours', null, Option::VALUE_OPTIONAL, '时间范围（小时），为空则不限时间')
            ->setDescription('批量退款');
    }

    protected function execute(Input $input, Output $output): int
    {
        $uid = trim((string)($input->getOption('uid') ?? ''));
        $channelId = trim((string)($input->getOption('channel') ?? ''));
        $subchannelId = trim((string)($input->getOption('subchannel') ?? ''));
        $hoursOpt = $input->getOption('hours');
        $hours = ($hoursOpt !== null && $hoursOpt !== '') ? max(1, (int)$hoursOpt) : 0;

        if ($uid === '' && $channelId === '' && $subchannelId === '') {
            $output->writeln('<error>商户ID、通道ID、子通道ID不能同时为空</error>');
            return 1;
        }

        $output->writeln(sprintf(
            '开始批量退款：商户=%s，通道=%s，子通道=%s，时间范围=%s',
            $uid !== '' ? $uid : '全部',
            $channelId !== '' ? $channelId : '全部',
            $subchannelId !== '' ? $subchannelId : '全部',
            $hours > 0 ? ('近' . $hours . '小时') : '不限'
        ));

        $query = Db::name('order')
            ->whereIn('status', [1, 2, 3])
            ->whereRaw('(refundmoney IS NULL OR refundmoney = 0 OR refundmoney < realmoney)');

        if ($uid !== '') {
            $query->where('uid', (int)$uid);
        }
        if ($channelId !== '') {
            $query->where('channel', (int)$channelId);
        }
        if ($subchannelId !== '') {
            $query->where('subchannel', (int)$subchannelId);
        }
        if ($hours > 0) {
            $endTime = date('Y-m-d H:i:s');
            $startTime = date('Y-m-d H:i:s', time() - $hours * 3600);
            $query->where('addtime', '<=', $endTime)->where('addtime', '>', $startTime);
        }

        $orders = $query->order('addtime', 'asc')->select()->toArray();
        if (empty($orders)) {
            $output->writeln('<comment>没有需要退款的订单</comment>');
            return 0;
        }

        $output->writeln('待退款数量：' . count($orders));

        $success = 0;
        $fail = 0;
        $skip = 0;

        foreach ($orders as $order) {
            $money = !empty($order['refundmoney'])
                ? round((float)$order['realmoney'] - (float)$order['refundmoney'], 2)
                : (float)$order['realmoney'];
            if ($money <= 0) {
                $skip++;
                $output->writeln($order['trade_no'] . ' 已全额退款，跳过');
                continue;
            }

            $refundNo = date('YmdHis') . sprintf('%05d', random_int(0, 99999));
            $result = Order::refund($refundNo, $order['trade_no'], $money, 1);
            if (($result['code'] ?? -1) === 0) {
                $success++;
                $output->writeln($order['trade_no'] . ' 退款成功，金额¥' . $result['money']);
            } else {
                $fail++;
                $output->writeln($order['trade_no'] . ' 退款失败：' . ($result['msg'] ?? '未知错误'));
            }
        }

        $output->writeln("<info>成功{$success}，失败{$fail}，跳过{$skip}</info>");
        return 0;
    }
}
