<?php

declare(strict_types=1);

namespace app\controller\applet;

use app\BaseController;
use app\common\PaymentContext;
use app\logic\PaymentLogic;
use Exception;
use think\facade\Db;

/**
 * 小程序聚合收银台接口
 */
class Cashier extends BaseController
{
    /**
     * 商户信息查询
     * GET /applet/cashier/merchant?merchant=xxx
     */
    public function merchant()
    {
        $merchant = $this->request->get('merchant', '', 'trim');
        if (empty($merchant)) {
            return json(['code' => -1, 'msg' => '参数不完整']);
        }

        $uid = authcode($merchant, 'DECODE', SYS_KEY);
        if (!$uid || !is_numeric($uid)) {
            return json(['code' => -1, 'msg' => '不支持的二维码类型']);
        }
        $uid = (int)$uid;

        $userrow = Db::name('user')
            ->where('uid', $uid)
            ->field('uid,gid,pay,cert,status,username,codename,qq,deposit')
            ->find();
        if (!$userrow || $userrow['status'] == 0 || $userrow['pay'] == 0) {
            return json(['code' => -1, 'msg' => '当前商户不存在或已被封禁']);
        }

        mergeGroupConfig($userrow['gid']);

        if ($userrow['pay'] == 2 && config_get('user_review') == 1) {
            return json(['code' => -1, 'msg' => '商户没通过审核，请联系官方客服进行审核']);
        }
        if (config_get('cert_force') == 1 && $userrow['cert'] == 0) {
            return json(['code' => -1, 'msg' => '当前商户未完成实名认证，无法收款']);
        }
        if (config_get('forceqq') == 1 && empty($userrow['qq'])) {
            return json(['code' => -1, 'msg' => '当前商户未填写联系QQ，无法收款']);
        }
        if (config_get('user_deposit') == 1 && config_get('user_deposit_min') > 0 && config_get('user_deposit_min') > $userrow['deposit']) {
            return json(['code' => -1, 'msg' => '商户保证金不足，无法收款']);
        }

        $codename = !empty($userrow['codename']) ? $userrow['codename'] : $userrow['username'];

        return json([
            'code' => 0,
            'data' => [
                'uid' => $uid,
                'merchant' => $merchant,
                'codename' => $codename,
            ]
        ]);
    }

    /**
     * 小程序下单
     * POST /applet/cashier/create
     * 支持 platform=wechat（默认）和 platform=alipay
     */
    public function create()
    {
        $merchant = $this->request->post('merchant', '', 'trim');
        $money = $this->request->post('money', '');
        $remark = $this->request->post('remark', '');
        $code = $this->request->post('code', '');
        $platform = $this->request->post('platform', 'wechat', 'trim');

        $isAlipay = ($platform === 'alipay');

        if (empty($merchant)) {
            return json(['code' => -1, 'msg' => '商户信息不能为空']);
        }
        if (empty($code)) {
            return json(['code' => -1, 'msg' => ($isAlipay ? '支付宝授权code不能为空' : '微信登录code不能为空')]);
        }

        $uid = authcode($merchant, 'DECODE', SYS_KEY);
        if (!$uid || !is_numeric($uid)) {
            return json(['code' => -1, 'msg' => '商户信息无效']);
        }
        $uid = (int)$uid;

        if ($money <= 0 || !is_numeric($money) || !preg_match('/^[0-9.]+$/', (string)$money)) {
            return json(['code' => -1, 'msg' => '金额不合法']);
        }
        if (!empty($remark)) {
            $remark = mb_substr(htmlspecialchars(filter_utf8mb4($remark)), 0, 30);
        }

        $clientip = $this->request->clientip;

        $userrow = Db::name('user')
            ->where('uid', $uid)
            ->field('uid,gid,money,mode,mode_feerate,ordername,ordername_rules,channelinfo,pay,cert,status,username,codename,pay_minmoney,pay_maxmoney,deposit')
            ->find();
        if (!$userrow || $userrow['status'] == 0 || $userrow['pay'] == 0) {
            return json(['code' => -1, 'msg' => '当前商户不存在或已被封禁']);
        }

        mergeGroupConfig($userrow['gid']);

        // 金额限制校验
        if ($userrow['pay_maxmoney'] > 0 && $money > $userrow['pay_maxmoney']) {
            return json(['code' => -1, 'msg' => '最大支付金额是' . $userrow['pay_maxmoney'] . '元']);
        }
        if ($userrow['pay_minmoney'] > 0 && $money < $userrow['pay_minmoney']) {
            return json(['code' => -1, 'msg' => '最小支付金额是' . $userrow['pay_minmoney'] . '元']);
        }
        if (config_get('pay_maxmoney') > 0 && $money > config_get('pay_maxmoney')) {
            return json(['code' => -1, 'msg' => '最大支付金额是' . config_get('pay_maxmoney') . '元']);
        }
        if (config_get('pay_minmoney') > 0 && $money < config_get('pay_minmoney')) {
            return json(['code' => -1, 'msg' => '最小支付金额是' . config_get('pay_minmoney') . '元']);
        }

        // 黑名单校验
        $blackip = Db::name('blacklist')->where(['type' => 1, 'content' => $clientip])->find();
        if ($blackip) return json(['code' => -1, 'msg' => config_get('blackmsg', '系统异常无法完成付款')]);

        // 日限额校验
        if (config_get('pay_daymax') > 0) {
            $daytotal = Db::name('order')->where('uid', $uid)->where('date', date('Y-m-d'))->where('status', '>', 0)->sum('money');
            if ($daytotal + $money > config_get('pay_daymax')) {
                return json(['code' => -1, 'msg' => '当前商户今日收款已达到限额']);
            }
        }

        // IP频率限制
        $ipWhite = config_get('pay_iplimit_white');
        $isIpWhitelisted = empty($ipWhite) ? false : (strpos($ipWhite, $clientip) !== false);

        $payIpInterval = (int)config_get('pay_ip_interval');
        if ($payIpInterval > 0 && !$isIpWhitelisted) {
            $lastAddtime = Db::name('order')->where('ip', $clientip)->order('addtime', 'desc')->value('addtime');
            if (!empty($lastAddtime)) {
                $diff = time() - strtotime($lastAddtime);
                if ($diff >= 0 && $diff < $payIpInterval) {
                    return json(['code' => -1, 'msg' => '下单频率过快，请' . ($payIpInterval - $diff) . '秒后再试']);
                }
            }
        }
        if (config_get('pay_iplimit') > 0 && !$isIpWhitelisted) {
            $ipcount = Db::name('order')->where('ip', $clientip)->where('date', date('Y-m-d'))->where('status', '>', 0)->count();
            if ($ipcount >= config_get('pay_iplimit')) {
                return json(['code' => -1, 'msg' => '你今天已无法再发起支付，请明天再试']);
            }
        }

        // 根据平台获取支付通道
        $channelType = $isAlipay ? 'alipay' : 'wxpay';
        $submitData = \app\lib\Channel::submit($channelType, $uid, $userrow['gid'], 0, 0, 0, true);
        if (!$submitData) {
            return json(['code' => -1, 'msg' => $isAlipay ? '当前无可用的支付宝支付通道' : '当前无可用的微信支付通道']);
        }

        $channelid = (int)$submitData['channel'];
        $subchannelid = (int)($submitData['subchannel'] ?? 0);
        $channel = $subchannelid > 0
            ? \app\lib\Channel::getSub($subchannelid)
            : \app\lib\Channel::get($channelid, $userrow['channelinfo']);
        if (!$channel) {
            return json(['code' => -1, 'msg' => '支付通道不存在']);
        }

        // 金额限制（通道级）
        $paymin = $submitData['paymin'] ?? 0;
        $paymax = $submitData['paymax'] ?? 0;
        if (!empty($paymin) && $paymin > 0 && $money < $paymin) {
            return json(['code' => -1, 'msg' => '最小支付金额是' . $paymin . '元']);
        }
        if (!empty($paymax) && $paymax > 0 && $money > $paymax) {
            return json(['code' => -1, 'msg' => '最大支付金额是' . $paymax . '元']);
        }

        // 频率限制
        $rateLimitSeconds = (int)($submitData['rate_limit_seconds'] ?? 0);
        $rateLimitCount = (int)($submitData['rate_limit_count'] ?? 0);
        if ($rateLimitSeconds > 0 && $rateLimitCount > 0) {
            $sinceTime = date('Y-m-d H:i:s', time() - $rateLimitSeconds);
            $recentCount = Db::name('order')->where('channel', $submitData['channel'])->where('addtime', '>=', $sinceTime)->count();
            if ($recentCount >= $rateLimitCount) {
                return json(['code' => -1, 'msg' => '当前支付通道繁忙，请稍后再试']);
            }
        }

        // 根据平台换取用户标识
        if ($isAlipay) {
            try {
                [$alipayAppId, $userType, $userId] = alipay_mini_oauth('', $code);
                $openid = $userId;
            } catch (Exception $e) {
                return json(['code' => -1, 'msg' => $e->getMessage()]);
            }
        } else {
            if (empty($channel['appwxa'])) {
                return json(['code' => -1, 'msg' => '支付通道未配置微信小程序']);
            }
            $wxinfo = \app\lib\Channel::getWeixin($channel['appwxa']);
            if (!$wxinfo) {
                return json(['code' => -1, 'msg' => '支付通道绑定的微信小程序不存在']);
            }
            try {
                $oauth = new \app\lib\wechat\WechatOauth($wxinfo['appid'], $wxinfo['appsecret']);
                $openid = $oauth->AppGetOpenid($code);
            } catch (Exception $e) {
                return json(['code' => -1, 'msg' => $e->getMessage()]);
            }
        }

        // 计算费率
        $rate = $submitData['rate'];
        $submitRate = ['rate' => (float)$rate, 'channel' => $channelid, 'payfee_lessthan' => $submitData['payfee_lessthan'] ?? null, 'payfee_mincost' => $submitData['payfee_mincost'] ?? null];
        if ($userrow['mode'] == 1) {
            [$realmoney, $getmoney] = PaymentLogic::calculateOrderFee($money, $submitRate, $userrow);
        } else {
            [$realmoney, $getmoney] = PaymentLogic::calculateOrderFee($money, $submitRate, array_merge($userrow, ['mode' => 0]));
        }

        if ($submitData['mode'] == 1 && $realmoney - $getmoney > $userrow['money']) {
            return json(['code' => -1, 'msg' => '当前商户余额不足，无法完成支付']);
        }

        [$realmoney, $getmoney] = PaymentLogic::getRandomPayMoney($realmoney, $getmoney, $submitData['mode']);

        // 创建订单
        $siteurl = $this->request->siteurl;
        $trade_no = date("YmdHis") . rand(11111, 99999);
        $return_url = $siteurl . 'paypage/success?trade_no=' . $trade_no;
        $domain = getdomain($return_url);

        $insertResult = Db::name('order')->insert([
            'trade_no' => $trade_no,
            'out_trade_no' => $trade_no,
            'uid' => $uid,
            'tid' => 3,
            'type' => $submitData['typeid'],
            'channel' => $channelid,
            'subchannel' => $subchannelid,
            'addtime' => date('Y-m-d H:i:s'),
            'date' => date('Y-m-d'),
            'name' => '在线收款',
            'money' => $money,
            'realmoney' => $realmoney,
            'getmoney' => $getmoney,
            'notify_url' => $return_url,
            'return_url' => $return_url,
            'param' => !empty($remark) ? $remark : null,
            'domain' => $domain,
            'ip' => $clientip,
            'province' => PaymentLogic::getProvinceCode($clientip),
            'buyer' => $openid,
            'status' => 0,
        ]);
        if (!$insertResult) {
            return json(['code' => -1, 'msg' => '创建订单失败，请重试']);
        }
        
        $order = [
            'trade_no' => $trade_no,
            'out_trade_no' => $trade_no,
            'uid' => $uid,
            'addtime' => date('Y-m-d H:i:s'),
            'name' => '在线收款',
            'realmoney' => sprintf("%.2f", $realmoney),
            'type' => $submitData['typeid'],
            'channel' => $channelid,
            'subchannel' => $subchannelid,
            'typename' => $submitData['typename'] ?? $channelType,
            'sub_openid' => $openid,
            'is_applet' => 1,
        ];
        $order['profits'] = \app\logic\ProfitSharingLogic::updateOrderProfits($order, $submitData['plugin']);

        $ctx = new PaymentContext($order, '', 'jsapi', true, 'applet');
        try {
            $service = new \app\service\PaymentService($ctx, $userrow);
            $paydata = $service->execute('mapi', true);
        } catch (Exception $e) {
            exception_log($e, '小程序支付异常');
            return json(['code' => -1, 'msg' => $e->getMessage()]);
        }

        if ($paydata['type'] == 'jsapi') {
            $data = $paydata['data'];
            if (is_string($data) && str_starts_with($data, '{')) {
                $data = json_decode($data, true);
            }
            return json([
                'code' => 0,
                'trade_no' => $trade_no,
                'money' => sprintf("%.2f", $money),
                'paydata' => $data,
            ]);
        } elseif ($paydata['type'] == 'error') {
            return json(['code' => -1, 'msg' => $paydata['msg']]);
        } else {
            return json(['code' => -1, 'msg' => '当前支付通道不支持小程序支付']);
        }
    }

}
