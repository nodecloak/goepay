<?php
declare(strict_types=1);

namespace app\middleware;

use app\logic\AuthLogic;

class AuthAdmin
{
    public function handle($request, \Closure $next)
    {
        $state = AuthLogic::getAdminState();
        if (!$state['islogin']) {
            if ($request->isAjax()) {
                return json_response(401, 'login required');
            }
            return redirect('./');
        }
        $request->isadmin = true;
        $request->adminUser = $state['data'];
        $request->adminUid = $state['uid'];
        return $next($request);
    }
}
