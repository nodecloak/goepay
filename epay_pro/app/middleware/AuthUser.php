<?php
declare(strict_types=1);

namespace app\middleware;

use app\logic\AuthLogic;

class AuthUser
{
    public function handle($request, \Closure $next)
    {
        $state = AuthLogic::getUserState();
        if (!$state['islogin']) {
            if ($request->isAjax()) {
                return json_response(401, 'login required');
            }
            return redirect('./');
        }
        $request->islogin = true;
        $request->uid = $state['uid'];
        $request->userdata = $state['data'];
        return $next($request);
    }
}
