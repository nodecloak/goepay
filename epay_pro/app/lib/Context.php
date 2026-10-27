<?php

declare(strict_types=1);

namespace app\lib;

use Closure;

class Context
{
    private static array $fallback = [];

    public static function get(?string $name = null, mixed $default = null): mixed
    {
        if (extension_loaded('swoole') && \Swoole\Coroutine::getCid() > 0) {
            $ctx = \Swoole\Coroutine::getContext();
            if ($name === null) {
                return $ctx;
            }
            return $ctx[$name] ?? $default;
        }

        if ($name === null) {
            return self::$fallback;
        }
        return self::$fallback[$name] ?? $default;
    }

    public static function set(string $name, mixed $value): void
    {
        if (extension_loaded('swoole') && \Swoole\Coroutine::getCid() > 0) {
            \Swoole\Coroutine::getContext()[$name] = $value;
            return;
        }

        self::$fallback[$name] = $value;
    }

    public static function has(string $name): bool
    {
        if (extension_loaded('swoole') && \Swoole\Coroutine::getCid() > 0) {
            return isset(\Swoole\Coroutine::getContext()[$name]);
        }

        return isset(self::$fallback[$name]);
    }

    public static function delete(string $name): void
    {
        if (extension_loaded('swoole') && \Swoole\Coroutine::getCid() > 0) {
            unset(\Swoole\Coroutine::getContext()[$name]);
            return;
        }

        unset(self::$fallback[$name]);
    }

    public static function onDestroy(Closure $closure): void
    {
        if (extension_loaded('swoole') && \Swoole\Coroutine::getCid() > 0) {
            \Swoole\Coroutine::defer($closure);
            return;
        }

        $closure();
    }
}
