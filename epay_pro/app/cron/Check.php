<?php

declare(strict_types=1);

namespace app\cron;

use app\common\AbstractCronTask;
use app\logic\RiskCheckLogic;

class Check extends AbstractCronTask
{
    public static function identifier(): string
    {
        return 'check';
    }

    public static function name(): string
    {
        return '支付风控检测';
    }

    public function execute(array $params = []): string
    {
        RiskCheckLogic::execute();
        return 'ok!';
    }
}
