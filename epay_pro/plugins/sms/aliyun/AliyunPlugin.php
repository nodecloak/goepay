<?php

namespace plugins\sms\aliyun;

use app\common\BaseSms;

class AliyunPlugin extends BaseSms
{
	private $endpoint = 'dysmsapi.aliyuncs.com';
	private $dypns_endpoint = 'dypnsapi.aliyuncs.com';
	private $version = '2017-05-25';
	private $regionId = 'cn-hangzhou';

	public function send(string $phone, string $tpl_code, array $tpl_param): bool
	{
		if (empty($this->config['AccessKeyId']) || empty($this->config['AccessKeySecret']) || empty($this->config['sign'])) {
			throw new \Exception('短信配置不能为空');
			return false;
		}

		$tpl_code = $this->getTplCode($tpl_code);

		if (($this->config['product'] ?? '0') == '1') {
			if (!isset($tpl_param['min'])) $tpl_param['min'] = '5';
			$url = 'https://' . $this->dypns_endpoint . '/';
			$data = array(
				'Action' => 'SendSmsVerifyCode',
				'PhoneNumber' => $phone,
				'SignName' => $this->config['sign'],
				'TemplateCode' => $tpl_code,
				'TemplateParam' => json_encode($tpl_param),
				'Format' => 'JSON',
				'RegionId' => $this->regionId,
				'Version' => $this->version,
				'AccessKeyId' => $this->config['AccessKeyId'],
				'SignatureMethod' => 'HMAC-SHA1',
				'Timestamp' => gmdate('Y-m-d\TH:i:s\Z'),
				'SignatureVersion' => '1.0',
				'SignatureNonce' => random(8)
			);
		} else {
			$url = 'https://' . $this->endpoint . '/';
			$data = array(
				'Action' => 'SendSms',
				'PhoneNumbers' => $phone,
				'SignName' => $this->config['sign'],
				'TemplateCode' => $tpl_code,
				'TemplateParam' => json_encode($tpl_param),
				'Format' => 'JSON',
				'RegionId' => $this->regionId,
				'Version' => $this->version,
				'AccessKeyId' => $this->config['AccessKeyId'],
				'SignatureMethod' => 'HMAC-SHA1',
				'Timestamp' => gmdate('Y-m-d\TH:i:s\Z'),
				'SignatureVersion' => '1.0',
				'SignatureNonce' => random(8)
			);
		}
		
		$data['Signature'] = $this->aliyunSignature($data, $this->config['AccessKeySecret'], 'POST');

		$response = get_curl($url, http_build_query($data));
		if (!$response) {
			throw new \Exception('接口请求失败');
			return false;
		}
		$arr = json_decode($response, true);
		if (isset($arr['Code']) && $arr['Code'] == 'OK') {
			return true;
		} elseif (isset($arr['Message'])) {
			throw new \Exception($arr['Message']);
			return false;
		} else {
			throw new \Exception('接口返回异常');
			return false;
		}
	}

	private function aliyunSignature($parameters, $accessKeySecret, $method)
	{
		ksort($parameters);
		$canonicalizedQueryString = '';
		foreach ($parameters as $key => $value) {
			if ($value === null) continue;
			$canonicalizedQueryString .= '&' . $this->percentEncode($key) . '=' . $this->percentEncode($value);
		}
		$stringToSign = $method . '&%2F&' . $this->percentencode(substr($canonicalizedQueryString, 1));
		$signature = base64_encode(hash_hmac("sha1", $stringToSign, $accessKeySecret . "&", true));

		return $signature;
	}

	private function percentEncode($str)
	{
		$search = ['+', '*', '%7E'];
		$replace = ['%20', '%2A', '~'];
		return str_replace($search, $replace, urlencode($str));
	}
}
