<?php

declare(strict_types=1);

namespace app\command;

use think\console\Command;
use think\console\Input;
use think\console\Output;
use think\facade\Config;
use think\facade\Db;

abstract class BaseCommand extends Command
{
    protected function initialize(Input $input, Output $output)
    {
        if (!file_exists(app()->getRootPath() . '.env')) {
            return;
        }

        Db::event('after_connect', static function (\PDO $pdo): void {
            $pdo->exec("SET SESSION sql_mode=''");
        });

        $res = Db::name('config')->where('cache', 1)->cache('configs', 0)->column('v', 'k');

        $siteurl = $res['siteurl'] ?? 'http://localhost/';
        if (empty($res['localurl'])) $res['localurl'] = $siteurl;
        Config::set($res, 'sys');
        define('SYS_KEY', $res['syskey'] ?? '');

        request()->siteurl = $siteurl;
        request()->clientip = config_get('serverip', '127.0.0.1');
        request()->isadmin = false;
        request()->islogin = false;
        \app\lib\AddonManager::boot();
    }
}
