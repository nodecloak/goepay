<?php

namespace plugins\mail\sendcloud;

use app\common\BaseMail;

class SendcloudPlugin extends BaseMail
{
    public function send(string $to, string $subject, string $body): bool
    {
        if (empty($this->config['api_user']) || empty($this->config['api_key']) || empty($this->config['from'])) {
            throw new \Exception('SendCloud 发信配置不完整');
        }

        $url = 'https://api.sendcloud.net/apiv2/mail/send';
        $data = [
            'apiUser' => $this->config['api_user'],
            'apiKey' => $this->config['api_key'],
            'from' => $this->config['from'],
            'fromName' => config_get('sitename'),
            'to' => $to,
            'subject' => $subject,
            'html' => $body,
        ];

        $ch = curl_init($url);
        curl_setopt($ch, CURLOPT_RETURNTRANSFER, true);
        curl_setopt($ch, CURLOPT_TIMEOUT, 10);
        curl_setopt($ch, CURLOPT_POST, 1);
        curl_setopt($ch, CURLOPT_POSTFIELDS, $data);
        $json = curl_exec($ch);
        curl_close($ch);

        if ($json === false) {
            throw new \Exception('接口返回异常');
        }

        $arr = json_decode($json, true);
        if (($arr['statusCode'] ?? 0) == 200) {
            return true;
        }
        $errMsg = $arr['message'] ?? '';
        $errMsg = is_array($errMsg) ? implode("\n", $errMsg) : (is_string($errMsg) ? $errMsg : '未知错误');
        throw new \Exception($errMsg);
    }
}
