<?php

declare(strict_types=1);

namespace app\lib;

use Throwable;
use think\Cache;
use think\Config;
use think\Db;
use think\Event;
use think\Log;
use think\db\ConnectionInterface;
use Swoole\ConnectionPool;

class PoolDbManager extends Db
{
    protected static array $pools = [];

    public static function __make(Event $event, Config $config, Log $log, Cache $cache): static
    {
        $db = new static();
        $db->setConfig($config);
        $db->setEvent($event);
        $db->setLog($log);
        $db->setCache($cache->store($db->getConfig('cache_store')));
        $db->triggerSql();
        return $db;
    }

    protected function instance(string|array|null $name = null, bool $force = false): ConnectionInterface
    {
        if (empty($name)) {
            $name = $this->getConfig('default', 'mysql');
        }

        if ($force) {
            return $this->createConnection($name);
        }

        $poolName = is_array($name) ? md5(json_encode($name)) : $name;
        if (!extension_loaded('swoole') || \Swoole\Coroutine::getCid() <= 0) {
            return $this->instanceFallback($name, $poolName);
        }

        $key = "think-orm.connections.$poolName";
        $connection = Context::get($key);

        if ($connection) {
            return $connection;
        }

        if (!isset(static::$pools[$poolName])) {
            $this->initPool($poolName, $name);
        }

        $pool = static::$pools[$poolName];
        $waitTimeout = is_string($name)
            ? $this->getConfig('connections.' . $name . '.pool.wait_timeout', 3)
            : 3;
        $connection = $pool->get($waitTimeout);
        if ($connection === false) {
            throw new \RuntimeException("Connection pool '{$poolName}' exhausted after waiting {$waitTimeout}s");
        }

        Context::set($key, $connection);
        Context::onDestroy(function () use ($connection, $pool) {
            try {
                $pool->put($connection);
            } catch (Throwable) {
            }
        });
        return $connection;
    }

    protected function initPool(string $poolName, string|array $name): void
    {
        $poolConfig = is_array($name)
            ? []
            : $this->getConfig('connections.' . $name . '.pool', []);
        $maxConnections = $poolConfig['max_connections'] ?? 10;

        static::$pools[$poolName] = new ConnectionPool(
            fn(): ConnectionInterface => $this->createConnection($name),
            (int)$maxConnections
        );
    }

    protected function instanceFallback(string|array $name, string $poolName): ConnectionInterface
    {
        $key = "think-orm.connections.$poolName";
        $connection = Context::get($key);

        if (!$connection) {
            $connection = $this->createConnection($name);
            Context::set($key, $connection);
        }

        return $connection;
    }
}
