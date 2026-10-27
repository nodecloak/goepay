<?php

declare(strict_types=1);

namespace app\common;

/**
 * 仅支持实时分账的支付通道
 */
final class RealtimeOnlyProfitSharing implements ProfitSharingInterface
{
    public function __construct(private array $channel)
    {
    }

    public function submit(string $trade_no, string $api_trade_no, string $order_money, array $info): array
    {
        throw new \Exception('当前支付通道仅支持实时分账');
    }

    public function query(string $trade_no, string $api_trade_no, string $settle_no): array
    {
        throw new \Exception('当前支付通道仅支持实时分账');
    }

    public function unfreeze(string $trade_no, string $api_trade_no): array
    {
        throw new \Exception('当前支付通道仅支持实时分账');
    }

    public function return(string $trade_no, string $api_trade_no, string $settle_no, array $rdata): array
    {
        throw new \Exception('当前支付通道仅支持实时分账');
    }

    public function addReceiver(string $account, ?string $name = null): array
    {
        return ['code' => 0, 'msg' => '添加分账接收方成功'];
    }

    public function deleteReceiver(string $account): array
    {
        return ['code' => 0, 'msg' => '删除分账接收方成功'];
    }
}
