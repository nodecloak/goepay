<?php

namespace plugins\mail\aliyun;

use app\common\BaseMail;

class AliyunPlugin extends BaseMail
{
    private $endpoint = 'dm.aliyuncs.com';
	private $version = '2015-11-23';

    public function send(string $to, string $subject, string $body): bool
    {
        if (empty($this->config['AccessKeyId']) || empty($this->config['AccessKeySecret']) || empty($this->config['from'])) {
            throw new \Exception('阿里云邮件推送配置不完整');
        }

        $url = 'https://' . $this->endpoint . '/';
        $data = [
            'Action' => 'SingleSendMail',
            'AccountName' => $this->config['from'],
            'ReplyToAddress' => 'false',
            'AddressType' => 1,
            'ToAddress' => $to,
            'FromAlias' => config_get('sitename'),
            'Subject' => $subject,
            'HtmlBody' => $body,
            'Format' => 'JSON',
            'Version' => $this->version,
            'AccessKeyId' => $this->config['AccessKeyId'],
            'SignatureMethod' => 'HMAC-SHA1',
            'Timestamp' => gmdate('Y-m-d\TH:i:s\Z'),
            'SignatureVersion' => '1.0',
            'SignatureNonce' => random(8),
        ];
        $data['Signature'] = $this->aliyunSignature($data, $this->config['AccessKeySecret'], 'POST');

        $ch = curl_init($url);
        curl_setopt($ch, CURLOPT_SSL_VERIFYPEER, false);
        curl_setopt($ch, CURLOPT_SSL_VERIFYHOST, false);
        curl_setopt($ch, CURLOPT_RETURNTRANSFER, true);
        curl_setopt($ch, CURLOPT_TIMEOUT, 10);
        curl_setopt($ch, CURLOPT_POST, 1);
        curl_setopt($ch, CURLOPT_POSTFIELDS, http_build_query($data));
        $response = curl_exec($ch);
        $httpCode = curl_getinfo($ch, CURLINFO_HTTP_CODE);
        curl_close($ch);

        if (!$response) {
            throw new \Exception('接口请求失败');
        }
        if ($httpCode == 200) {
            return true;
        } else {
            $arr = json_decode($response, true);
            $errMsg = $arr['Message'] ?? $response ?: '未知错误';
            throw new \Exception($errMsg);
        }
    }

    private function aliyunSignature(array $parameters, string $accessKeySecret, string $method): string
    {
        ksort($parameters);
        $canonicalizedQueryString = '';
        foreach ($parameters as $key => $value) {
            if ($value === null) continue;
            $canonicalizedQueryString .= '&' . $this->percentEncode($key) . '=' . $this->percentEncode($value);
        }
        $stringToSign = $method . '&%2F&' . $this->percentEncode(substr($canonicalizedQueryString, 1));
        return base64_encode(hash_hmac('sha1', $stringToSign, $accessKeySecret . '&', true));
    }

    private function percentEncode(string $str): string
    {
        $search = ['+', '*', '%7E'];
        $replace = ['%20', '%2A', '~'];
        return str_replace($search, $replace, urlencode($str));
    }
}
