<?php

declare(strict_types=1);

namespace app\common;

abstract class AbstractCronTask implements CronTaskInterface
{
    public static function parameterDefinitions(): array
    {
        return [];
    }
}
