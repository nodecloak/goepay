<?php

declare(strict_types=1);

namespace app\middleware;

/**
 * 用户中心 Referer 检查
 */
class UserRefererCheck
{
    public function handle($request, \Closure $next)
    {
        if (!checkRefererHost() && !checkwechat()) {
            if ($request->isAjax()) {
                return json_response(403, 'Forbidden');
            }
            return response('Forbidden', 403);
        }
        return $next($request);
    }
}
