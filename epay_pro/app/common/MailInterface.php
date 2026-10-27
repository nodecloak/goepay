<?php

declare(strict_types=1);

namespace app\common;

/**
 * 邮件插件接口
 */
interface MailInterface
{
    /**
     * 发送邮件
     * @param string $to 收件人邮箱
     * @param string $subject 邮件主题
     * @param string $body 邮件内容（HTML）
     * @return bool 成功返回 true
     * @throws \Exception
     */
    public function send(string $to, string $subject, string $body): bool;
}
