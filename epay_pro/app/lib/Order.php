<?php

namespace app\lib;

use think\facade\Db;

class Order
{
    public static function freeze($trade_no)
    {
        $row = Db::name('order')->where('trade_no', $trade_no)->field('uid,getmoney,status,channel')->find();
        if (!$row)
            return ['code' => -1, 'msg' => '当前订单不存在！'];
        if ($row['status'] != 1)
            return ['code' => -1, 'msg' => '只支持冻结已支付状态的订单'];
        $channel = \app\lib\Channel::get($row['channel']);
        if ($channel['mode'] == 1)
            return ['code' => -1, 'msg' => '当前支付通道为商户直清，不支持冻结'];
        if ($row['getmoney'] > 0) {
            changeUserMoney($row['uid'], $row['getmoney'], false, '订单冻结', $trade_no);
            Db::name('order')->where('trade_no', $trade_no)->update(['status' => 3]);
        }
        return ['code' => 0, 'msg' => '已成功从UID:' . $row['uid'] . '冻结' . $row['getmoney'] . '元余额'];
    }

    public static function unfreeze($trade_no)
    {
        $row = Db::name('order')->where('trade_no', $trade_no)->field('uid,getmoney,status,channel')->find();
        if (!$row)
            return ['code' => -1, 'msg' => '当前订单不存在！'];
        if ($row['status'] != 3)
            return ['code' => -1, 'msg' => '只支持解冻已冻结状态的订单'];
        $channel = \app\lib\Channel::get($row['channel']);
        if ($channel['mode'] == 1)
            return ['code' => -1, 'msg' => '当前支付通道为商户直清，不支持冻结'];
        if ($row['getmoney'] > 0) {
            changeUserMoney($row['uid'], $row['getmoney'], true, '订单解冻', $trade_no);
            Db::name('order')->where('trade_no', $trade_no)->update(['status' => 1]);
        }
        return ['code' => 0, 'msg' => '已成功为UID:' . $row['uid'] . '恢复' . $row['getmoney'] . '元余额'];
    }

    public static function refund_info($trade_no, $api = 0, $uid = 0)
    {
        $where = ['trade_no' => $trade_no];
        if ($uid > 0) $where['uid'] = $uid;
        $order = Db::name('order')->where($where)->find();
        if (!$order)
            return ['code' => -1, 'msg' => '当前订单不存在！'];
        if (!in_array($order['status'], [1, 2, 3]))
            return ['code' => -1, 'msg' => '该订单状态不支持退款！'];
        if ($order['status'] == 2 && empty($order['refundmoney'])) return ['code' => -1, 'msg' => '该订单已退款！'];
        if ($order['status'] == 2 && $order['refundmoney'] > 0 && $order['refundmoney'] >= $order['realmoney']) return ['code' => -1, 'msg' => '该订单已全额退款！'];
        $money = !empty($order['refundmoney']) ? round((float)$order['realmoney'] - (float)$order['refundmoney'], 2) : $order['realmoney'];

        if ($api == 1) {
            if (!$order['api_trade_no']) return ['code' => -1, 'msg' => '接口订单号不存在'];
            $channel = \app\lib\Channel::get($order['channel']);
            if (!$channel) return ['code' => -1, 'msg' => '当前支付通道信息不存在'];
            if (\app\lib\Plugin::isrefund($channel['plugin']) == false) {
                return ['code' => -1, 'msg' => '当前支付通道不支持API退款'];
            }
        }

        return ['code' => 0, 'money' => $money];
    }

    public static function refund($refund_no, $trade_no, $money, $api = 0, $uid = 0, $out_refund_no = null, $noreduce = false, $send_event = true)
    {
        $where = ['trade_no' => $trade_no];
        if ($uid > 0) $where['uid'] = $uid;
        $order = Db::name('order')->where($where)->find();
        if (!$order)
            return ['code' => -1, 'msg' => '当前订单不存在！'];
        $eventUid = (int)$order['uid'];
        if (!in_array($order['status'], [1, 2, 3]))
            return ['code' => -1, 'msg' => '该订单状态不支持退款！'];
        if (empty($money)) $money = (float)$order['realmoney'];
        if ($money > (float)$order['realmoney']) return ['code' => -1, 'msg' => '退款金额不能大于订单金额'];
        if (!$order['api_trade_no']) return ['code' => -1, 'msg' => '接口订单号不存在'];
        if ($order['status'] == 2 && empty($order['refundmoney'])) return ['code' => -1, 'msg' => '该订单已退款！'];
        if ($order['status'] == 2 && $order['refundmoney'] > 0 && $order['refundmoney'] >= $order['realmoney']) return ['code' => -1, 'msg' => '该订单已全额退款！'];
        if ($order['status'] == 2 && $order['refundmoney'] > 0 && $money > round((float)$order['realmoney'] - (float)$order['refundmoney'], 2) || $money > $order['realmoney']) return ['code' => -1, 'msg' => '退款金额不能超过该订单剩余可退款金额！'];
        $refunded = (float)$order['refundmoney'];

        if (!$out_refund_no) $out_refund_no = $refund_no;

        $mode = Db::name('channel')->where('id', $order['channel'])->value('mode');
        if ($noreduce || $order['status'] == 3 || $mode == 1) {
            $reducemoney = 0;
        } elseif (config_get('refund_fee_type') == 1 && $money == $order['realmoney']) {
            $reducemoney = $order['realmoney'];
        } elseif (!config_get('refund_fee_type') && ($money == $order['realmoney'] || $money >= $order['getmoney'])) {
            $reducemoney = $order['getmoney'];
        } else {
            $reducemoney = $money;
        }

        if ($uid > 0 && $reducemoney > 0) {
            $usermoney = Db::name('user')->where('uid', $uid)->value('money');
            if ($reducemoney > $usermoney) {
                return ['code' => -1, 'msg' => '商户余额不足，请先充值'];
            }
        }

        if ($api == 1) {
            $order['refund_no'] = $refund_no;
            $order['refundmoney'] = sprintf("%.2f", $money);
            try {
                \app\lib\Plugin::refund($order);
            } catch (\Exception $e) {
                exception_log($e, '退款异常');
                return ['code' => -1, 'msg' => '退款失败：' . $e->getMessage()];
            }
        }
        if ($reducemoney > 0) {
            if ($order['tid'] == 2) {
                $param = json_decode($order['param'], true);
                if (isset($param['uid'])) {
                    $order['uid'] = $param['uid'];
                }
            }
            changeUserMoney($order['uid'], $reducemoney, false, '订单退款', $trade_no);
        }
        $refundmoney = !empty($refunded) ? round($refunded + $money, 2) : $money;
        if ($mode == 1 && !config_get('refund_fee_type') && $refundmoney >= $order['realmoney']) {
            $record = Db::name('record')->where('trade_no', $trade_no)->whereRaw("(type='订单服务费' OR type='在线收款服务费')")->find();
            if ($record) {
                $addmoney = $record['money'];
                changeUserMoney($order['uid'], $addmoney, true, '服务费退款', $trade_no);
            }
        }

        if ($api == 1) {
            Db::name('order')->where('trade_no', $trade_no)->update(['status' => 2, 'refundmoney' => $refundmoney]);
            Db::name('refundorder')->insert(['refund_no' => $refund_no, 'out_refund_no' => $out_refund_no, 'trade_no' => $trade_no, 'uid' => $order['uid'], 'money' => $money, 'reducemoney' => $reducemoney, 'addtime' => date('Y-m-d H:i:s'), 'endtime' => date('Y-m-d H:i:s'), 'status' => 1]);
        } else {
            Db::name('order')->where('trade_no', $trade_no)->update(['status' => 2]);
        }

        $result = ['code' => 0, 'refund_no' => $refund_no, 'out_refund_no' => $out_refund_no, 'trade_no' => $trade_no, 'out_trade_no' => $order['out_trade_no'], 'uid' => $order['uid'], 'money' => $money, 'refundmoney' => $refundmoney, 'reducemoney' => $reducemoney];
        if ($send_event) {
            try {
                \app\async\SendEvent::dispatch($eventUid, \app\async\SendEvent::EVENT_REFUND, $result);
            } catch (\Throwable $e) {
                exception_log($e, '发送订单退款事件');
            }
        }
        return $result;
    }

    public static function close($trade_no, $uid = 0)
    {
        $where = ['trade_no' => $trade_no];
        if ($uid > 0) $where['uid'] = $uid;
        $order = Db::name('order')->where($where)->find();
        if (!$order)
            return ['code' => -1, 'msg' => '当前订单不存在！'];
        if ($order['status'] != 0)
            return ['code' => -1, 'msg' => '该订单状态不支持关闭！'];

        try {
            \app\lib\Plugin::close($order);
        } catch (\Exception $e) {
            exception_log($e, '关闭订单异常');
            return ['code' => -1, 'msg' => $e->getMessage()];
        }
        return ['code' => 0];
    }
}
