<?php

declare(strict_types=1);

namespace app\common;

/**
 * 延迟分账插件基类
 */
abstract class BaseProfitSharing implements ProfitSharingInterface
{
    protected array $channel;

    public function __construct(array $channel)
    {
        $this->channel = $channel;
    }

    abstract public function submit(string $trade_no, string $api_trade_no, string $order_money, array $info): array;

    abstract public function query(string $trade_no, string $api_trade_no, string $settle_no): array;

    abstract public function unfreeze(string $trade_no, string $api_trade_no): array;

    abstract public function return(string $trade_no, string $api_trade_no, string $settle_no, array $rdata): array;

    public function addReceiver(string $account, ?string $name = null): array
    {
        return ['code' => 0, 'msg' => '添加分账接收方成功'];
    }

    public function deleteReceiver(string $account): array
    {
        return ['code' => 0, 'msg' => '删除分账接收方成功'];
    }
}
