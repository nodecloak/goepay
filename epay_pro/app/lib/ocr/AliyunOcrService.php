<?php

namespace app\lib\ocr;

use Exception;

class AliyunOcrService implements OcrServiceInterface
{
	private $AccessKeyId;
	private $AccessKeySecret;
	private $Endpoint = 'ocr-api.cn-hangzhou.aliyuncs.com'; //API接入域名
	private $Version = '2021-07-07'; //API版本号

	function __construct($AccessKeyId, $AccessKeySecret)
	{
		$this->AccessKeyId = $AccessKeyId;
		$this->AccessKeySecret = $AccessKeySecret;
	}

	//身份证正面识别
	public function idcard($file_path)
	{
		try {
			$arr = $this->request('RecognizeIdcard', file_get_contents($file_path), true);
			if (!isset($arr['Data'])) throw new Exception('接口返回异常');
			$data = json_decode($arr['Data'], true);
			if (!is_array($data) || empty($data['data']['face'])) {
				throw new Exception('请上传人像面照片');
			}
			$face = $data['data']['face']['data'] ?? [];
			$result = ['id_no' => $face['idNumber'] ?? '', 'name' => $face['name'] ?? '', 'address' => $face['address'] ?? '', 'sex' => $face['sex'] ?? '', 'ethnicity' => $face['ethnicity'] ?? '', 'birth_date' => str_replace(['年', '月', '日'], ['-', '-', ''], $face['birthDate'] ?? '')];
			return $result;
		} catch (Exception $e) {
			throw new Exception('身份证识别失败，' . $e->getMessage());
		}
	}

	//身份证反面识别
	public function idcard_back($file_path)
	{
		try {
			$arr = $this->request('RecognizeIdcard', file_get_contents($file_path), true);
			if (!isset($arr['Data'])) throw new Exception('接口返回异常');
			$data = json_decode($arr['Data'], true);
			if (!is_array($data) || empty($data['data']['back'])) {
				throw new Exception('请上传国徽面照片');
			}
			$back = $data['data']['back']['data'] ?? [];
			$period = explode('-', $back['validPeriod'] ?? '');
			$result = ['issue_authority' => $back['issueAuthority'] ?? '', 'period_begin' => str_replace('.', '-', $period[0] ?? ''), 'period_end' => str_replace('.', '-', $period[1] ?? '')];
			return $result;
		} catch (Exception $e) {
			throw new Exception('身份证识别失败，' . $e->getMessage());
		}
	}

	//护照识别
	public function passport($file_path)
	{
		try {
			$arr = $this->request('RecognizePassport', file_get_contents($file_path), true);
			if (!is_array($arr) || !isset($arr['Data'])) throw new Exception('接口返回异常');
			$data = json_decode($arr['Data'], true);
			if (!is_array($data) || empty($data['data'])) {
				throw new Exception('护照识别失败');
			}
			$d = $data['data'];
			$result = ['passport_no' => $d['passportNumber'] ?? '', 'passport_type' => $d['passportType'] ?? '', 'last_name' => $d['surname'] ?? '', 'first_name' => $d['givenName'] ?? '', 'name' => $d['name'] ?? '', 'sex' => $d['sex'] ?? '', 'country' => $d['country'] ?? '', 'birth_date' => $d['birthDateYmd'] ?? '', 'period_begin' => $d['issueDateYmd'] ?? '', 'period_end' => $d['validToDate'] ?? ''];
			return $result;
		} catch (Exception $e) {
			throw new Exception('护照识别失败，' . $e->getMessage());
		}
	}

	//银行卡识别
	public function bank_card($file_path)
	{
		try {
			$arr = $this->request('RecognizeBankCard', file_get_contents($file_path), true);
			if (!is_array($arr) || !isset($arr['Data'])) throw new Exception('接口返回异常');
			$data = json_decode($arr['Data'], true);
			if (!is_array($data) || empty($data['data'])) throw new Exception('银行卡识别失败');
			$d = $data['data'];
			$result = ['bank_name' => $d['bankName'] ?? '', 'card_type' => $d['cardType'] ?? '', 'card_no' => $d['cardNumber'] ?? '', 'period_end' => $d['validToDate'] ?? ''];
			return $result;
		} catch (Exception $e) {
			throw new Exception('银行卡识别失败，' . $e->getMessage());
		}
	}

	//营业执照识别
	public function business_license($file_path)
	{
		try {
			$arr = $this->request('RecognizeBusinessLicense', file_get_contents($file_path), true);
			if (!isset($arr['Data'])) throw new Exception('接口返回异常');
			$data = json_decode($arr['Data'], true);
			if (!is_array($data) || empty($data['data'])) {
				throw new Exception('营业执照识别失败');
			}
			$d = $data['data'];
			$result = ['license_no' => $d['creditCode'] ?? '', 'name' => $d['companyName'] ?? '', 'address' => $d['businessAddress'] ?? '', 'reg_date' => str_replace(['年', '月', '日'], ['-', '-', ''], $d['RegistrationDate'] ?? ''), 'legal_name' => $d['legalPerson'] ?? '', 'type' => $d['companyType'] ?? '', 'registered_capital' => $d['registeredCapital'] ?? '', 'business_scope' => $d['businessScope'] ?? '', 'period_begin' => self::formatDate($d['validFromDate'] ?? ''), 'period_end' => empty($d['validToDate']) ? '长期' : self::formatDate($d['validToDate'])];
			return $result;
		} catch (Exception $e) {
			throw new Exception('营业执照识别失败，' . $e->getMessage());
		}
	}

	//银行开户许可证识别
	public function bank_account_license($file_path)
	{
		try {
			$arr = $this->request('RecognizeBankAccountLicense', file_get_contents($file_path), true);
			if (!isset($arr['Data'])) throw new Exception('接口返回异常');
			$data = json_decode($arr['Data'], true);
			if (!is_array($data) || empty($data['data'])) {
				throw new Exception('银行开户许可证识别失败');
			}
			$d = $data['data'];
			$result = ['bank_account' => $d['bankAccount'] ?? '', 'legal_name' => $d['legalRepresentative'] ?? '', 'bank_name' => $d['depositaryBank'] ?? '', 'approval_no' => $d['approvalNumber'] ?? '', 'name' => $d['customerName'] ?? '', 'permit_no' => $d['permitNumber'] ?? ''];
			return $result;
		} catch (Exception $e) {
			throw new Exception('银行开户许可证识别失败，' . $e->getMessage());
		}
	}

	private static function formatDate($str)
	{
		return substr($str, 0, 4) . '-' . substr($str, 4, 2) . '-' . substr($str, 6, 2);
	}

	//签名方法
	private function aliyunSignature($parameters, $accessKeySecret, $method)
	{
		ksort($parameters);
		$canonicalizedQueryString = '';
		foreach ($parameters as $key => $value) {
			if ($value === null || $value instanceof \CURLFile) continue;
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
	//请求方法（当需要返回列表等数据时，returnData=true）
	private function request($action, $file = null, $returnData = false)
	{
		if (empty($this->AccessKeyId) || empty($this->AccessKeySecret)) return false;
		$url = 'https://' . $this->Endpoint . '/';
		$data = array(
			'Action' => $action,
			'Format' => 'JSON',
			'Version' => $this->Version,
			'AccessKeyId' => $this->AccessKeyId,
			'SignatureMethod' => 'HMAC-SHA1',
			'Timestamp' => gmdate('Y-m-d\TH:i:s\Z'),
			'SignatureVersion' => '1.0',
			'SignatureNonce' => $this->random(8)
		);
		$data['Signature'] = $this->aliyunSignature($data, $this->AccessKeySecret, 'POST');
		$url .= '?' . http_build_query($data);
		$ch = curl_init($url);
		curl_setopt($ch, CURLOPT_SSL_VERIFYPEER, false);
		curl_setopt($ch, CURLOPT_SSL_VERIFYHOST, false);
		curl_setopt($ch, CURLOPT_RETURNTRANSFER, true);
		curl_setopt($ch, CURLOPT_TIMEOUT, 30);
		if ($file !== null) {
			curl_setopt($ch, CURLOPT_HTTPHEADER, ['Content-Type: application/octet-stream']);
			curl_setopt($ch, CURLOPT_POST, 1);
			curl_setopt($ch, CURLOPT_POSTFIELDS, $file);
		}
		$json = curl_exec($ch);
		$httpCode = curl_getinfo($ch, CURLINFO_HTTP_CODE);
		curl_close($ch);
		if (!$json) throw new Exception('接口请求失败');
		if ($httpCode >= 200 && $httpCode < 300) {
			if ($returnData == true) {
				$arr = json_decode($json, true);
				if (!$arr) throw new Exception('无法解析返回数据');
				return $arr;
			} else {
				return true;
			}
		} else {
			$arr = json_decode($json, true);
			if (isset($arr['Code']) && isset($arr['Message'])) {
				$msg = $arr['Message'];
				if (strpos($msg, ' server string to sign is:') !== false) {
					$msg = substr($msg, 0, strpos($msg, ' server string to sign is:') - 1);
				}
				throw new Exception('[' . $arr['Code'] . '] ' . $msg);
			} else {
				throw new Exception('无法解析返回数据，' . ($json ?? ''));
			}
		}
	}
	private function random($length, $numeric = 0)
	{
		$seed = base_convert(md5(microtime() . $_SERVER['DOCUMENT_ROOT']), 16, $numeric ? 10 : 35);
		$seed = $numeric ? (str_replace('0', '', $seed) . '012340567890') : ($seed . 'zZ' . strtoupper($seed));
		$hash = '';
		$max = strlen($seed) - 1;
		for ($i = 0; $i < $length; $i++) {
			$hash .= $seed[mt_rand(0, $max)];
		}
		return $hash;
	}
}
