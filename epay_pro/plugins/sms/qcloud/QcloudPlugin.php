<?php

namespace plugins\sms\qcloud;

use app\common\BaseSms;

class QcloudPlugin extends BaseSms
{
	private static $apiurl = 'https://yun.tim.qq.com/v5/tlssmssvr/sendsms';

	public function send(string $phone, string $tpl_code, array $tpl_param): bool
	{
		if (empty($this->config['appid']) || empty($this->config['appkey']) || empty($this->config['sign'])) {
			throw new \Exception('短信配置不能为空');
			return false;
		}
		$tpl_code = $this->getTplCode($tpl_code);

		$time = time();
		$random = rand(100000, 999999);
		$url = self::$apiurl . "?sdkappid=" . $this->config['appid'] . "&random=" . $random;
		$data = [
			'tel' => [
				'nationcode' => '86',
				'mobile' => $phone
			],
			'params' => array_values($tpl_param),
			'time' => time(),
			'tpl_id' => intval($tpl_code),
			'sign' => $this->config['sign'],
			'sig' => $this->getSig($random, $time, $phone),
		];
		$response = get_curl($url, json_encode($data), null, null, null, null, null, ['Content-Type: application/json; charset=utf8']);
		if (!$response) {
			throw new \Exception('接口请求失败');
			return false;
		}
		$arr = json_decode($response, true);
		if (isset($arr['result']) && $arr['result'] == 0) {
			return true;
		} elseif (isset($arr['errmsg'])) {
			throw new \Exception($arr['errmsg']);
			return false;
		} else {
			throw new \Exception('接口返回异常');
			return false;
		}
	}

	//生成签名
	private function getSig($random, $time, $mobile)
	{
		$signstr = 'appkey=' . $this->config['appkey'] . '&random=' . $random . '&time=' . $time . '&mobile=' . $mobile;
		return hash("sha256", $signstr);
	}
}
