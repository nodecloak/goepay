<?php

declare(strict_types=1);

namespace app\cron;

use app\common\AbstractCronTask;
use app\lib\Channel;
use app\logic\TransferLogic;
use think\facade\Db;

class Transfer extends AbstractCronTask
{
    public static function identifier(): string
    {
        return 'transfer';
    }

    public static function name(): string
    {
        return '商户余额自动转账';
    }

    public function execute(array $params = []): string
    {
        if (!config_get('auto_settle_money')) {
            throw new \Exception('未开启自动结算转账功能');
        }
        if (!config_get('transfer_alipay')) {
            throw new \Exception('未设置支付宝转账接口通道');
        }

        $minimum = (float)config_get('auto_settle_money');
        $success = 0;
        $users = Db::name('user')->where('status', 1)->where('settle', 1)
            ->where('settle_id', 1)->where('money', '>', $minimum)
            ->order('uid', 'desc')->limit(5)->select()->toArray();

        foreach ($users as $user) {
            $rate = (float)config_get('settle_rate', 0);
            $group = getGroupConfig($user['gid']);
            if (isset($group['settle_open']) && $group['settle_open'] == 2) {
                continue;
            }
            if (isset($group['settle_rate']) && $group['settle_rate'] !== '' && $group['settle_rate'] !== null) {
                $rate = (float)$group['settle_rate'];
            }

            $realMoney = (float)$user['money'];
            if ($rate > 0) {
                $fee = round($realMoney * $rate / 100, 2);
                $feeMax = (float)config_get('settle_fee_max', 0);
                if ($feeMax > 0 && $fee > $feeMax) {
                    $fee = $feeMax;
                }
                $realMoney -= $fee;
            }

            $businessNo = date('YmdHis') . random_int(11111, 99999);
            $channel = Channel::get((int)config_get('transfer_alipay'));
            $result = TransferLogic::submit(
                'alipay',
                $channel,
                $businessNo,
                $user['account'],
                $user['username'],
                $realMoney
            );

            if ($result['code'] == 0) {
                if (Db::name('settle')->insert([
                    'uid' => $user['uid'],
                    'type' => $user['settle_id'],
                    'account' => $user['account'],
                    'username' => $user['username'],
                    'money' => $user['money'],
                    'realmoney' => $realMoney,
                    'addtime' => date('Y-m-d H:i:s'),
                    'endtime' => date('Y-m-d H:i:s'),
                    'status' => 1,
                    'transfer_no' => $businessNo,
                    'transfer_channel' => config_get('transfer_alipay'),
                    'transfer_status' => 1,
                    'transfer_result' => $result['orderid'] ?? '',
                    'transfer_date' => $result['paydate'] ?? '',
                ])) {
                    $success++;
                    changeUserMoney($user['uid'], (float)$user['money'], false, '自动结算');
                    echo '[余额自动转账] 商户' . $user['uid'] . '成功结算' . $realMoney . '元，交易号：'
                        . ($result['orderid'] ?? '') . "\n";
                } else {
                    echo '[余额自动转账] 商户' . $user['uid'] . '成功结算' . $realMoney . '元，但记录插入失败'."\n";
                }
                continue;
            }

            echo '[余额自动转账] 商户' . $user['uid'] . '结算' . $realMoney . '元失败：' . $result['msg'] . "\n";
            if (!in_array($result['errcode'] ?? '', TransferLogic::$payee_err_code)) {
                Db::name('channel')->where('id', $channel['id'])->update(['status' => 0]);
                echo '[余额自动转账] 已关闭通道:' . $channel['name'] . "\n";
                $siteName = config_get('sitename');
                $title = $siteName . ' - 支付通道自动关闭提醒';
                $content = '尊敬的管理员：支付通道"' . $channel['name']
                    . '"因自动结算转账失败，已被系统自动关闭！<br/>----------<br/>'
                    . $siteName . '<br/>' . date('Y-m-d H:i:s');
                \app\lib\MsgNotice::sendAdminMessage($title, $content);
            }
        }

        return '[余额自动转账] 成功结算' . $success . '个商户';
    }
}
