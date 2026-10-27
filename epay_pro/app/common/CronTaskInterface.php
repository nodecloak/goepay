<?php

declare(strict_types=1);

namespace app\common;

interface CronTaskInterface
{
    public static function identifier(): string;

    public static function name(): string;

    /**
     * @return array<string, array<string, mixed>>
     */
    public static function parameterDefinitions(): array;

    /**
     * @param array<string, mixed> $params
     */
    public function execute(array $params = []): string;
}
