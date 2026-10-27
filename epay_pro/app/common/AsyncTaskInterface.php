<?php

declare(strict_types=1);

namespace app\common;

/**
 * 异步任务接口。
 *
 * 新增任务：在 app/async 下按标识创建类（order_after → OrderAfter），
 * 或调用 AsyncTaskRunner::register($identifier, $class) 注册。
 * 投递：AsyncTaskRunner::tryDispatch('task_name', $payload)
 */
interface AsyncTaskInterface
{
    public static function identifier(): string;

    /**
     * @param array<string, mixed> $payload
     */
    public function handle(array $payload): void;
}
