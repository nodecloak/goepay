<?php

declare(strict_types=1);

namespace app\cron;

use app\common\AbstractCronTask;
use app\lib\Channel;
use app\lib\MsgNotice;
use app\logic\TransferLogic;
use think\facade\Db;

class Settle extends AbstractCronTask
{
    private const TYPE_APP_MAP = [1 => 'alipay', 2 => 'wxpay', 3 => 'qqpay', 4 => 'bank'];
    private const TYPE_NAME_MAP = [1 => '支付宝', 2 => '微信', 3 => 'QQ钱包', 4 => '银行卡'];

    public static function identifier(): string
    {
        return 'settle';
    }

    public static function name(): string
    {
        return '自动生成结算列表';
    }

    public function execute(array $params = []): string
    {
        $messages = [$this->generateSettleList()];
        $transferMessage = $this->autoTransfer();
        if ($transferMessage !== null) {
            $messages[] = $transferMessage;
        }
        return implode('；', $messages);
    }

    private function generateSettleList(): string
    {
        $settleTime = config_get('settle_time', null, true);
        if (config_get('settle_set_hour') == 1) {
            if ($settleTime && strtotime($settleTime) >= strtotime(date('Y-m-d H:00:00'))) {
                return '自动生成结算列表今日[' . date('H') . '时]已完成';
            }
        } elseif ($settleTime && strtotime($settleTime) >= strtotime(date('Y-m-d 00:00:00'))) {
            return '自动生成结算列表今日已完成';
        }

        $minimum = (float)config_get('settle_money', 0);
        $users = Db::name('user')
            ->where('money', '>=', $minimum)
            ->where('settle', 1)
            ->where('status', 1)
            ->whereNotNull('account')
            ->whereNotNull('username')
            ->select()
            ->toArray();
        $count = 0;
        $total = 0.0;

        foreach ($users as $user) {
            if (config_get('cert_force') == 1 && $user['cert'] == 0) {
                continue;
            }
            $count++;
            $rate = (float)config_get('settle_rate', 0);
            $group = getGroupConfig($user['gid']);
            if (isset($group['settle_open']) && $group['settle_open'] > 0) {
                if ($group['settle_open'] == 2) {
                    continue;
                }
            } elseif (config_get('settle_open') != 1 && config_get('settle_open') != 3) {
                continue;
            }

            if (config_get('settle_set_hour') == 1) {
                $hour = !empty($group['settle_hour']) ? $group['settle_hour'] : config_get('settle_hour', '0');
                if ($hour != date('H')) {
                    continue;
                }
                if (Db::name('settle')->where('uid', $user['uid'])
                    ->where('addtime', '>=', date('Y-m-d 00:00:00'))->find()) {
                    continue;
                }
            }
            if (isset($group['settle_rate'])) {
                $rate = (float)$group['settle_rate'];
            }

            $money = (float)$user['money'];
            if ($user['remain_money'] > 0) {
                $remain = (string)$user['remain_money'];
                if (str_contains($remain, '%')) {
                    $remainRate = (float)str_replace('%', '', $remain);
                    $money = $remainRate > 0 && $remainRate < 100
                        ? round($money * (1 - $remainRate / 100), 2)
                        : round($money - (float)$remain, 2);
                } else {
                    $money = round($money - (float)$user['remain_money'], 2);
                }
            }
            if ($money < $minimum) {
                continue;
            }

            $realMoney = $money;
            if ($rate > 0) {
                $fee = round($money * $rate / 100, 2);
                $feeMin = (float)config_get('settle_fee_min', 0);
                $feeMax = (float)config_get('settle_fee_max', 0);
                if ($feeMin > 0 && $fee < $feeMin) {
                    $fee = $feeMin;
                }
                if ($feeMax > 0 && $fee > $feeMax) {
                    $fee = $feeMax;
                }
                $realMoney = $money - $fee;
            }

            if (Db::name('settle')->insert([
                'uid' => $user['uid'],
                'type' => $user['settle_id'],
                'account' => $user['account'],
                'username' => $user['username'],
                'money' => $money,
                'realmoney' => $realMoney,
                'addtime' => date('Y-m-d H:i:s'),
                'status' => 0,
            ])) {
                changeUserMoney($user['uid'], $money, false, '自动结算');
                $total += $realMoney;
            }
        }

        config_set('settle_time', date('Y-m-d H:i:s'));
        return '自动生成结算列表成功 allmony=' . $total . ' num=' . $count;
    }

    private function autoTransfer(): ?string
    {
        if (config_get('settle_auto_transfer') != 1) {
            return null;
        }

        $hourRaw = config_get('settle_auto_transfer_hour', '');
        if ($hourRaw === '' || $hourRaw === null || !is_numeric($hourRaw)) {
            return null;
        }
        $hour = (int)$hourRaw;
        if ($hour < 0 || $hour > 23) {
            return null;
        }
        if ((int)date('H') < $hour) {
            return '未到自动转账时间点[' . $hour . '时]';
        }

        $transferTime = config_get('settle_auto_transfer_time', null, true);
        if ($transferTime && strtotime($transferTime) >= strtotime(date('Y-m-d 00:00:00'))) {
            return '结算列表自动转账今日已完成';
        }

        $rows = Db::name('settle')->alias('s')
            ->leftJoin('user u', 's.uid = u.uid')
            ->where('s.status', 0)
            ->where('s.addtime', '>=', date('Y-m-d 00:00:00'))
            ->field('s.*, u.gid, u.channelinfo')
            ->order('s.id', 'asc')
            ->select()
            ->toArray();

        $success = 0;
        $fail = 0;
        $skipLimit = 0;
        $skipChannel = 0;
        $successMoney = 0.0;
        $failDetails = [];
        $transfermax = (float)config_get('settle_auto_transfermax', 0);
        $groupCache = [];

        foreach ($rows as $row) {
            $money = (float)$row['money'];
            $realmoney = (float)$row['realmoney'];
            if ($transfermax > 0 && $money > $transfermax) {
                $skipLimit++;
                continue;
            }

            $app = self::TYPE_APP_MAP[$row['type']] ?? '';
            if ($app === '') {
                $skipChannel++;
                continue;
            }

            $gid = (int)($row['gid'] ?? 0);
            if (!isset($groupCache[$gid])) {
                $groupCache[$gid] = getGroupConfig($gid);
            }
            $channelid = $this->resolveTransferChannel($app, $groupCache[$gid]);
            if ($channelid <= 0) {
                $skipChannel++;
                continue;
            }

            $channel = Channel::get($channelid, $row['channelinfo'] ?? null);
            if (!$channel) {
                $fail++;
                $failDetails[] = $this->formatFailDetail($row, '转账通道不存在');
                continue;
            }

            $out_biz_no = date('YmdHis') . rand(11111, 99999);
            try {
                $result = TransferLogic::submit($app, $channel, $out_biz_no, $row['account'], $row['username'], $realmoney);
            } catch (\Exception $e) {
                exception_log($e, '结算列表自动转账');
                $message = $e->getMessage();
                Db::name('settle')->where('id', $row['id'])->update([
                    'status' => 3,
                    'result' => $message,
                    'transfer_status' => 2,
                    'transfer_result' => '转账失败 ' . $message,
                ]);
                $fail++;
                $failDetails[] = $this->formatFailDetail($row, $message);
                continue;
            }

            if (isset($result['code']) && $result['code'] == 0) {
                $update = [
                    'status' => 1,
                    'endtime' => date('Y-m-d H:i:s'),
                    'transfer_no' => $out_biz_no,
                    'transfer_channel' => $channelid,
                    'transfer_status' => 1,
                    'transfer_result' => $result['orderid'] ?? '',
                    'transfer_date' => $result['paydate'] ?? '',
                ];
                if (isset($result['wxpackage'])) {
                    $update['transfer_ext'] = $result['wxpackage'];
                }
                Db::name('settle')->where('id', $row['id'])->update($update);

                $notice = [
                    'money' => $row['money'],
                    'realmoney' => $row['realmoney'],
                    'time' => date('Y-m-d H:i:s'),
                    'account' => $row['account'],
                ];
                if (isset($result['wxpackage'])) {
                    $notice['jumpurl'] = request()->siteurl . 'transfer/wx?type=settle&id=' . $row['id'];
                }
                MsgNotice::send('settle', $row['uid'], $notice);

                $success++;
                $successMoney += $realmoney;
            } else {
                $message = $result['msg'] ?? '转账失败';
                Db::name('settle')->where('id', $row['id'])->update([
                    'status' => 3,
                    'result' => $message,
                    'transfer_status' => 2,
                    'transfer_result' => '转账失败 ' . $message,
                ]);
                $fail++;
                $failDetails[] = $this->formatFailDetail($row, $message);
            }

            usleep(300000);
        }

        config_set('settle_auto_transfer_time', date('Y-m-d H:i:s'));
        $this->sendAdminReport($success, $fail, $skipLimit, $skipChannel, $successMoney, count($rows), $failDetails);

        $skip = $skipLimit + $skipChannel;
        return '结算列表自动转账完成 成功' . $success . '笔 失败' . $fail . '笔 跳过' . $skip . '笔';
    }

    /**
     * 转账付款通道优先使用用户组配置，未设置则回退系统配置。
     */
    private function resolveTransferChannel(string $app, array $group): int
    {
        $groupChannel = $group['transfer_' . $app] ?? null;
        if ($groupChannel !== null && $groupChannel !== '' && $groupChannel !== '_') {
            return (int)$groupChannel;
        }
        return (int)config_get('transfer_' . $app);
    }

    private function formatFailDetail(array $row, string $message): string
    {
        $typeName = self::TYPE_NAME_MAP[$row['type']] ?? '未知';
        return '商户' . $row['uid'] . ' ' . $typeName . ' ' . $row['realmoney'] . '元：' . $message;
    }

    private function sendAdminReport(
        int $success,
        int $fail,
        int $skipLimit,
        int $skipChannel,
        float $successMoney,
        int $total,
        array $failDetails
    ): void {
        $siteName = config_get('sitename');
        $skip = $skipLimit + $skipChannel;
        $content = '尊敬的' . $siteName . '管理员，今日结算列表自动转账已完成。<br/>'
            . '<b>日期：</b>' . date('Y-m-d') . '<br/>'
            . '<b>待处理笔数：</b>' . $total . '<br/>'
            . '<b>成功：</b>' . $success . '笔，金额 ' . $successMoney . ' 元<br/>'
            . '<b>失败：</b>' . $fail . '笔<br/>'
            . '<b>跳过：</b>' . $skip . '笔（超出限额' . $skipLimit . '笔，未开启转账通道' . $skipChannel . '笔）';

        if (!empty($failDetails)) {
            $content .= '<br/><b>失败明细：</b><br/>' . implode('<br/>', array_slice($failDetails, 0, 50));
            if (count($failDetails) > 50) {
                $content .= '<br/>...其余 ' . (count($failDetails) - 50) . ' 条已省略';
            }
        }

        $content .= '<br/>----------<br/>' . $siteName . '<br/>' . date('Y-m-d H:i:s');
        \app\lib\MsgNotice::sendAdminMessage($siteName . ' - 结算列表自动转账已完成', $content);
    }
}
