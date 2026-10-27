<?php

namespace app\lib;

use app\logic\SubOrderLogic;
use Exception;
use think\facade\Db;
use think\facade\Log;

class Payment
{

    //生成待签名字符串
    static private function getSignContent($data)
    {
        ksort($data);
        $signStr = '';
        foreach ($data as $k => $v) {
            if (is_array($v) || isNullOrEmpty($v) || $k == 'sign' || $k == 'sign_type') continue;
            $signStr .= $k . '=' . $v . '&';
        }
        $signStr = substr($signStr, 0, -1);
        return $signStr;
    }

    //生成签名
    public static function makeSign($data, $md5key)
    {
        $sign_type = isset($data['sign_type']) ? $data['sign_type'] : 'MD5';
        $signStr = self::getSignContent($data);
        if ($sign_type == 'RSA') {
            $private_key = base64ToPem(config_get('private_key'), 'PRIVATE KEY');
            $pkey = openssl_pkey_get_private($private_key);
            if (!$pkey) return false;
            openssl_sign($signStr, $sign, $pkey, OPENSSL_ALGO_SHA256);
            return base64_encode($sign);
        } else {
            $sign = md5($signStr . $md5key);
            return $sign;
        }
    }

    //验证签名
    public static function verifySign($data, $md5key, $publicKey)
    {
        if (!isset($data['sign'])) throw new Exception('缺少签名参数');
        $sign_type = isset($data['sign_type']) ? $data['sign_type'] : 'MD5';
        if ($sign_type == 'RSA') {
            $public_key = base64ToPem($publicKey, 'PUBLIC KEY');
            $pkey = openssl_pkey_get_public($public_key);
            if (!$pkey) throw new Exception('签名校验失败，商户公钥错误');
            $signStr = self::getSignContent($data);
            $result = openssl_verify($signStr, base64_decode($data['sign']), $pkey, OPENSSL_ALGO_SHA256);
            return $result === 1;
        } else {
            $sign = self::makeSign($data, $md5key);
            return $sign === $data['sign'];
        }
    }

    //支付宝直付通确认结算
    public static function alipaydSettle($channel, $order, bool $freeze = false)
    {
        $alipay_config = \app\common\PaymentConfig::getAlipayConfig($channel);
        $alipaySevice = new \Alipay\AlipayTradeService($alipay_config);
        if ($order['combine'] == 1) {
            $sub_orders = SubOrderLogic::getSubOrders($order['trade_no']);
            if (empty($sub_orders)) throw new Exception('子订单数据不存在');
            $failnum = 0;
            $errmsg = '';
            foreach ($sub_orders as $sub_order) {
                if ($sub_order['settle'] == 0) {
                    $settle = 0;
                    try {
                        $alipaySevice->settle_confirm($sub_order['api_trade_no'], $sub_order['money'], $freeze);
                        $settle = 1;
                    } catch (Exception $e) {
                        if (strpos($e->getMessage(), 'ALREADY_CONFIRM_SETTLE') !== false) {
                            $settle = 1;
                        } else {
                            $failnum++;
                            $errmsg .= $e->getMessage() . ',';
                        }
                    }
                    if ($settle == 1) SubOrderLogic::updateSubOrderSettle($sub_order['sub_trade_no'], 1);
                }
            }
            if ($failnum > 0) throw new Exception('部分子单结算失败，失败数量：' . $failnum . '，失败原因：' . $errmsg);
            return true;
        }
        try {
            $alipaySevice->settle_confirm($order['api_trade_no'], $order['realmoney'], $freeze);
            return true;
        } catch (Exception $e) {
            if (strpos($e->getMessage(), 'ALREADY_CONFIRM_SETTLE') !== false) {
                return true;
            } else {
                throw $e;
            }
        }
    }

    //支付宝直付通结算失败处理
    public static function alipayd_settle_fail($channel, $order, $failmsg)
    {
        Log::alert('[支付宝直付通结算失败][' . $order['trade_no'] . '][' . $channel['id'] . ']' . $failmsg);
        if (config_get('alipay_settle_check') == 1) {
            if (!empty($channel['subid'])) {
                if (Db::name('subchannel')->where('id', $channel['subid'])->update(['status' => 0])) {
                    if (config_get('alipay_settle_notice') == 1) {
                        $title = config_get('sitename') . ' - 支付宝直付通结算失败提醒';
                        $content = '尊敬的管理员：支付通道“' . $channel['name'] . '”下的子通道“' . $channel['subname'] . '”因支付宝直付通结算失败（' . $failmsg . '），已被系统自动关闭！<br/>----------<br/>' . config_get('sitename') . '<br/>' . date('Y-m-d H:i:s');
                        MsgNotice::sendAdminMessage($title, $content);
                    }
                }
            } else {
                if (Db::name('channel')->where('id', $channel['id'])->update(['status' => 0])) {
                    if (config_get('alipay_settle_notice') == 1) {
                        $title = config_get('sitename') . ' - 支付宝直付通结算失败提醒';
                        $content = '尊敬的管理员：支付通道“' . $channel['name'] . '”因支付宝直付通结算失败（' . $failmsg . '），已被系统自动关闭！<br/>----------<br/>' . config_get('sitename') . '<br/>' . date('Y-m-d H:i:s');
                        MsgNotice::sendAdminMessage($title, $content);
                    }
                }
            }
        }
    }

    //微信收付通确认结算
    public static function wxpaynpSettle($channel, $order)
    {
        $wechatpay_config = \app\common\PaymentConfig::getWxpayV3Config($channel);
        if ($wechatpay_config['ecommerce']) {
            if (!$order['profits']) {
                $client = new \WeChatPay\V3\ProfitsharingService($wechatpay_config);
                if ($order['combine'] == 1) {
                    $sub_orders = SubOrderLogic::getSubOrders($order['trade_no']);
                    if (empty($sub_orders)) throw new Exception('子订单数据不存在');
                    $failnum = 0;
                    $errmsg = '';
                    foreach ($sub_orders as $sub_order) {
                        if ($sub_order['settle'] == 0) {
                            $settle = 0;
                            try {
                                $client->unfreeze($sub_order['sub_trade_no'], $sub_order['api_trade_no']);
                                $settle = 1;
                            } catch (Exception $e) {
                                $failnum++;
                                $errmsg .= $e->getMessage() . ',';
                            }
                            if ($settle == 1) SubOrderLogic::updateSubOrderSettle($sub_order['sub_trade_no'], 1);
                        }
                    }
                    if ($failnum > 0) throw new Exception('部分子单结算失败，失败数量：' . $failnum . '，失败原因：' . $errmsg);
                    return true;
                }
                return $client->unfreeze($order['trade_no'], $order['api_trade_no']);
            } else {
                throw new Exception('当前订单需要分账，请进入分账订单页面确认分账');
            }
        } else {
            throw new Exception('非平台收付通订单');
        }
    }

    //支付宝预授权资金支付
    public static function alipayPreAuthPay($channel, $order)
    {
        $alipay_config = \app\common\PaymentConfig::getAlipayConfig($channel);
        $alipaySevice = new \Alipay\AlipayTradeService($alipay_config);
        $trade_no = $order['trade_no'];
        $bizContent = [
            'out_order_no' => $trade_no,
            'out_request_no' => $trade_no,
        ];
        $result = $alipaySevice->preAuthQuery($bizContent);
        //print_r($result);exit;
        if (!is_array($result) || !isset($result['auth_no'])) throw new Exception('预授权订单查询失败');
        $rest_amount = $result['rest_amount'] ?? 0;
        $order_status = $result['order_status'] ?? '';
        if ($order_status == 'FINISH') {
            $result = $alipaySevice->query(null, $trade_no);
            return $result;
        } elseif ($order_status == 'AUTHORIZED') {
            if ($rest_amount == 0) throw new Exception('剩余冻结金额为0');
            $auth_no = $result['auth_no'];
            $ordername_key = config_get('ordername');
            $ordername_rules = config_get('ordername_rules');
            if ($channel && !empty($channel['ordername'])) {
                $ordername_key = $channel['ordername'];
            }
            if ($channel && !empty($channel['ordername_rules'])) {
                $ordername_rules = $channel['ordername_rules'];
            }
            $ordername = \app\logic\PaymentLogic::getOrdername(
                (string)($ordername_key ?: $order['name']),
                (string)$order['name'],
                (int)$order['uid'],
                (string)$trade_no,
                (string)$order['out_trade_no'],
                $order['realmoney'],
                $ordername_rules
            );
            $bizContent = [
                'out_trade_no' => $trade_no,
                'total_amount' => $rest_amount,
                'subject' => $ordername,
                'product_code' => 'PREAUTH_PAY',
                'auth_no' => $auth_no,
                'auth_confirm_mode' => 'COMPLETE'
            ];
            return $alipaySevice->scanPay($bizContent);
        } else {
            throw new Exception('该笔订单非已授权状态，无需支付');
        }
    }

    //支付宝预授权资金解冻
    public static function alipayUnfreeze($channel, $order)
    {
        $alipay_config = \app\common\PaymentConfig::getAlipayConfig($channel);
        $alipaySevice = new \Alipay\AlipayTradeService($alipay_config);
        $trade_no = $order['trade_no'];
        $bizContent = [
            'out_order_no' => $trade_no,
            'out_request_no' => $trade_no,
        ];
        $result = $alipaySevice->preAuthQuery($bizContent);
        //print_r($result);exit;
        if (!is_array($result) || !isset($result['auth_no'])) throw new Exception('预授权订单查询失败');
        $rest_amount = $result['rest_amount'] ?? 0;
        $order_status = $result['order_status'] ?? '';
        if ($rest_amount == 0) throw new Exception('剩余冻结金额为0');
        if ($order_status == 'AUTHORIZED') {
            $auth_no = $result['auth_no'];
            $bizContent = [
                'auth_no' => $auth_no,
                'out_request_no' => date("YmdHis") . rand(11111, 99999),
                'amount' => $rest_amount,
                'remark' => '解冻资金'
            ];
            return $alipaySevice->preAuthUnfreeze($bizContent);
        } else {
            throw new Exception('该笔订单非已授权状态，无需解冻');
        }
    }

    //支付宝红包转账
    public static function alipayRedPacketTransfer($channel, $payee_user_id, $money, $order_id)
    {
        $out_biz_no = date("YmdHis") . rand(11111, 99999);
        $alipay_config = \app\common\PaymentConfig::getAlipayConfig($channel);
        $alipaySevice = new \Alipay\AlipayTransferService($alipay_config);
        $alipaySevice->redPacketTansfer($out_biz_no, $money, $payee_user_id, config_get('sitename'), $order_id);
    }

    //支付宝红包资金退回
    public static function alipayRedPacketRefund($channel, $trade_no, $money)
    {
        $out_biz_no = date("YmdHis") . rand(11111, 99999);
        $alipay_config = \app\common\PaymentConfig::getAlipayConfig($channel);
        $alipaySevice = new \Alipay\AlipayTransferService($alipay_config);
        $alipaySevice->redPacketRefund($out_biz_no, $trade_no, $money);
    }

    //支付宝直付通&微信收付通延迟结算处理
    public static function settle_task(int $limit = 10)
    {
        $limit = max(1, $limit);
        $orders = Db::name('order')->alias('A')->join('channel B', 'A.channel=B.id')->where('A.status', 1)->where('A.settle', 1)->where('A.addtime', '<', date('Y-m-d H:i:s', strtotime('-24 hours')))->whereIn('B.plugin', ['alipayd', 'wxpaynp'])->order('A.trade_no', 'asc')->limit($limit)->field('A.*,B.plugin')->select();
        foreach ($orders as $row) {
            self::settle_item($row);
        }

        if (config_get('alipayd_settle_failretry') == 1) {
            $limit = 10;
            $orders = Db::name('order')->alias('A')->join('channel B', 'A.channel=B.id')->where('A.status', 1)->where('A.settle', 3)->where('A.addtime', '<', date('Y-m-d H:i:s', strtotime('-3 hours')))->where('A.addtime', '>', date('Y-m-d H:i:s', strtotime('-28 hours')))->where('B.plugin', 'alipayd')->order('A.trade_no', 'asc')->limit($limit)->field('A.*,B.plugin')->select();
            foreach ($orders as $row) {
                self::settle_item($row);
            }
        }
    }

    private static function settle_item(array $row): void
    {
        $trade_no = $row['trade_no'];
        $channel = $row['subchannel'] > 0 ? \app\lib\Channel::getSub($row['subchannel']) : \app\lib\Channel::get($row['channel'], Db::name('user')->where('uid', $row['uid'])->value('channelinfo'));
        if (!$channel) return;
        try {
            if ($row['plugin'] == 'alipayd') {
                self::alipaydSettle($channel, $row);
            } elseif ($row['plugin'] == 'wxpaynp') {
                self::wxpaynpSettle($channel, $row);
            }
            Db::name('order')->where('trade_no', $trade_no)->update(['settle' => 2]);
            echo '[延迟结算处理] ' . $trade_no . ' 结算成功'."\n";
        } catch (Exception $e) {
            exception_log($e, '延迟结算处理');
            $errmsg = $e->getMessage();
            if (strpos($errmsg, 'ALREADY_CONFIRM_SETTLE')) {
                Db::name('order')->where('trade_no', $trade_no)->update(['settle' => 2]);
                echo '[延迟结算处理] ' . $trade_no . ' 结算成功'."\n";
                return;
            }
            $status = $row['status'] == 3 ? 4 : 3;
            Db::name('order')->where('trade_no', $trade_no)->update(['settle' => $status]);
            echo '[延迟结算处理] ' . $trade_no . ' 结算失败,' . $errmsg . "\n";
            if ($row['plugin'] == 'alipayd' && strpos($errmsg, '系统繁忙') === false) {
                self::alipayd_settle_fail($channel, $row, $errmsg);
            }
        }
    }
}
