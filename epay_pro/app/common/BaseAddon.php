<?php

declare(strict_types=1);

namespace app\common;

use think\facade\Cache;
use think\facade\Db;

/**
 * 功能扩展类插件基类
 */
abstract class BaseAddon
{
    protected string $addonPath;
    protected string $addonName;

    public function __construct()
    {
        $this->addonName = static::getName();
        $this->addonPath = PLUGIN_ROOT . 'addons' . DIRECTORY_SEPARATOR . $this->addonName . DIRECTORY_SEPARATOR;
    }

    /**
     * 插件标识名（与目录名一致）
     */
    abstract public static function getName(): string;

    /**
     * 插件启动时执行（注册 hook 等）
     */
    public function boot(): void {}

    public function getAddonPath(): string
    {
        return $this->addonPath;
    }

    /**
     * 注册后台路由
     */
    public function registerAdminRoutes(): void {}

    /**
     * 注册用户端路由
     */
    public function registerUserRoutes(): void {}

    /**
     * 注册通用路由
     */
    public function registerCommonRoutes(): void {}
    
    /**
     * 返回后台菜单项（注入到 admin 菜单）
     * 统一格式：
     *   'parent'    => string 父级菜单 name，空串表示顶级菜单
     *   'sort'      => int    排序值（越小越靠前，多插件时排序用）
     *   'scriptUrl' => string 前端脚本 URL（可空）
     *   'menu'      => array  Vue router 菜单结构
     */
    public function getAdminMenus(): array
    {
        return [];
    }

    /**
     * 返回用户端菜单项（注入到 user 菜单）
     * 格式同 getAdminMenus
     */
    public function getUserMenus(): array
    {
        return [];
    }

    /**
     * 安装插件（执行安装 SQL）
     */
    public function install(): bool
    {
        $this->executeSqlFile($this->addonPath . 'install.sql');
        Cache::clear();
        return true;
    }

    /**
     * 卸载插件
     */
    public function uninstall(): bool
    {
        $this->executeSqlFile($this->addonPath . 'uninstall.sql');
        Cache::clear();
        return true;
    }

    /**
     * 更新插件（执行更新 SQL）
     */
    public function update(): bool
    {
        $this->executeSqlFile($this->addonPath . 'update.sql');
        Cache::clear();
        return true;
    }

    public function enable(): bool
    {
        return true;
    }

    public function disable(): bool
    {
        return true;
    }

    protected function executeSqlFile(string $sqlFile): void
    {
        if (!is_file($sqlFile)) return;
        $sql = file_get_contents($sqlFile);
        $prefix = config('database.connections.mysql.prefix', '');
        $sql = str_replace('pre_', $prefix, $sql);
        foreach (parseSql($sql) as $statement) {
            if (!empty($statement)) {
                try {
                    Db::execute($statement);
                } catch (\Throwable $e) {
                }
            }
        }
    }
}
