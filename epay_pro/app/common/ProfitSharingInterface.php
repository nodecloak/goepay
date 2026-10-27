<?php

declare(strict_types=1);

namespace app\common;

/**
 * 延迟分账插件接口
 */
interface ProfitSharingInterface
{
    /**
     * 请求分账
     * @param string $trade_no 系统订单号
     * @param string $api_trade_no 渠道支付单号
     * @param string $order_money 订单金额
     * @param array $info 分账接收方列表
     * @return array
     */
    public function submit(string $trade_no, string $api_trade_no, string $order_money, array $info): array;

    /**
     * 查询分账结果
     * @param string $trade_no 系统订单号
     * @param string $api_trade_no 渠道支付单号
     * @param string $settle_no 分账单号
     * @return array
     */
    public function query(string $trade_no, string $api_trade_no, string $settle_no): array;

    /**
     * 解冻剩余未分账资金
     * @param string $trade_no 系统订单号
     * @param string $api_trade_no 渠道支付单号
     * @return array
     */
    public function unfreeze(string $trade_no, string $api_trade_no): array;

    /**
     * 分账回退
     * @param string $trade_no 系统订单号
     * @param string $api_trade_no 渠道支付单号
     * @param string $settle_no 分账单号
     * @param array $rdata 需回退的分账明细
     * @return array
     */
    public function return(string $trade_no, string $api_trade_no, string $settle_no, array $rdata): array;

    /**
     * 添加分账接收方
     * @param string $account 接收方账号
     * @param string|null $name 接收方名称
     * @return array
     */
    public function addReceiver(string $account, ?string $name = null): array;

    /**
     * 删除分账接收方
     * @param string $account 接收方账号
     * @return array
     */
    public function deleteReceiver(string $account): array;
}
