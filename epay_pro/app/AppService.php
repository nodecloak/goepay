<?php

declare(strict_types=1);

namespace app;

use think\Service;

/**
 * 应用服务类
 */
class AppService extends Service
{
    // 服务注册
    public function register()
    {
        define('PASSWORD_HASH', '!@#%!s!0');
        define('PLUGIN_ROOT', app()->getRootPath() . 'plugins' . DIRECTORY_SEPARATOR);
        define('UPLOAD_ROOT', app()->getRootPath() . 'public' . DIRECTORY_SEPARATOR . 'upload' . DIRECTORY_SEPARATOR);
        define('TEMPLATE_ROOT', app()->getRootPath() . 'public' . DIRECTORY_SEPARATOR . 'template' . DIRECTORY_SEPARATOR);

        $adminPath = env('admin_path', 'admin');
        if ($adminPath !== 'admin' && is_dir(app()->getRootPath() . 'public' . DIRECTORY_SEPARATOR . 'admin')) {
            deleteDirectory(app()->getRootPath() . 'public' . DIRECTORY_SEPARATOR . $adminPath);
            rename(app()->getRootPath() . 'public' . DIRECTORY_SEPARATOR . 'admin', app()->getRootPath() . 'public' . DIRECTORY_SEPARATOR . $adminPath);
        }
    }

    // 服务启动
    public function boot()
    {

    }
}
