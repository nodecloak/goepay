<?php

declare(strict_types=1);

namespace app\async;

use app\common\AsyncTaskInterface;
use app\lib\MsgNotice;
use InvalidArgumentException;

/**
 * 管理员系统告警异步发送
 */
class SendAdminMsg implements AsyncTaskInterface
{
    public static function identifier(): string
    {
        return 'send_admin_message';
    }

    public function handle(array $payload): void
    {
        $title = $payload['title'] ?? '';
        $content = $payload['content'] ?? '';
        if (empty($title) || empty($content)) {
            throw new InvalidArgumentException('消息标题和内容不能为空');
        }
        MsgNotice::sendAdminMessage($title, $content);
    }
}
