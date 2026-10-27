<?php

declare(strict_types=1);

namespace app\cron;

use app\common\AbstractCronTask;
use app\lib\Channel;
use app\logic\ComplainLogic;
use think\facade\Cache;
use think\facade\Db;
use Throwable;

class Complain extends AbstractCronTask
{
    private const MAX_FAILURES = 10;

    public static function identifier(): string
    {
        return 'complain';
    }

    public static function name(): string
    {
        return '交易投诉同步';
    }

    public function execute(array $params = []): string
    {
        $tasks = Db::name('complaintask')
            ->where('status', 1)
            ->whereRaw('(nexttime IS NULL OR nexttime <= NOW())')
            ->order('id', 'asc')
            ->select()
            ->toArray();

        if (empty($tasks)) {
            return '暂无需要执行的投诉同步任务';
        }

        $success = 0;
        $failed = 0;
        $skipped = 0;
        foreach ($tasks as $task) {
            $channelId = (int)$task['channel'];
            $nextTime = date(
                'Y-m-d H:i:s',
                time() + max(1, (int)$task['frequency']) * 60 + rand(0, 59)
            );
            $channelRow = Db::name('channel')->where('id', $channelId)->field('id,name,paytime')->find();

            if ((int)$task['days'] > 0 && $channelRow) {
                $since = date('Y-m-d H:i:s', strtotime('-' . (int)$task['days'] . ' days'));
                if (empty($channelRow['paytime']) || $channelRow['paytime'] < $since) {
                    Db::name('complaintask')->where('id', $task['id'])->update(['nexttime' => $nextTime]);
                    $skipped++;
                    continue;
                }
            }

            try {
                $channel = Channel::get($channelId);
                if (!$channel) {
                    throw new \RuntimeException('当前支付通道不存在');
                }
                $channel['source'] = 1;
                $model = ComplainLogic::getModel($channel);
                if (!$model) {
                    throw new \RuntimeException('不支持该支付插件');
                }

                $result = $model->refreshNewList($task['num'] ?? 20);
                if (!is_array($result) || (int)($result['code'] ?? -1) !== 0) {
                    throw new \RuntimeException((string)($result['msg'] ?? '获取投诉记录失败'));
                }

                Db::name('complaintask')->where('id', $task['id'])->update([
                    'lasttime' => date('Y-m-d H:i:s'),
                    'laststatus' => 1,
                    'nexttime' => $nextTime,
                    'reason' => null,
                ]);
                Cache::delete($this->failureCacheKey((int)$task['id']));
                $success++;
                echo '[投诉同步] 通道' . $channelId . '：' . ($result['msg'] ?? '同步成功') . PHP_EOL;
            } catch (Throwable $e) {
                $failed++;
                $this->recordFailure($task, $channelRow, $nextTime, $e->getMessage());
                echo '[投诉同步] 通道' . $channelId . '：' . $e->getMessage() . PHP_EOL;
                exception_log($e, '投诉同步通道:' . $channelId);
            }
            usleep(100000);
        }

        return sprintf('[投诉同步] 成功%d个，失败%d个，跳过%d个。', $success, $failed, $skipped);
    }

    private function recordFailure(array $task, ?array $channel, string $nextTime, string $reason): void
    {
        $taskId = (int)$task['id'];
        $cacheKey = $this->failureCacheKey($taskId);
        $failures = (int)Cache::get($cacheKey, 0) + 1;
        Cache::set($cacheKey, $failures, 0);

        $update = [
            'lasttime' => date('Y-m-d H:i:s'),
            'laststatus' => 2,
            'nexttime' => $nextTime,
            'reason' => mb_substr($reason, 0, 1000),
        ];
        if ($failures >= self::MAX_FAILURES) {
            $update['status'] = 0;
            $update['nexttime'] = null;
        }
        Db::name('complaintask')->where('id', $taskId)->update($update);

        if ($failures >= self::MAX_FAILURES) {
            $siteName = config_get('sitename');
            \app\lib\MsgNotice::sendAdminMessage(
                $siteName . ' - 投诉同步任务自动停止提醒',
                '尊敬的管理员：支付通道“' . ($channel['name'] ?? $task['channel'])
                . '”（ID：' . $task['channel'] . '）连续' . self::MAX_FAILURES
                . '次获取投诉失败，自动同步任务已停止。<br/>失败原因：'
                . htmlspecialchars($reason, ENT_QUOTES, 'UTF-8')
                . '<br/>----------<br/>' . $siteName . '<br/>' . date('Y-m-d H:i:s')
            );
        }
    }

    private function failureCacheKey(int $taskId): string
    {
        return 'complaintask_fail_' . $taskId;
    }
}
