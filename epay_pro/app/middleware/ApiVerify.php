<?php

declare(strict_types=1);

namespace app\middleware;

use Exception;
use think\facade\Db;
use think\Response;
use app\logic\ApiVerifyLogic;

/**
 * API接口签名校验中间件
 * 验证通过后将 userrow 和 queryArr 挂载到 request 上
 * 响应时自动对返回数据进行 RSA 签名
 */
class ApiVerify
{
    public function handle($request, \Closure $next): Response
    {
        $params = $request->post();
        if (empty($params)) {
            return json(['code' => -4, 'msg' => '未传入任何参数']);
        }

        $pid = intval($params['pid'] ?? 0);
        if (empty($pid)) {
            return json(['code' => -1, 'msg' => '商户ID不能为空']);
        }

        $userrow = Db::name('user')->where('uid', $pid)
            ->field('uid,gid,key,money,channelinfo,keytype,publickey,status,pay,settle,refund,transfer')
            ->find();
        if (!$userrow) {
            return json(['code' => -1, 'msg' => '商户不存在！']);
        }
        if ($userrow['status'] == 0) {
            return json(['code' => -1, 'msg' => '商户已被封禁']);
        }

        try {
            ApiVerifyLogic::verify($userrow, $params);
        } catch (Exception $e) {
            exception_log($e, 'API签名校验');
            return json(['code' => -3, 'msg' => $e->getMessage()]);
        }

        $request->uid = $pid;
        $request->userdata = $userrow;
        $request->params = $params;

        try {
            $response = $next($request);
        } catch (Exception $e) {
            $code = $e->getCode();
            exception_log($e, 'API接口异常');
            return json(['code' => $code != 0 ? $code : -1, 'msg' => $e->getMessage()]);
        }

        $data = $response->getData();
        if (is_array($data) && isset($data['code']) && $data['code'] == 0) {
            $data['timestamp'] = (string)time();
            $data['sign_type'] = 'RSA';
            $data['sign'] = \app\lib\Payment::makeSign($data, null);
            return json($data);
        }

        return $response;
    }
}
