<?php

declare(strict_types=1);

namespace app\command;

use app\lib\Channel;
use app\lib\Plugin;
use app\service\OrderProcessService;
use think\console\Input;
use think\console\input\Option;
use think\console\Output;
use think\facade\Db;
use Throwable;

class OrderQuery extends BaseCommand
{
    protected function configure()
    {
        $this->setName('orderquery')
            ->addOption('channel', 'c', Option::VALUE_OPTIONAL, '支付通道ID，为空则不限通道')
            ->addOption('plugin', 'p', Option::VALUE_OPTIONAL, '支付插件标识，为空则不限插件')
            ->addOption('hours', null, Option::VALUE_OPTIONAL, '时间范围（小时），默认查询24小时内的订单', '24')
            ->setDescription('未支付订单查询补单');
    }

    protected function execute(Input $input, Output $output): int
    {
        $channelId = trim((string)($input->getOption('channel') ?? ''));
        $plugin = trim((string)($input->getOption('plugin') ?? ''));
        $hours = max(1, (int)($input->getOption('hours') ?: 24));

        $endTime = date('Y-m-d H:i:s');
        $startTime = date('Y-m-d H:i:s', time() - $hours * 3600);

        $output->writeln(sprintf(
            '开始未支付订单查单：通道=%s，插件=%s，时间范围=%s ~ %s',
            $channelId !== '' ? $channelId : '全部',
            $plugin !== '' ? $plugin : '全部',
            $startTime,
            $endTime
        ));

        $query = Db::name('order')
            ->where('status', 0)
            ->where('channel', '>', 0)
            ->where('addtime', '<=', $endTime)
            ->where('addtime', '>', $startTime);

        if ($channelId !== '') {
            $query->where('channel', (int)$channelId);
        }

        if ($plugin !== '') {
            $channelIds = Db::name('channel')->where('plugin', $plugin)->column('id');
            if (empty($channelIds)) {
                $output->writeln('<comment>没有匹配该插件的支付通道</comment>');
                return 0;
            }
            if ($channelId !== '' && !in_array((int)$channelId, array_map('intval', $channelIds), true)) {
                $output->writeln('<error>指定的支付通道与插件不匹配</error>');
                return 1;
            }
            if ($channelId === '') {
                $query->whereIn('channel', $channelIds);
            }
        }

        $orders = $query->order('addtime', 'asc')->select()->toArray();
        if (empty($orders)) {
            $output->writeln('<comment>没有需要补单的订单</comment>');
            return 0;
        }

        $output->writeln('待查单数量：' . count($orders));

        $paid = 0;
        $unpaid = 0;
        $skip = 0;
        $fail = 0;

        foreach ($orders as $row) {
            $channel = $row['subchannel'] > 0
                ? Channel::getSub($row['subchannel'])
                : Channel::get($row['channel'], Db::name('user')->where('uid', $row['uid'])->value('channelinfo'));
            if (!$channel) {
                $skip++;
                $output->writeln($row['trade_no'] . ' 通道不存在');
                continue;
            }
            if (!Plugin::isquery($channel['plugin'])) {
                $skip++;
                $output->writeln($row['trade_no'] . ' 插件不支持查单');
                continue;
            }

            try {
                $data = Plugin::query($row);
            } catch (Throwable $e) {
                exception_log($e, '未支付订单查单');
                $fail++;
                $output->writeln($row['trade_no'] . ' 查单失败：' . $e->getMessage());
                continue;
            }

            if (isset($data['status']) && (int)$data['status'] === 1) {
                try {
                    (new OrderProcessService($channel, $row))->processNotify(
                        $data['api_trade_no'],
                        $data['buyer'] ?? null,
                        $data['bill_trade_no'] ?? null,
                        $data['bill_mch_trade_no'] ?? null,
                        $data['endtime'] ?? null
                    );
                    $paid++;
                    $output->writeln($row['trade_no'] . ' 补单成功');
                } catch (Throwable $e) {
                    exception_log($e, '未支付订单补单');
                    $fail++;
                    $output->writeln($row['trade_no'] . ' 补单失败：' . $e->getMessage());
                }
            } else {
                $unpaid++;
                $output->writeln($row['trade_no'] . ' 未支付');
            }
        }

        $output->writeln("<info>成功补单{$paid}，未支付{$unpaid}，跳过{$skip}，失败{$fail}</info>");
        return 0;
    }
}
