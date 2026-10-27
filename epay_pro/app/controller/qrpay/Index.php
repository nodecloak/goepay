<?php

declare(strict_types=1);

namespace app\controller\qrpay;

use app\BaseController;
use app\common\PaymentContext;
use app\logic\PaymentLogic;
use Exception;
use think\facade\Db;
use think\facade\Session;
use think\facade\View;
use think\Response;

/**
 * 商家聚合收款码页面
 */
class Index extends BaseController
{
    /**
     * 收款码首页（输入金额 + 选择支付方式）
     */
    public function index()
    {
        if ($this->request->has('ucode', 'get')) {
            $code = trim($this->request->get('ucode'));
            if (!preg_match('/^[a-zA-Z0-9]{1,32}$/', $code)) return $this->showAlert('参数错误');
            $uid = Db::name('onecode')->where('code', $code)->value('uid');
            if (!$uid) return $this->showAlert('当前码牌未绑定商户<br/>码牌编号：' . $code . '<br/><p class="weui-btn-area"><a href="/user/onecode.php?bind=' . $code . '" class="weui-btn weui-btn_primary">点此绑定</a></p>');
        } elseif ($this->request->has('merchant', 'get')) {
            $merchant = trim($this->request->get('merchant'));
            $uid = authcode($merchant, 'DECODE', SYS_KEY);
            if (!$uid || !is_numeric($uid)) return $this->showAlert('参数错误');
            $uid = (int)$uid;
        } elseif (Session::has('paypage_uid')) {
            $uid = (int)Session::get('paypage_uid');
        } else {
            return $this->showAlert('参数不完整');
        }

        $userrow = Db::name('user')
            ->where('uid', $uid)
            ->field('uid,gid,money,mode,pay,cert,status,username,channelinfo,qq,codename,deposit')
            ->find();
        if (!$userrow || $userrow['status'] == 0 || $userrow['pay'] == 0) {
            return $this->showAlert('当前商户不存在或已被封禁');
        }
        if ($userrow['pay'] == 2 && config_get('user_review') == 1) {
            return $this->showAlert('商户没通过审核，请联系官方客服进行审核');
        }

        mergeGroupConfig($userrow['gid']);

        if (config_get('cert_force') == 1 && $userrow['cert'] == 0) {
            return $this->showAlert('当前商户未完成实名认证，无法收款');
        }
        if (config_get('forceqq') == 1 && empty($userrow['qq'])) {
            return $this->showAlert('当前商户未填写联系QQ，无法收款');
        }
        if (config_get('user_deposit') == 1 && config_get('user_deposit_min') > 0 && config_get('user_deposit_min') > $userrow['deposit']) {
            return $this->showAlert('商户保证金不足，请前往支付平台充值保证金后再发起支付');
        }
        if (!empty(config_get('pay_region_block'))) {
            $ipregion = get_ip_city($this->request->clientip);
            if ($ipregion) {
                foreach (explode('|', config_get('pay_region_block')) as $rows) {
                    if (strpos($ipregion, $rows) !== false) {
                        return $this->showAlert('您所在的地区无法发起支付，请更换其他支付方式');
                    }
                }
            }
        }
        if (config_get('pay_block_oversea') == 1) {
            $ipregion = get_ip_region($this->request->clientip);
            if ($ipregion && $ipregion['country'] != '中国') {
                return $this->showAlert('您所在的地区无法发起支付，请更换其他支付方式');
            }
        }

        Session::set('paypage_uid', $uid);

        $direct = '0';
        $openId = '';
        $checktype = $this->checkPaytype();
        $type = $this->request->get('type', $checktype);
        $channel_id = intval($this->request->get('channel_id', 0));

        if ($type) {
            if (($this->request->has('code', 'get') || $this->request->has('auth_code', 'get') || $this->request->has('userAuthCode', 'get')) && Session::get('paypage_channel')) {
                $submitData = \app\lib\Channel::info(Session::get('paypage_channel'), $userrow['gid']);
                if (Session::get('paypage_subchannel') > 0) {
                    $submitData['subchannel'] = Session::get('paypage_subchannel');
                }
            } else {
                $submitData = \app\lib\Channel::submit($type, $uid, $userrow['gid'], 0, 0, $channel_id, checkmobile());
                if (!$submitData) return $this->showAlert('当前支付方式无法使用');
            }

            $channel = ($submitData['subchannel'] ?? 0) > 0
                ? \app\lib\Channel::getSub($submitData['subchannel'])
                : \app\lib\Channel::get($submitData['channel'], $userrow['channelinfo']);
            if (!$channel) return $this->showAlert('支付通道不存在');

            Session::set('paypage_typeid', $submitData['typeid']);
            Session::set('paypage_channel', $submitData['channel']);
            Session::set('paypage_subchannel', $submitData['subchannel'] ?? 0);
            Session::set('paypage_rate', $submitData['rate']);
            Session::set('paypage_paymax', $submitData['paymax']);
            Session::set('paypage_paymin', $submitData['paymin']);
            Session::set('paypage_mode', $submitData['mode']);
            Session::set('paypage_rate_limit_seconds', $submitData['rate_limit_seconds'] ?? 0);
            Session::set('paypage_rate_limit_count', $submitData['rate_limit_count'] ?? 0);
            Session::set('paypage_payfee_lessthan', $submitData['payfee_lessthan'] ?? null);
            Session::set('paypage_payfee_mincost', $submitData['payfee_mincost'] ?? null);

            $apptype = explode(',', (string) $channel['apptype']);

            if ($checktype == 'alipay' && $type == 'alipay' && $this->isAlipayDirect($submitData['plugin'], $apptype)) {
                if (config_get('alipay_web_login_all') == 1 && config_get('alipay_web_login') > 0
                    || !in_array($submitData['plugin'], ['alipay', 'alipaysl', 'alipayd', 'alipayn'])) {
                    if (!config_get('alipay_web_login')) return $this->showAlert('未配置支付宝网页快捷登录通道');
                    $channel = \app\lib\Channel::get(config_get('alipay_web_login'));
                }
                $openId = $this->getAlipayOpenId($channel);
                if ($openId instanceof Response) return $openId;
                $direct = '1';

            } elseif ($checktype == 'wxpay' && $type == 'wxpay' && $channel['appwxmp'] > 0 && $this->isWxpayDirect($submitData['plugin'], $apptype)) {
                $openId = $this->getWeixinOpenId($channel);
                if ($openId instanceof Response) return $openId;
                $direct = '1';

            } elseif ($checktype == 'bank' && $type == 'bank' && $this->isBankDirect($submitData['plugin'], $apptype)) {
                $openId = $this->getUnionpayOpenId($channel);
                if ($openId instanceof Response) return $openId;
                $direct = '1';

            } elseif ($checktype == 'qqpay' && $type == 'qqpay' && $this->isQqpayDirect($submitData['plugin'], $apptype)) {
                $direct = '1';
            } elseif ($checktype == 'douyinpay' && $type == 'douyinpay' && $this->isDouyinpayDirect($submitData['plugin'], $apptype)) {
                $openId = $this->getDouyinOpenId($channel);
                if ($openId instanceof Response) return $openId;
                $direct = '1';
            }
        }

        $money = $this->request->get('money');
        if ($money !== null && ($money <= 0 || !is_numeric($money) || !preg_match('/^[0-9.]+$/', (string)$money))) {
            $money = null;
        }
        $codename = !empty($userrow['codename']) ? $userrow['codename'] : $userrow['username'];
        $csrf_token = md5(mt_rand(0, 999) . time());
        Session::set('paypage_token', $csrf_token);

        View::assign([
            'uid' => $uid,
            'codename' => $codename,
            'type' => $type ?: '',
            'direct' => $direct,
            'openId' => $openId ?: '',
            'money' => $money,
            'csrf_token' => $csrf_token,
            'sitename' => config_get('sitename'),
            'cdnpublic' => get_cdn_public(),
        ]);
        $template_file = \app\lib\Template::getQrpayFilePath();
        return View::fetch($template_file);
    }

    /**
     * 支付AJAX提交
     */
    public function ajax()
    {
        if (!checkRefererHost()) return json(['code' => 403]);

        $uid = intval($this->request->post('uid', 0));
        $money = $this->request->post('money', '');
        $payer = $this->request->post('payer', '');
        $paytype = $this->request->post('paytype', '');
        $direct = intval($this->request->post('direct', 0));
        $param = $this->request->post('remark', '');
        if (!empty($param)) $param = htmlspecialchars(filter_utf8mb4($param));

        if ($this->request->post('token') != Session::get('paypage_token')) {
            return json(['code' => -1, 'msg' => 'CSRF TOKEN ERROR']);
        }
        if (!$uid || $uid != Session::get('paypage_uid')) {
            return json(['code' => -1, 'msg' => '收款方信息无效']);
        }
        if ($money <= 0 || !is_numeric($money) || !preg_match('/^[0-9.]+$/', (string)$money)) {
            return json(['code' => -1, 'msg' => '金额不合法']);
        }

        $clientip = $this->request->clientip;

        $blackip = Db::name('blacklist')->where(['type' => 1, 'content' => $clientip])->find();
        if ($blackip) return json(['code' => -1, 'msg' => config_get('blackmsg', '系统异常无法完成付款')]);
        if ($payer) {
            $black = Db::name('blacklist')->where(['type' => 0, 'content' => $payer])->find();
            if ($black) return json(['code' => -1, 'msg' => config_get('blackmsg', '系统异常无法完成付款')]);
        }

        if (!empty($paytype) && Session::has('paypage_typeid') && Session::has('paypage_paymax') && Session::has('paypage_paymin')) {
            $paymin = Session::get('paypage_paymin');
            $paymax = Session::get('paypage_paymax');
            if (!empty($paymin) && $paymin > 0 && $money < $paymin) {
                return json(['code' => -1, 'msg' => '当前支付通道最大支付金额是' . $paymin . '元']);
            }
            if (!empty($paymax) && $paymax > 0 && $money > $paymax) {
                return json(['code' => -1, 'msg' => '当前支付通道最小支付金额是' . $paymax . '元']);
            }
        }

        $userrow = Db::name('user')->where('uid', $uid)
            ->field('gid,mode,mode_feerate,ordername,ordername_rules,channelinfo,money,pay_minmoney,pay_maxmoney')
            ->find();

        if ($userrow['pay_maxmoney'] > 0 && $money > $userrow['pay_maxmoney']) {
            return json(['code' => -1, 'msg' => '最大支付金额是' . $userrow['pay_maxmoney'] . '元']);
        }
        if ($userrow['pay_minmoney'] > 0 && $money < $userrow['pay_minmoney']) {
            return json(['code' => -1, 'msg' => '最小支付金额是' . $userrow['pay_minmoney'] . '元']);
        }

        mergeGroupConfig($userrow['gid']);

        if (config_get('pay_maxmoney') > 0 && $money > config_get('pay_maxmoney')) {
            return json(['code' => -1, 'msg' => '最大支付金额是' . config_get('pay_maxmoney') . '元']);
        }
        if (config_get('pay_minmoney') > 0 && $money < config_get('pay_minmoney')) {
            return json(['code' => -1, 'msg' => '最小支付金额是' . config_get('pay_minmoney') . '元']);
        }

        if (config_get('pay_daymax') > 0) {
            $daytotal = Db::name('order')->where('uid', $uid)->where('date', date('Y-m-d'))->where('status', '>', 0)->sum('money');
            if ($daytotal + $money > config_get('pay_daymax')) {
                return json(['code' => -1, 'msg' => '当前商户今日收款已达到限额，无法发起支付']);
            }
        }

        $ipWhite = config_get('pay_iplimit_white');
        $isIpWhitelisted = empty($ipWhite) ? false : (strpos($ipWhite, $clientip) !== false);

        $payIpInterval = (int)config_get('pay_ip_interval');
        if ($payIpInterval > 0 && !$isIpWhitelisted) {
            $lastAddtime = Db::name('order')->where('ip', $clientip)->order('addtime', 'desc')->value('addtime');
            if (!empty($lastAddtime)) {
                $diff = time() - strtotime($lastAddtime);
                if ($diff >= 0 && $diff < $payIpInterval) {
                    $remain = $payIpInterval - $diff;
                    return json(['code' => -1, 'msg' => '你的下单频率过快，请' . $remain . '秒后再试']);
                }
            }
        }

        if (config_get('pay_iplimit') > 0 && !$isIpWhitelisted) {
            $ipcount = Db::name('order')->where('ip', $clientip)->where('date', date('Y-m-d'))->where('status', '>', 0)->count();
            if ($ipcount >= config_get('pay_iplimit')) {
                return json(['code' => -1, 'msg' => '你今天已无法再发起支付，请明天再试']);
            }
        }

        // 频率限制
        if (!empty($paytype) && Session::has('paypage_channel')) {
            $rateLimitSeconds = (int)Session::get('paypage_rate_limit_seconds', 0);
            $rateLimitCount = (int)Session::get('paypage_rate_limit_count', 0);
            if ($rateLimitSeconds > 0 && $rateLimitCount > 0) {
                $sinceTime = date('Y-m-d H:i:s', time() - $rateLimitSeconds);
                $recentCount = Db::name('order')->where('channel', (int)Session::get('paypage_channel'))->where('addtime', '>=', $sinceTime)->count();
                if ($recentCount >= $rateLimitCount) {
                    return json(['code' => -1, 'msg' => '当前支付通道繁忙，请稍后再试']);
                }
            }
        }

        $siteurl = $this->request->siteurl;
        $trade_no = date("YmdHis") . rand(11111, 99999);
        $return_url = $siteurl . 'paypage/success?trade_no=' . $trade_no;
        $domain = getdomain($return_url);

        $insertResult = Db::name('order')->insert([
            'trade_no' => $trade_no,
            'out_trade_no' => $trade_no,
            'uid' => $uid,
            'tid' => 3,
            'addtime' => date('Y-m-d H:i:s'),
            'date' => date('Y-m-d'),
            'name' => '在线收款',
            'money' => $money,
            'notify_url' => $return_url,
            'return_url' => $return_url,
            'param' => $param ?: null,
            'domain' => $domain,
            'ip' => $clientip,
            'province' => \app\logic\PaymentLogic::getProvinceCode($clientip),
            'buyer' => $payer ?: null,
            'status' => 0,
        ]);
        if (!$insertResult) return json(['code' => -1, 'msg' => '创建订单失败，请返回重试！']);

        Session::set('paypage_trade_no', $trade_no);

        $result = ['code' => 0, 'msg' => 'succ', 'trade_no' => $trade_no, 'direct' => $direct];

        if (!empty($paytype) && Session::has('paypage_typeid') && Session::has('paypage_channel') && Session::has('paypage_rate')) {
            $typeid = intval(Session::get('paypage_typeid'));
            $channelid = intval(Session::get('paypage_channel'));
            $subchannelid = intval(Session::get('paypage_subchannel'));

            if ($direct == 1) {
                $rate = Session::get('paypage_rate');
                $submitRate = ['rate' => (float) $rate, 'channel' => $channelid, 'payfee_lessthan' => Session::get('paypage_payfee_lessthan'), 'payfee_mincost' => Session::get('paypage_payfee_mincost')];
                if ($userrow['mode'] == 1) {
                    [$realmoney, $getmoney] = \app\logic\PaymentLogic::calculateOrderFee($money, $submitRate, $userrow);
                } else {
                    [$realmoney, $getmoney] = \app\logic\PaymentLogic::calculateOrderFee($money, $submitRate, array_merge($userrow, ['mode' => 0]));
                }
                if (Session::get('paypage_mode') == 1 && $realmoney - $getmoney > $userrow['money']) {
                    return json(['code' => -1, 'msg' => '当前商户余额不足，无法完成支付，请商户登录用户中心充值余额']);
                }

                [$realmoney, $getmoney] = PaymentLogic::getRandomPayMoney($realmoney, $getmoney, Session::get('paypage_mode'));

                $channelrow = Db::name('channel')->where('id', $channelid)->field('id,plugin')->find();
                if (!$channelrow) return json(['code' => -1, 'msg' => '支付通道不存在']);

                Db::name('order')->where('trade_no', $trade_no)->update([
                    'type' => $typeid,
                    'channel' => $channelid,
                    'subchannel' => $subchannelid,
                    'realmoney' => $realmoney,
                    'getmoney' => $getmoney,
                ]);

                $order = [
                    'trade_no' => $trade_no,
                    'out_trade_no' => $trade_no,
                    'uid' => $uid,
                    'addtime' => date('Y-m-d H:i:s'),
                    'name' => '在线收款',
                    'realmoney' => sprintf("%.2f", $realmoney),
                    'type' => $typeid,
                    'channel' => $channelid,
                    'subchannel' => $subchannelid,
                    'typename' => $paytype,
                    'sub_openid' => $payer,
                    'is_applet' => 0,
                ];
                $order['profits'] = \app\logic\ProfitSharingLogic::updateOrderProfits($order, $channelrow['plugin']);

                $ctx = new PaymentContext($order, '', 'jsapi', checkmobile(), getMobileClientType());
                try {
                    $service = new \app\service\PaymentService($ctx, $userrow);
                    $paydata = $service->execute('mapi', true);
                } catch (Exception $e) {
                    exception_log($e, '支付异常');
                    return json(['code' => -1, 'msg' => $e->getMessage()]);
                }

                if ($paydata['type'] == 'jsapi') {
                    if (is_array($paydata['data'])) $paydata['data'] = json_encode($paydata['data']);
                    $result['paydata'] = $paydata['data'];

                    if ($channelrow['plugin'] == 'douyinpay') {
                        $channel = $subchannelid > 0 ? \app\lib\Channel::getSub($subchannelid) : \app\lib\Channel::get($channelid);
                        if (!$channel) return json(['code' => -1, 'msg' => '支付通道不存在']);
                        $oauth = new \app\lib\douyinpay\DouyinOauth($channel['appid'], $channel['appsecret']);
                        $url = rtrim($siteurl, '/').$_SERVER['REQUEST_URI'];
                        try {
                            $sdkConfig = json_encode($oauth->getSdkConfig($url), JSON_UNESCAPED_SLASHES);
                        } catch (Exception $e) {
                            return json(['code' => -1, 'msg' => $e->getMessage()]);
                        }
                        $result['sdk_config'] = $sdkConfig;
                    }
                    
                } elseif ($paydata['type'] == 'error') {
                    return json(['code' => -1, 'msg' => $paydata['msg']]);
                } else {
                    $result['direct'] = 0;
                    $result['url'] = $paydata['url'];
                }
            } else {
                $result['url'] = '/payment/submit?typeid=' . $typeid . '&trade_no=' . $trade_no . '&channel_id=' . $subchannelid;
            }
        } else {
            $result['url'] = '/payment/cashier?trade_no=' . $trade_no;
        }

        return json($result);
    }

    /**
     * 支付成功页面
     */
    public function success()
    {
        $trade_no = $this->request->get('trade_no', '');
        $row = Db::name('order')->where('trade_no', $trade_no)->find();
        if (!$row) return $this->showAlert('订单号不存在');
        if ($row['status'] != 1) return $this->showAlert('订单未完成支付');
        if (!Session::has('paypage_trade_no') || Session::get('paypage_trade_no') != $trade_no) {
            return $this->showAlert('订单校验失败');
        }
        $userrow = Db::name('user')->where('uid', $row['uid'])->field('codename,username')->find();
        $codename = !empty($userrow['codename']) ? $userrow['codename'] : $userrow['username'];

        View::assign([
            'row' => $row,
            'trade_no' => $trade_no,
            'codename' => $codename,
            'sitename' => config_get('sitename'),
            'cdnpublic' => get_cdn_public(),
        ]);
        $template_file = \app\lib\Template::getQrpayFilePath('success');
        return View::fetch($template_file);
    }

    /**
     * 检测客户端支付类型（UA 判断）
     */
    private function checkPaytype(): ?string
    {
        if (checkwechat()) return 'wxpay';
        elseif (checkalipay()) return 'alipay';
        elseif (checkmobileqq()) return 'qqpay';
        elseif (checkunionpay()) return 'bank';
        elseif (checkdouyin()) return 'douyinpay';
        else return null;
    }

    /**
     * 获取支付宝 OpenID
     * @return string|Response
     */
    private function getAlipayOpenId(array $channel): string|Response
    {
        $alipay_config = \app\common\PaymentConfig::getAlipayConfig($channel);
        try {
            [$user_type, $user_id] = alipay_oauth($alipay_config);
        } catch (Exception $e) {
            return $this->showAlert($e->getMessage());
        }
        return $user_id;
    }

    /**
     * 获取微信 OpenID
     * @return string|Response
     */
    private function getWeixinOpenId(array $channel): string|Response
    {
        $wxinfo = \app\lib\Channel::getWeixin($channel['appwxmp']);
        if (!$wxinfo) return $this->showAlert('支付通道绑定的微信公众号不存在');
        try {
            $openId = wechat_oauth($wxinfo);
        } catch (Exception $e) {
            exception_log($e, 'wechat_oauth');
            return $this->showAlert($e->getMessage());
        }
        return $openId;
    }

    /**
     * 获取抖音 OpenID
     * @return string|Response
     */
    private function getDouyinOpenId(array $channel): string|Response
    {
        $oauth = new \app\lib\douyinpay\DouyinOauth($channel['appid'], $channel['appsecret']);
        $redirect_uri = (is_https() ? 'https://' : 'http://') . request()->host() . request()->baseUrl();
        try {
            $openId = $oauth->get_openid($redirect_uri);
        } catch (Exception $e) {
            exception_log($e, 'douyin_oauth');
            return $this->showAlert($e->getMessage());
        }
        return $openId;
    }

    /**
     * 获取银联 OpenID
     * @return string|Response
     */
    private function getUnionpayOpenId(array $channel): string|Response
    {
        if ($this->request->has('respCode', 'get')) {
            $respCode = $this->request->get('respCode');
            if ($respCode == '00' && $this->request->has('userAuthCode', 'get')) {
                try {
                    $result = \app\lib\Plugin::callPay($channel, 'get_unionpay_userid', $this->request->get('userAuthCode'));
                    if ($result['code'] == 0) {
                        return $result['data'];
                    } else {
                        return $this->showAlert('银联用户标识获取失败：' . ($result['msg'] ?? ''));
                    }
                } catch (Exception $e) {
                    exception_log($e, 'get_unionpay_userid');
                    return $this->showAlert('银联用户标识获取失败：' . $e->getMessage());
                }
            } elseif ($respCode == '34') {
                return '';
            } else {
                return $this->showAlert('银联用户标识获取失败，respCode=' . $respCode);
            }
        } else {
            $redirect_uri = (is_https() ? 'https://' : 'http://') . $this->request->host() . $this->request->url();
            $url = 'https://qr.95516.com/qrcGtwWeb-web/api/userAuth?version=1.0.0&redirectUrl=' . urlencode($redirect_uri);
            return redirect($url);
        }
    }

    /**
     * 判断支付宝是否走直接支付（JSAPI）
     */
    private function isAlipayDirect(string $plugin, array $apptype): bool
    {
        $rules = [
            'alipay' => '4', 'alipaysl' => '4', 'alipayd' => '4',
            'lakala' => '2', 'huifu' => '4', 'xsy' => '2', 'baofu' => '2',
            'adapay' => '2', 'allinpay' => '2', 'dinpay' => '3', 'duolabao' => '2',
            'fubei' => null, 'fuiou2' => '2', 'haipay' => '2', 'hlpay' => '2',
            'huishouqian' => '2', 'jindd' => '2', 'jlpay' => '2', 'joinpay' => '3',
            'leshua' => '2', 'llianpay' => '2', 'sandpay' => '2', 'shengpay' => '4',
            'suixingpay' => '2', 'unionpay' => '2', 'ysepay' => '3', 'yseqt' => '2',
            'yeepay' => '2', 'xingyifu' => '2', 'hmpay' => '2', 'kunpeng' => '2',
            'easypay' => '2', 'easypay2' => '2',
        ];
        if (!isset($rules[$plugin])) return false;
        return $rules[$plugin] === null || in_array($rules[$plugin], $apptype);
    }

    /**
     * 判断微信是否走直接支付（JSAPI）
     */
    private function isWxpayDirect(string $plugin, array $apptype): bool
    {
        $rules = [
            'wxpay' => '2', 'wxpaysl' => '2', 'wxpayn' => '2', 'wxpaynp' => '2',
            'lakala' => null, 'huifu' => '1', 'xsy' => null, 'baofu' => '2',
            'adapay' => '1', 'allinpay' => '2', 'dinpay' => '3', 'duolabao' => '2',
            'fubei' => null, 'fuiou2' => '2', 'haipay' => null, 'hlpay' => '2',
            'huishouqian' => '2', 'jindd' => '1', 'jlpay' => '2', 'joinpay' => '3',
            'leshua' => '2', 'llianpay' => '2', 'passpay' => '2', 'sandpay' => '2',
            'shengpay' => '1', 'suixingpay' => '2', 'unionpay' => '2', 'ysepay' => '2',
            'yseqt' => '3', 'yeepay' => '2', 'xingyifu' => '2', 'hmpay' => '2', 'kunpeng' => '1',
            'easypay' => '2', 'easypay2' => '2',
        ];
        if (!isset($rules[$plugin])) return false;
        return $rules[$plugin] === null || in_array($rules[$plugin], $apptype);
    }

    /**
     * 判断QQ是否走直接支付
     */
    private function isQqpayDirect(string $plugin, array $apptype): bool
    {
        $rules = [
            'qqpay' => '2', 'wxpayn' => '2', 'wxpaynp' => '2',
        ];
        if (!isset($rules[$plugin])) return false;
        return in_array($rules[$plugin], $apptype);
    }

    /**
     * 判断银联是否走直接支付
     */
    private function isBankDirect(string $plugin, array $apptype): bool
    {
        $rules = [
            'lakala' => '2', 'huifu' => '4', 'xsy' => '2', 'baofu' => '2',
            'allinpay' => '2', 'jlpay' => '2', 'yseqt' => '2',
        ];
        if (!isset($rules[$plugin])) return false;
        return in_array($rules[$plugin], $apptype);
    }

    /**
     * 判断抖音是否走直接支付
     */
    private function isDouyinpayDirect(string $plugin, array $apptype): bool
    {
        $rules = [
            'douyinpay' => '2',
        ];
        if (!isset($rules[$plugin])) return false;
        return in_array($rules[$plugin], $apptype);
    }
}
