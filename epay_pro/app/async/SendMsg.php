<?php

declare(strict_types=1);

namespace app\async;

use app\common\AsyncTaskInterface;
use app\lib\MsgNotice;
use InvalidArgumentException;

/**
 * 统一消息通知异步发送
 */
class SendMsg implements AsyncTaskInterface
{
    public static function identifier(): string
    {
        return 'send_msg';
    }

    public function handle(array $payload): void
    {
        $scene = $payload['scene'] ?? '';
        if (!is_string($scene) || $scene === '') {
            throw new InvalidArgumentException('消息通知任务场景不能为空');
        }
        $uid = $payload['uid'] ?? 0;
        $param = $payload['param'] ?? [];
        if (!is_array($param)) {
            throw new InvalidArgumentException('消息通知任务参数无效');
        }

        MsgNotice::send($scene, $uid, $param);
    }
}
