<?php

declare (strict_types=1);

namespace app\middleware;

use think\facade\View;

class ViewOutput
{
    /**
     * 处理请求
     *
     * @param \think\Request $request
     * @param \Closure $next
     * @return Response
     */
    public function handle($request, \Closure $next)
    {
        View::assign('cdnpublic', get_cdn_public());
        View::assign('sitename', config_get('sitename'));
        return $next($request);
    }
}
