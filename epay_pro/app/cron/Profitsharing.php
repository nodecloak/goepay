<?php

declare(strict_types=1);

namespace app\cron;

use app\common\AbstractCronTask;
use app\lib\Payment;
use app\logic\ProfitSharingLogic;

class Profitsharing extends AbstractCronTask
{
    public static function identifier(): string
    {
        return 'profitsharing';
    }

    public static function name(): string
    {
        return '订单分账与延迟结算';
    }

    public static function parameterDefinitions(): array
    {
        return [
            'profitsharing_limit' => [
                'label' => '每次分账的订单数量',
                'type' => 'number',
                'required' => true,
                'min' => 1,
                'default' => 10,
            ],
            'settle_query_limit' => [
                'label' => '每次处理延迟结算的订单数量',
                'type' => 'number',
                'required' => true,
                'min' => 1,
                'default' => 10,
            ],
        ];
    }

    public function execute(array $params = []): string
    {
        $profitsharingLimit = max(1, (int)($params['profitsharing_limit'] ?? 10));
        $settleQueryLimit = max(1, (int)($params['settle_query_limit'] ?? 10));
        ProfitSharingLogic::task($profitsharingLimit);
        Payment::settle_task($settleQueryLimit);
        return 'ok!';
    }
}
