<?php

declare(strict_types=1);

namespace app\cron;

use app\common\AbstractCronTask;
use app\logic\ApplymentLogic;
use think\facade\Db;
use Throwable;

class Applyrefresh extends AbstractCronTask
{
    public static function identifier(): string
    {
        return 'applyrefresh';
    }

    public static function name(): string
    {
        return '进件商户状态刷新';
    }

    public static function parameterDefinitions(): array
    {
        return [
            'refresh_minutes' => [
                'label' => '商户状态刷新频率（分钟）',
                'type' => 'number',
                'required' => true,
                'min' => 15,
                'default' => 60,
            ],
        ];
    }

    public function execute(array $params = []): string
    {
        $refreshMinutes = max(15, (int)($params['refresh_minutes'] ?? 60));
        $now = time();
        $refreshBefore = date('Y-m-d H:i:s', $now - $refreshMinutes * 60);
        $updatedBefore = date('Y-m-d H:i:s', $now - 10 * 60);
        $updatedAfter = date('Y-m-d H:i:s', $now - 3 * 86400);

        $merchants = Db::name('applymerchant')
            ->whereIn('status', [1, 5])
            ->where('updatetime', '<=', $updatedBefore)
            ->where('updatetime', '>=', $updatedAfter)
            ->where(function ($query) use ($refreshBefore) {
                $query->whereNull('refreshtime')
                    ->whereOr('refreshtime', '<=', $refreshBefore);
            })
            ->order('refreshtime', 'asc')
            ->order('updatetime', 'asc')
            ->limit(10)
            ->select()
            ->toArray();

        if (empty($merchants)) {
            return '没有需要刷新状态的进件商户';
        }

        $success = 0;
        $fail = 0;
        $total = count($merchants);

        foreach ($merchants as $index => $row) {
            try {
                $model = ApplymentLogic::getModel((int)$row['cid']);
                if (!$model) {
                    throw new \RuntimeException('进件渠道不存在');
                }

                $result = $model->query($row);
                if ((int)($result['code'] ?? -1) === 0) {
                    $success++;
                } else {
                    $fail++;
                    echo '[进件商户状态刷新] ' . $row['orderid'] . ' 刷新失败：'
                        . ($result['msg'] ?? '未知错误') . "\n";
                }
            } catch (Throwable $e) {
                $fail++;
                exception_log($e, '进件商户状态刷新');
                echo '[进件商户状态刷新] ' . $row['orderid'] . ' 刷新失败：'
                    . $e->getMessage() . "\n";
            } finally {
                Db::name('applymerchant')->where('id', $row['id'])->update([
                    'refreshtime' => date('Y-m-d H:i:s'),
                ]);
            }

            if ($index < $total - 1) {
                usleep(500000);
            }
        }

        return "[进件商户状态刷新] 成功{$success}，失败{$fail}";
    }
}
