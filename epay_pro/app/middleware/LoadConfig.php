<?php

declare(strict_types=1);

namespace app\middleware;

use think\facade\Db;
use think\facade\Config;
use think\facade\Cache;

class LoadConfig
{
    /**
     * 处理请求
     *
     * @param \think\Request $request
     * @param \Closure       $next
     * @return Response
     */
    public function handle($request, \Closure $next)
    {
        Db::event('after_connect', static function (\PDO $pdo): void {
            $pdo->exec("SET SESSION sql_mode=''");
        });

        if (!file_exists(app()->getRootPath() . '.env')) {
            if (strpos($request->url(), '/install') === false) {
                return redirect((string)url('/install'))->header([
                    'Cache-Control' => 'no-store, no-cache, must-revalidate',
                    'Pragma' => 'no-cache',
                ]);
            } else {
                return $next($request);
            }
        }

        $siteurl = (is_https() ? 'https://' : 'http://') . $request->host() . '/';

        try {
            $res = Db::name('config')->where('cache', 1)->cache('configs', 0)->column('v', 'k');
        } catch (\Throwable $e) {
            $res = Db::name('config')->cache('configs', 0)->column('v', 'k');
        }
        
        if (empty($res['localurl'])) $res['localurl'] = $siteurl;
        Config::set($res, 'sys');
        define('SYS_KEY', $res['syskey'] ?? '');

        $request->siteurl = $siteurl;
        $request->clientip = real_ip($res['ip_type'] ?? 0);
        $request->isadmin = false;
        $request->islogin = false;

        if (config('app.dbversion') && config_get('version') < config('app.dbversion')) {
            $this->db_update();
        }

        if(empty(config_get('public_key'))){
            $key_pair = generate_key_pair();
            if($key_pair){
                config_set('public_key', $key_pair['public_key']);
                config_set('private_key', $key_pair['private_key']);
                Cache::delete('configs');
                unset($key_pair);
            }
        }

        \app\lib\AddonManager::boot();

        return $next($request);
    }

    /**
     * 数据库更新
     */
    private function db_update()
    {
        if (config_get('version') < 2044) {
            $this->executeSqlFile('update2.sql');
        }
        if (config_get('version') < 2054) {
            $this->executeSqlFile('update3.sql');
        }
        if (config_get('version') < 2059) {
            $this->executeSqlFile('update4.sql');
        }
        $this->executeSqlFile('update5.sql');
        Cache::clear();
        $this->migrateAdminUser();
        if (empty(config_get('site_id'))) {
            config_set('site_id', getSid());
        }
        config_set('version', config('app.dbversion'));
        Cache::clear();
    }

    private function executeSqlFile($name)
    {
        $sqlFile = app()->getBasePath() . 'sql' . DIRECTORY_SEPARATOR . $name;
        if (!file_exists($sqlFile)) return;
        $sql = file_get_contents($sqlFile);
        $mysql_prefix = config('database.connections.mysql.prefix', '');
        $sql = str_replace('`pre_', '`' . $mysql_prefix, $sql);
        foreach (parseSql($sql) as $statement) {
            if (!empty($statement)) {
                try {
                    Db::execute($statement);
                } catch (\Throwable $e) {
                }
            }
        }
    }

    /**
     * 从旧配置迁移管理员数据到adminuser表
     */
    private function migrateAdminUser()
    {
        try {
            $count = Db::name('adminuser')->count();
            if ($count > 0) return;
            $adminUser = config_get('admin_user');
            $adminPwd = config_get('admin_pwd', '123456');
            if (empty($adminUser)) return;
            $adminPaypwd = config_get('admin_paypwd', '123456');
            $totpOpen = config_get('totp_open');
            $totpSecret = config_get('totp_secret');
            $now = date('Y-m-d H:i:s');
            Db::name('adminuser')->insert([
                'username' => $adminUser,
                'password' => password_hash($adminPwd, PASSWORD_DEFAULT),
                'paypwd' => password_hash($adminPaypwd, PASSWORD_DEFAULT),
                'totp' => $totpOpen == 1 ? 1 : 0,
                'totp_secret' => $totpSecret ?: null,
                'addtime' => $now,
                'lasttime' => $now,
                'status' => 1,
                'super' => 1,
                'permission' => null,
            ]);
        } catch (\Throwable $e) {
        }
    }
}
