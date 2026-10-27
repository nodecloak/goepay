<?php

namespace app\common;

/**
 * 邮件插件基类
 */
abstract class BaseMail implements MailInterface
{
    protected array $config;

    public function __construct(array $config)
    {
        $this->config = $config;
    }

    abstract public function send(string $to, string $subject, string $body): bool;
}
