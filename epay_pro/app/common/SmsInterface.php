<?php

declare(strict_types=1);

namespace app\common;

/**
 * 短信插件接口
 */
interface SmsInterface
{
    /**
     * 发送短信
     * @param string $phone 手机号码
     * @param string $tpl_code 模板ID
     * @param array $tpl_param 模板参数
     * @return bool
     * @throws \Exception
     */
    public function send(string $phone, string $tpl_code, array $tpl_param): bool;
}
