<?php

namespace app\common;

use think\facade\Db;
use think\facade\Log;

/**
 * 投诉处理插件基类
 */
abstract class BaseComplain implements ComplainInterface
{
    protected $channel;

    public function __construct(array $channel)
    {
        $this->channel = $channel;
    }

    //回调刷新单条投诉记录
    public function refreshNewInfo(string $thirdid, mixed $type = null): mixed
    {
        return false;
    }

    //获取投诉协商记录
    public function getNegotiationHistory(string $thirdid): mixed
    {
        return false;
    }

    //回复用户
    public function replySubmit(string $thirdid, string $content, array $images = []): mixed
    {
        return false;
    }

    //更新退款审批结果（仅微信）
    public function refundProgressSubmit(string $thirdid, string $code, string $content, ?string $remark = null, array $images = []): mixed
    {
        return false;
    }

    //处理完成（仅微信）
    public function complete(string $thirdid): mixed
    {
        return false;
    }

    //商家补充凭证（仅支付宝）
    public function supplementSubmit(string $thirdid, string $content, array $images = []): mixed
    {
        return false;
    }

    //下载图片（仅微信）
    public function getImage(string $media_id): mixed
    {
        return false;
    }

    //设置投诉通知回调地址
    public function setNotifyUrl(): mixed
    {
        return false;
    }

    //删除投诉通知回调地址
    public function delNotifyUrl(): mixed
    {
        return false;
    }

    protected static function getMimeType(string $ext): string
    {
        $mime_types = [
            'png' => 'image/png',
            'jpe' => 'image/jpeg',
            'jpeg' => 'image/jpeg',
            'jpg' => 'image/jpeg',
            'gif' => 'image/gif',
            'bmp' => 'image/bmp',
        ];
        return $mime_types[$ext] ?? 'application/octet-stream';
    }

    //自动处理逻辑
    protected static function autoHandle($trade_no, $status, $complaint_times = 1)
    {
        if (empty($trade_no)) return;
        if (is_array($trade_no)) {
            foreach ($trade_no as $item) {
                self::autoHandle($item, $status, $complaint_times);
            }
            return;
        }

        $order = Db::name('order')->where('trade_no', $trade_no)->field('uid,buyer,realmoney,status,ip,mobile,type')->find();
        if (!$order) return;
        if ($order['uid'] > 0) {
            $gid = Db::name('user')->where('uid', $order['uid'])->value('gid');
            mergeGroupConfig($gid);
        }

        //自动冻结订单
        if (config_get('complain_freeze_order') >= 1) {
            if ($status < 2) { //冻结订单
                \app\lib\Order::freeze($trade_no);
            } elseif ($status == 2 && config_get('complain_freeze_order') != 2) { //解冻订单
                \app\lib\Order::unfreeze($trade_no);
            }
        }

        //自动拉黑支付账号
        if ($status < 2 && config_get('complain_auto_black') == 1 && !empty($order['buyer'])) {
            if (!Db::name('blacklist')->where(['type' => 0, 'content' => $order['buyer']])->find()) {
                Db::name('blacklist')->insert(['type' => 0, 'content' => $order['buyer'], 'addtime' => date('Y-m-d H:i:s'), 'remark' => '投诉自动拉黑']);
            }
            if (!empty($order['mobile'])) {
                if (!Db::name('blacklist')->where(['type' => 0, 'content' => $order['mobile']])->find()) {
                    Db::name('blacklist')->insert(['type' => 0, 'content' => $order['mobile'], 'addtime' => date('Y-m-d H:i:s'), 'remark' => '投诉自动拉黑']);
                }
            }
        }
        if ($status < 2 && config_get('complain_auto_ipblack') > 0 && !empty($order['ip'])) {
            if (!Db::name('blacklist')->where(['type' => 1, 'content' => $order['ip']])->find()) {
                switch (config_get('complain_auto_ipblack')) {
                    case 1:
                        $endtime = date('Y-m-d H:i:s', strtotime('+3 days'));
                        break;
                    case 2:
                        $endtime = date('Y-m-d H:i:s', strtotime('+5 days'));
                        break;
                    case 3:
                        $endtime = date('Y-m-d H:i:s', strtotime('+7 days'));
                        break;
                    default:
                        $endtime = date('Y-m-d H:i:s', strtotime('+3 days'));
                        break;
                }
                Db::name('blacklist')->insert(['type' => 1, 'content' => $order['ip'], 'addtime' => date('Y-m-d H:i:s'), 'endtime' => $endtime, 'remark' => '投诉自动拉黑']);
            }
        }

        //自动退款
        if ($status == 0 && config_get('complain_auto_refund') == 1 && (empty(config_get('complain_auto_refund_money')) || config_get('complain_auto_refund_money') >= $order['realmoney']) && ($order['status'] == 1 || $order['status'] == 3)) {
            if ($order['type'] == 2 && config_get('complain_auto_refund_times', 1) > 1) {
                if ($complaint_times < config_get('complain_auto_refund_times', 1)) {
                    return;
                }
            }
            $noreduce = false;
            if (config_get('complain_auto_refund_nodeduct') == 1 && (empty(config_get('complain_auto_refund_nodeduct_money')) || config_get('complain_auto_refund_nodeduct_money') >= $order['realmoney'])) {
                $noreduce = true;
            }
            $refund_no = date('YmdHis') . rand(11111, 99999);
            $result = \app\lib\Order::refund($refund_no, $trade_no, $order['realmoney'], 1, 0, null, $noreduce);
            if ($result['code'] < 0 && isset($result['msg'])) {
                Log::error('投诉自动退款失败['.$trade_no.']：' . $result['msg']);
            }
        }
    }

    //发送投诉事件消息
    protected static function sendEventMessage($id): void
    {
        $row = Db::name('complain')->alias('A')
            ->leftJoin('order B', 'A.trade_no=B.trade_no')
            ->where('A.id', $id)
            ->field('A.*,B.money as order_money,B.name as order_name,B.status as order_status,B.trade_no AS platform_trade_no,B.out_trade_no')
            ->find();
        if (!$row || $row['uid'] <= 0) {
            return;
        }
        $paytypes = Db::name('type')->column('name', 'id');

        $content = [
            'id' => $row['id'],
            'source' => $row['source'],
            'pay_type' => $paytypes[$row['paytype']] ?? '',
            'out_trade_no' => $row['out_trade_no'],
            'trade_no' => $row['platform_trade_no'] ?: $row['trade_no'],
            'trade_no_list' => !empty($row['trade_no_list']) && str_contains($row['trade_no_list'], ',') ? explode(',', $row['trade_no_list']) : [],
            'problem_type' => $row['type'],
            'problem_description' => $row['title'],
            'complaint_content' => $row['content'],
            'user_complaint_times' => $row['times'],
            'status' => $row['status'],
            'phone' => $row['phone'],
            'addtime' => $row['addtime'],
            'edittime' => $row['edittime'],
            'money' => $row['money'],
            'order_money' => $row['money'],
            'thirdmchid' => $row['thirdmchid'],
            'order_name' => $row['order_name'],
            'order_status' => $row['order_status'],
        ];
        try {
            \app\async\SendEvent::dispatch(
                $row['uid'],
                \app\async\SendEvent::EVENT_COMPLAIN,
                $content
            );
        } catch (\Throwable $e) {
            exception_log($e, '发送交易投诉事件');
        }
    }

    //发送消息
    protected static function sendmsg($msgtype, $thirdid)
    {
        $row = Db::name('complain')->alias('A')->join('order B', 'A.trade_no=B.trade_no')->where('A.thirdid', $thirdid)->field('A.uid,A.trade_no,A.title,A.content,A.addtime,B.name as ordername,B.money')->find();
        if ($row) {
            \app\lib\MsgNotice::sendAsync('complain', $row['uid'], ['trade_no' => $row['trade_no'], 'title' => $row['title'], 'content' => $row['content'], 'type' => $msgtype, 'name' => $row['ordername'], 'money' => $row['money'], 'time' => $row['addtime']]);
        }
    }

    //自动回复
    protected function autoReply($thirdid, $status, $complaint_content = null)
    {
        $content = config_get('complain_auto_reply_con');
        $results = \app\lib\AddonManager::executeHook('complain_auto_reply', [
            'thirdid' => $thirdid,
            'status' => $status,
            'complaint_content' => $complaint_content,
            'channel' => $this->channel,
            'complain_handler' => $this,
        ]);
        foreach ($results as $result) {
            if (is_string($result) && trim($result) !== '') {
                $content = trim($result);
                break;
            }
        }
        if (empty($content)) return;
        usleep(300000);
        $this->feedbackSubmit($thirdid, '', $content);
    }

    //刷新最新投诉记录列表（多商户模式）
    protected function refreshNewListBySubchannels(int $num): array
    {
        $subchannelids = \app\lib\Channel::getSubChannelIdsByDays($this->channel['id']);
        $success = 0;
        $failed = 0;
        foreach ($subchannelids as $subchannelid) {
            $subchannel = \app\lib\Channel::getSub($subchannelid);
            if (!$subchannel) {
                continue;
            }
            if (isset($this->channel['source'])) $subchannel['source'] = $this->channel['source'];
            $model = \app\logic\ComplainLogic::getModel($subchannel);
            if (!$model) {
                continue;
            }
            try {
                $result = $model->refreshNewList($num);
                $success++;
                echo '[投诉同步] 子通道' . $subchannelid . '：' . $result['msg'] . "\n";
            } catch (\Throwable $e) {
                echo '[投诉同步] 子通道' . $subchannelid . '：' . $e->getMessage() . "\n";
                $failed++;
            }
        }
        return ['code' => 0, 'msg' => '成功' . $success . '个子通道，失败' . $failed . '个子通道'];
    }
}
