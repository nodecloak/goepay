<?php

declare(strict_types=1);

namespace app\cron;

use app\common\AbstractCronTask;
use app\lib\MsgNotice;
use think\facade\Db;
use Throwable;

class DailyReport extends AbstractCronTask
{
    private const TRANSFER_TYPE_NAMES = [
        'alipay' => '支付宝',
        'wxpay' => '微信',
        'qqpay' => 'QQ钱包',
        'bank' => '银行卡',
    ];

    public static function identifier(): string
    {
        return 'daily_report';
    }

    public static function name(): string
    {
        return '收款日报发送';
    }

    public static function parameterDefinitions(): array
    {
        return [
            'send_hour' => [
                'label' => '发送时间点（小时，0~23）',
                'type' => 'number',
                'required' => true,
                'min' => 0,
                'max' => 23,
                'default' => 9,
            ],
        ];
    }

    public function execute(array $params = []): string
    {
        $sendHour = (int)($params['send_hour'] ?? 9);
        if ($sendHour < 0 || $sendHour > 23) {
            $sendHour = 9;
        }
        if ((int)date('H') < $sendHour) {
            return '未到收款日报发送时间点[' . $sendHour . '时]';
        }

        $date = date('Y-m-d', strtotime('-1 day'));
        $today = date('Y-m-d');
        $sent = 0;
        $skipped = 0;
        $lastUid = 0;

        while (true) {
            $users = Db::name('user')
                ->where('status', 1)
                ->where('uid', '>', $lastUid)
                ->field('uid,gid,msgconfig')
                ->order('uid', 'asc')
                ->limit(200)
                ->select()
                ->toArray();
            if ($users === []) {
                break;
            }
            $lastUid = (int)end($users)['uid'];

            $targets = [];
            foreach ($users as $user) {
                $config = !empty($user['msgconfig']) ? @unserialize($user['msgconfig']) : [];
                if (!is_array($config) || empty($config['daily_report'])) {
                    continue;
                }
                $uid = (int)$user['uid'];
                if (cache($this->cacheKey($uid, $today))) {
                    $skipped++;
                    continue;
                }
                $targets[] = [
                    'uid' => $uid,
                    'gid' => (int)$user['gid'],
                ];
            }
            if ($targets === []) {
                continue;
            }

            $reports = $this->buildReports($targets, $date, $today);
            foreach ($targets as $target) {
                $uid = $target['uid'];
                try {
                    MsgNotice::send('daily_report', $uid, $reports[$uid]);
                    cache($this->cacheKey($uid, $today), 1, max(60, strtotime('tomorrow') - time()));
                    $sent++;
                } catch (Throwable $e) {
                    exception_log($e, '收款日报发送');
                    echo '[收款日报] 商户' . $uid . ' 发送失败：' . $e->getMessage() . "\n";
                }
            }
        }

        if ($sent === 0 && $skipped === 0) {
            return '暂无需要发送收款日报的商户';
        }
        return '收款日报已发送给' . $sent . '位商户' . ($skipped > 0 ? '，跳过已发送' . $skipped . '位' : '');
    }

    private function cacheKey(int $uid, string $today): string
    {
        return 'daily_report_' . $uid . '_' . str_replace('-', '', $today);
    }

    /**
     * @param array<int, array{uid:int,gid:int}> $targets
     * @return array<int, array<string, mixed>>
     */
    private function buildReports(array $targets, string $date, string $today): array
    {
        $uids = array_column($targets, 'uid');
        $orderMap = $this->queryOrderStats($uids, $date);
        $typeMap = $this->queryOrderTypeStats($uids, $date);

        $transferUidSet = [];
        foreach ($targets as $target) {
            if ($this->isTransferEnabled($target['gid'])) {
                $transferUidSet[$target['uid']] = true;
            }
        }
        $transferMap = $transferUidSet === []
            ? []
            : $this->queryTransferStats(array_keys($transferUidSet), $date, $today);

        $reports = [];
        foreach ($targets as $target) {
            $uid = $target['uid'];
            $order = $orderMap[$uid] ?? [
                'money' => 0.0,
                'getmoney' => 0.0,
                'order_count' => 0,
                'success_count' => 0,
            ];
            $orderCount = (int)$order['order_count'];
            $successCount = (int)$order['success_count'];
            $successRate = $orderCount > 0 ? round($successCount / $orderCount * 100, 2) : 0.0;

            $hasTransfer = isset($transferUidSet[$uid]);
            $transfer = $transferMap[$uid] ?? ['total' => 0.0, 'types' => []];
            $reports[$uid] = [
                'date' => $date,
                'money' => $this->formatMoney((float)$order['money']),
                'getmoney' => $this->formatMoney((float)$order['getmoney']),
                'order_count' => $orderCount,
                'success_count' => $successCount,
                'success_rate' => $this->formatMoney($successRate),
                'type_money' => $typeMap[$uid] ?? [],
                'has_transfer' => $hasTransfer,
                'transfer_total' => $this->formatMoney((float)$transfer['total']),
                'transfer_types' => $transfer['types'],
            ];
        }
        return $reports;
    }

    /**
     * @param int[] $uids
     * @return array<int, array{money:float,getmoney:float,order_count:int,success_count:int}>
     */
    private function queryOrderStats(array $uids, string $date): array
    {
        $rows = Db::name('order')
            ->whereIn('uid', $uids)
            ->where('date', $date)
            ->fieldRaw('uid,
                COALESCE(SUM(CASE WHEN status>0 THEN money ELSE 0 END), 0) as money,
                COALESCE(SUM(CASE WHEN status>0 THEN getmoney ELSE 0 END), 0) as getmoney,
                COUNT(*) as order_count,
                COUNT(IF(status>0, 1, NULL)) as success_count')
            ->group('uid')
            ->select()
            ->toArray();

        $map = [];
        foreach ($rows as $row) {
            $map[(int)$row['uid']] = [
                'money' => (float)$row['money'],
                'getmoney' => (float)$row['getmoney'],
                'order_count' => (int)$row['order_count'],
                'success_count' => (int)$row['success_count'],
            ];
        }
        return $map;
    }

    /**
     * @param int[] $uids
     * @return array<int, array<int, array{name:string,money:string}>>
     */
    private function queryOrderTypeStats(array $uids, string $date): array
    {
        $rows = Db::name('order')->alias('A')
            ->leftJoin('type B', 'A.type = B.id')
            ->whereIn('A.uid', $uids)
            ->where('A.date', $date)
            ->where('A.status', '>', 0)
            ->fieldRaw('A.uid, A.type, COALESCE(B.showname, \'其他\') as showname, COALESCE(SUM(A.money), 0) as money')
            ->group('A.uid, A.type')
            ->order('A.uid', 'asc')
            ->order('A.type', 'asc')
            ->select()
            ->toArray();

        $map = [];
        foreach ($rows as $row) {
            $map[(int)$row['uid']][] = [
                'name' => (string)$row['showname'],
                'money' => $this->formatMoney((float)$row['money']),
            ];
        }
        return $map;
    }

    /**
     * @param int[] $uids
     * @return array<int, array{total:float,types:array<int, array{name:string,money:string}>}>
     */
    private function queryTransferStats(array $uids, string $date, string $today): array
    {
        $rows = Db::name('transfer')
            ->whereIn('uid', $uids)
            ->where('status', 1)
            ->where('addtime', '>=', $date)
            ->where('addtime', '<', $today)
            ->fieldRaw('uid, type, COALESCE(SUM(money), 0) as money')
            ->group('uid, type')
            ->select()
            ->toArray();

        $map = [];
        foreach ($rows as $row) {
            $uid = (int)$row['uid'];
            if (!isset($map[$uid])) {
                $map[$uid] = ['total' => 0.0, 'types' => []];
            }
            $money = (float)$row['money'];
            $map[$uid]['total'] += $money;
            $type = (string)$row['type'];
            $map[$uid]['types'][] = [
                'name' => self::TRANSFER_TYPE_NAMES[$type] ?? $type,
                'money' => $this->formatMoney($money),
            ];
        }
        return $map;
    }

    private function isTransferEnabled(int $gid): bool
    {
        $group = getGroupConfig($gid);
        if (isset($group['user_transfer'])) {
            return (int)$group['user_transfer'] === 1;
        }
        return (int)config_get('user_transfer') === 1;
    }

    private function formatMoney(float $money): string
    {
        return number_format($money, 2, '.', '');
    }
}
