<?php

declare(strict_types=1);

namespace app\lib;

use app\common\BaseAddon;
use think\facade\Cache;
use think\facade\Db;

/**
 * 功能扩展类插件管理器
 */
class AddonManager
{
    /** @var array<string, callable[]> 已注册的 hook 回调 */
    private static array $hooks = [];

    /** @var array<string, BaseAddon> 已加载的插件实例 */
    private static array $instances = [];

    private static bool $booted = false;

    /**
     * 启动插件管理器，扫描并加载所有已启用的功能扩展插件
     */
    public static function boot(): void
    {
        if (self::$booted) return;
        self::$booted = true;

        try {
            self::loadEnabledAddons();
        } catch (\Throwable $e) {
            // 安装阶段或数据库不可用时静默失败
        }
    }

    private static function loadEnabledAddons(): void
    {
        $rows = Db::name('plugin')->where('type', 'addons')->cache('addons', 0)->column('name');
        foreach ($rows as $name) {
            try {
                $instance = self::loadAddonInstance($name);
                if (!$instance) continue;
                if (!self::isEnabled($name)) continue;
                self::$instances[$name] = $instance;
                $instance->boot();
            } catch (\Throwable $e) {
                continue;
            }
        }
    }

    private static function loadAddonInstance(string $name): ?BaseAddon
    {
        $className = 'plugins\\addons\\' . $name . '\\' . ucfirst($name) . 'Plugin';
        if (!class_exists($className)) return null;
        $instance = new $className();
        if (!($instance instanceof BaseAddon)) return null;
        return $instance;
    }

    /**
     * 判断指定插件是否已安装
     */
    public static function isInstalled(string $name): bool
    {
        return config_get('addon_' . $name . '_installed', '0') == '1';
    }

    /**
     * 判断指定插件是否已启用
     */
    public static function isEnabled(string $name): bool
    {
        return self::isInstalled($name) && config_get('addon_' . $name, '0') == '1';
    }

    /**
     * 获取指定插件实例
     */
    public static function getInstance(string $name): ?BaseAddon
    {
        self::boot();
        return self::$instances[$name] ?? null;
    }

    /**
     * 获取所有已启用的插件实例
     */
    public static function getEnabledAddons(): array
    {
        self::boot();
        return self::$instances;
    }

    // =========== Hook 系统 ===========

    /**
     * 注册 hook 回调
     * @param string $name hook 名称
     * @param callable $callback 回调函数
     * @param int $priority 优先级（数字越小越先执行）
     */
    public static function addHook(string $name, callable $callback, int $priority = 10): void
    {
        self::$hooks[$name][] = ['callback' => $callback, 'priority' => $priority];
    }

    /**
     * 执行 hook，调用所有注册的回调
     * @param string $name hook 名称
     * @param array $params 传递给回调的参数
     * @return array 所有回调的返回值
     */
    public static function executeHook(string $name, array $params = []): array
    {
        $results = [];
        if (!isset(self::$hooks[$name])) return $results;

        $hooks = self::$hooks[$name];
        usort($hooks, fn($a, $b) => $a['priority'] <=> $b['priority']);

        foreach ($hooks as $hook) {
            $results[] = call_user_func($hook['callback'], $params);
        }
        return $results;
    }

    /**
     * 判断某个 hook 是否已注册回调
     */
    public static function hasHook(string $name): bool
    {
        return !empty(self::$hooks[$name]);
    }

    // =========== 路由注册 ===========

    /**
     * 注册所有已启用插件的后台路由（在 admin 路由组内部调用）
     */
    public static function registerAdminRoutes(): void
    {
        self::boot();
        foreach (self::$instances as $addon) {
            $addon->registerAdminRoutes();
        }
    }

    /**
     * 注册所有已启用插件的用户端路由（在 user 路由组内部调用）
     */
    public static function registerUserRoutes(): void
    {
        self::boot();
        foreach (self::$instances as $addon) {
            $addon->registerUserRoutes();
        }
    }

    /**
     * 注册所有已启用插件的通用路由
     */
    public static function registerCommonRoutes(): void
    {
        self::boot();
        foreach (self::$instances as $addon) {
            $addon->registerCommonRoutes();
        }
    }

    // =========== 菜单注入 ===========

    /**
     * 获取所有已启用插件的后台菜单项（按 sort 排序）
     */
    public static function getAdminMenus(): array
    {
        self::boot();
        $menus = [];
        foreach (self::$instances as $addon) {
            $menus = array_merge($menus, $addon->getAdminMenus());
        }
        usort($menus, fn($a, $b) => ($a['sort'] ?? 0) <=> ($b['sort'] ?? 0));
        return $menus;
    }

    /**
     * 获取所有已启用插件的用户端菜单项（按 sort 排序）
     */
    public static function getUserMenus(): array
    {
        self::boot();
        $menus = [];
        foreach (self::$instances as $addon) {
            $menus = array_merge($menus, $addon->getUserMenus());
        }
        usort($menus, fn($a, $b) => ($a['sort'] ?? 0) <=> ($b['sort'] ?? 0));
        return $menus;
    }

    // =========== 插件管理 ===========

    /**
     * 扫描磁盘上所有可用的扩展目录
     */
    public static function getAvailableAddons(): array
    {
        $addonsDir = PLUGIN_ROOT . 'addons';
        if (!is_dir($addonsDir)) return [];

        $list = [];
        foreach (scandir($addonsDir) as $entry) {
            if ($entry === '.' || $entry === '..') continue;
            $infoFile = $addonsDir . DIRECTORY_SEPARATOR . $entry . DIRECTORY_SEPARATOR . 'info.json';
            if (!is_file($infoFile)) continue;
            $info = json_decode(file_get_contents($infoFile), true);
            if (!is_array($info)) continue;
            $list[$entry] = $info;
        }
        return $list;
    }

    /**
     * 获取所有功能扩展插件的信息（含未安装和未启用的）
     */
    public static function getAllAddons(): array
    {
        $rows = Db::name('plugin')->where('type', 'addons')->select()->toArray();
        $list = [];
        foreach ($rows as $row) {
            $name = $row['name'];
            $row['installed'] = self::isInstalled($name);
            $row['enabled'] = self::isEnabled($name);
            $list[$name] = $row;
        }
        return $list;
    }

    /**
     * 安装指定插件
     */
    public static function installAddon(string $name): bool
    {
        $infoFile = PLUGIN_ROOT . 'addons' . DIRECTORY_SEPARATOR . $name . DIRECTORY_SEPARATOR . 'info.json';
        if (is_file($infoFile)) {
            $info = json_decode(file_get_contents($infoFile), true);
            if (is_array($info) && isset($info['require_version']) && $info['require_version'] !== ''
                && (int)$info['require_version'] > (int)config('app.version')) {
                throw new \Exception('该扩展最低系统版本要求为' . $info['require_version'] . '，请先更新系统至最新版本');
            }
        }

        $instance = self::loadAddonInstance($name);
        if (!$instance) return false;
        $result = $instance->install();
        if (!$result) return false;
        config_set('addon_' . $name . '_installed', '1');
        Cache::delete('configs');
        return true;
    }

    /**
     * 卸载指定插件
     */
    public static function uninstallAddon(string $name): bool
    {
        $instance = self::$instances[$name] ?? null;
        if (!$instance) {
            $instance = self::loadAddonInstance($name);
            if (!$instance) return false;
        }
        $instance->disable();
        $result = $instance->uninstall();
        if (!$result) return false;
        config_set('addon_' . $name, null);
        config_set('addon_' . $name . '_installed', null);
        plugin_config_set('addons', $name, null);
        Cache::delete('configs');
        unset(self::$instances[$name]);
        return true;
    }

    /**
     * 启用指定插件
     */
    public static function enableAddon(string $name): bool
    {
        $instance = self::loadAddonInstance($name);
        if (!$instance) return false;
        $result = $instance->enable();
        if (!$result) return false;
        config_set('addon_' . $name, '1');
        Cache::delete('configs');
        return true;
    }

    /**
     * 禁用指定插件
     */
    public static function disableAddon(string $name): bool
    {
        $instance = self::loadAddonInstance($name);
        if (!$instance) return false;
        $result = $instance->disable();
        if (!$result) return false;
        config_set('addon_' . $name, '0');
        unset(self::$instances[$name]);
        Cache::delete('configs');
        return true;
    }

    /**
     * 获取指定插件的配置项定义
     */
    public static function getAddonInputs(string $name): array
    {
        $infoFile = PLUGIN_ROOT . 'addons' . DIRECTORY_SEPARATOR . $name . DIRECTORY_SEPARATOR . 'info.json';
        if (!is_file($infoFile)) return [];
        $info = json_decode(file_get_contents($infoFile), true);
        return $info['inputs'] ?? [];
    }


}
