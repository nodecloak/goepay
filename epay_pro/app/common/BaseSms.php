<?php

namespace app\common;

/**
 * 邮件插件基类
 */
abstract class BaseSms implements SmsInterface
{
    protected array $config;

    public function __construct(array $config)
    {
        $this->config = $config;
    }

    abstract public function send(string $phone, string $tpl_code, array $tpl_param): bool;

    protected function getTplCode(string $tpl_code): string
    {
        $scenes = ['login', 'reg', 'find', 'edit', 'balance', 'group', 'complain'];
        if (in_array($tpl_code, $scenes)) {
            $realTplCode = $this->config['sms_tpl_' . $tpl_code] ?? null;
            if (empty($realTplCode)) {
                throw new \Exception('短信模板编号未配置');
            }
        } else {
            $realTplCode = $tpl_code;
        }
        return $realTplCode;
    }
}
