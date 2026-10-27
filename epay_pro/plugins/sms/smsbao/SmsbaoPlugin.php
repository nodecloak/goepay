<?php

namespace plugins\sms\smsbao;

use app\common\BaseSms;

class SmsbaoPlugin extends BaseSms
{

	private static $apiurl = 'https://api.smsbao.com/sms';
	private static $statusStr = array(
		"0" => "短信发送成功",
		"-1" => "参数不全",
		"-2" => "服务器空间不支持",
		"30" => "密码错误",
		"40" => "账号不存在",
		"41" => "余额不足",
		"42" => "帐户已过期",
		"43" => "IP地址限制",
		"50" => "内容含有敏感词"
	);

	public function send(string $phone, string $tpl_code, array $tpl_param): bool
	{
		if (empty($this->config['user']) || empty($this->config['pass']) || empty($this->config['sign'])) {
			throw new \Exception('短信配置不能为空');
			return false;
		}
		$tpl_code = $this->getTplCode($tpl_code);

		foreach ($tpl_param as $k => $v) {
			$tpl_code = str_replace('{' . $k . '}', $v, $tpl_code);
		}
		$content = '【' . $this->config['sign'] . '】' . $tpl_code;
		$sendurl = self::$apiurl . "?u=" . $this->config['user'] . "&p=" . md5($this->config['pass']) . "&m=" . $phone . "&c=" . urlencode($content);
		$result = get_curl($sendurl);
		if ($result === false) {
			throw new \Exception('接口请求失败');
			return false;
		}
		if ($result === '0') {
			return true;
		} else {
			throw new \Exception(self::$statusStr[$result] ?? ('CODE: ' . $result));
			return false;
		}
	}
}
