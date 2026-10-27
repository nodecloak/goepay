<?php

declare(strict_types=1);

namespace app\cron;

use app\common\AbstractCronTask;
use app\lib\AddonManager;
use app\lib\MsgNotice;
use think\facade\Db;

class Order extends AbstractCronTask
{
    public static function identifier(): string
    {
        return 'order';
    }

    public static function name(): string
    {
        return '系统每日数据维护';
    }

    public function execute(array $params = []): string
    {
        $orderTime = config_get('order_time', null, true);
        if ($orderTime && strtotime($orderTime) >= strtotime(date('Y-m-d 00:00:00'))) {
            return '系统每日数据维护任务今日已完成';
        }

        //未支付订单清理
        Db::name('order')->where('status', 0)
            ->where('addtime', '<', date('Y-m-d H:i:s', time() - 3 * 86400))->delete();
        
        //邀请码清理
        Db::name('regcode')->where('time', '<', time() - 86400)->delete();
        
        //过期黑名单清理
        Db::name('blacklist')->whereNotNull('endtime')->whereRaw('endtime < NOW()')->delete();

        //微信客服支付日志清理
        if (AddonManager::isEnabled('wxkfpay')) {
            Db::table('pay_wxkflog')->where('addtime', '<', date('Y-m-d H:i:s', strtotime('-48 hours')))->delete();
        }

        //支付通道每日限额状态重置
        Db::name('channel')->whereRaw('1=1')->update(['daystatus' => 0]);

        //邀请返现结算
        $lastDay = date('Y-m-d', strtotime('-1 day'));
        if (config_get('invite_mode') == 1) {
            $moneyList = Db::name('order')
                ->whereIn('status', [1, 3])
                ->where('date', $lastDay)
                ->group('uid')
                ->field('uid, SUM(realmoney) as money')
                ->select()
                ->toArray();
            foreach ($moneyList as $row) {
                $parentId = Db::name('user')->where('uid', $row['uid'])->value('upid');
                if ($parentId <= 0) {
                    continue;
                }
                $groupId = Db::name('user')->where('uid', $parentId)->value('gid');
                $group = getGroupConfig($groupId);
                $enabled = $group['invite_open'] ?? config_get('invite_open');
                $rate = $group['invite_rate'] ?? config_get('invite_rate');
                if ($enabled == 1 && !empty($rate)) {
                    $money = round((float)$row['money'] * (float)$rate / 100, 2);
                    if ($money > 0) {
                        changeUserMoney($parentId, $money, true, '邀请返现', $row['uid']);
                    }
                }
            }
        }

        //到期用户组修改与通知
        $expiredUsers = Db::name('user')->where('gid', '>', 0)->whereNotNull('endtime')
            ->whereRaw('endtime < NOW()')->field('uid,gid,status,endtime')->select()->toArray();
        foreach ($expiredUsers as $user) {
            $group = Db::name('group')->where('gid', $user['gid'])->find();
            $groupId = isset($group['orig']) && $group['orig'] > 0 ? $group['orig'] : 0;
            Db::name('user')->where('uid', $user['uid'])->update(['gid' => $groupId, 'endtime' => null]);
            if ($user['status'] == 1 && $group) {
                MsgNotice::send('group', $user['uid'], [
                    'uid' => $user['uid'],
                    'group' => $group['name'],
                    'endtime' => $user['endtime'],
                ]);
            }
        }

        AddonManager::executeHook('cron_daily', ['lastday' => $lastDay]);
        config_set('order_time', date('Y-m-d H:i:s'));
        return $lastDay . '系统每日数据维护任务执行成功';
    }
}
