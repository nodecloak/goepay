<?php

namespace lib\douyinpay;

use Exception;

class DouyinOauth
{
    private string $client_key;
    private string $client_secret;
    private string $accessToken;
    private string $jsapiTicket;

    public function __construct(string $client_key, string $client_secret)
    {
        $this->client_key = $client_key;
        $this->client_secret = $client_secret;
    }

    /**
     * 抖音授权回调 URL 不可带查询参数时，将 channel、可选 sid 写入 JSON 再 base64 作为 OAuth state。
     */
    public static function encodeOpenidState(int $channelId, ?string $sid = null): string
    {
        $payload = ['channel' => $channelId];
        if ($sid !== null && $sid !== '') {
            $payload['sid'] = $sid;
        }
        return base64_encode(json_encode($payload, JSON_UNESCAPED_UNICODE));
    }

    /**
     * @return array{channel:int,sid?:string}|null
     */
    public static function decodeOpenidState(string $state): ?array
    {
        $raw = base64_decode($state, true);
        if ($raw === false) {
            return null;
        }
        $data = json_decode($raw, true);
        if (!is_array($data) || !isset($data['channel']) || !is_numeric($data['channel'])) {
            return null;
        }
        $data['channel'] = (int) $data['channel'];
        return $data;
    }

    public function get_openid(string $redirect_uri, ?string $state = null)
    {
        if (isset($_GET['code'])) {
            $code = $_GET['code'];
            $data = $this->callback($code);
            return $data['open_id'];
        } else {
            $this->login($redirect_uri, $state);
        }
    }

    public function login(string $redirect_uri, ?string $state = null): void
    {
        $param = [
            "client_key" => $this->client_key,
            "scope" => "login_id",
            "response_type" => "code",
            "redirect_uri" => $redirect_uri,
        ];
        if ($state) $param['state'] = $state;
        $url = 'https://aweme.snssdk.com/passport/open/silent_auth/?' . http_build_query($param);
        Header("Location: $url");
        exit;
    }

    public function callback(string $code): array
    {
        $param = [
            "client_key" => $this->client_key,
            "client_secret" => $this->client_secret,
            "code" => $code,
            "grant_type" => "authorization_code"
        ];
        $url = 'https://open.douyin.com/oauth/access_token/';
        $res = get_curl($url, http_build_query($param));
        if (!$res) throw new Exception('Openid获取失败，请求失败');
        $data = json_decode($res, true);
        if (isset($data['data']['error_code']) && $data['data']['error_code'] == 0) {
            return $data['data'];
        } elseif (isset($data['data']['error_code'])) {
            throw new Exception('Openid获取失败 [' . $data['data']['error_code'] . ']' . $data['data']['description']);
        } else {
            throw new Exception('Openid获取失败，原因未知');
        }
    }

    public function getAccessToken($force = false)
    {
        global $CACHE;
        if (!empty($this->accessToken)) return $this->accessToken;
        $cachekey = 'douyin_' . $this->client_key;
        $row = $CACHE->read($cachekey);
        if ($row) {
            $row = unserialize($row);
            if ($row['access_token'] && strtotime($row['expiretime']) - 200 >= time() && !$force) {
                $this->accessToken = $row['access_token'];
                return $this->accessToken;
            }
        }

        $url = "https://open.douyin.com/oauth/client_token/";
        $post = [
            'grant_type' => 'client_credential',
            'client_key' => $this->client_key,
            'client_secret' => $this->client_secret
        ];
        $output = get_curl($url, json_encode($post), 0, 0, 0, 0, 0, ['Content-Type: application/json']);
        if (!$output) throw new Exception('AccessToken获取失败，请求失败');
        $res = json_decode($output, true);
        if (isset($res['message']) && $res['message'] == 'success' && isset($res['data']['access_token'])) {
            $this->accessToken = $res['data']['access_token'];
            $expire_time = time() + $res['data']['expires_in'];
            $CACHE->save($cachekey, ['access_token' => $this->accessToken, 'expiretime' => date("Y-m-d H:i:s", $expire_time)], $res['data']['expires_in']);
        } elseif (isset($res['data']['description'])) {
            throw new Exception('AccessToken获取失败：' . $res['data']['description']);
        } else {
            throw new Exception('AccessToken获取失败');
        }
        return $this->accessToken;
    }

    public function getJsapiTicket($force = false)
    {
        global $CACHE;
        if (!empty($this->jsapiTicket)) return $this->jsapiTicket;

        $cachekey = 'douyin_js_' . $this->client_key;
        $row = $CACHE->read($cachekey);
        if ($row) {
            $row = unserialize($row);
            if ($row['ticket'] && strtotime($row['expiretime']) - 200 >= time() && !$force) {
                $this->jsapiTicket = $row['ticket'];
                return $this->jsapiTicket;
            }
        }

        $access_token = $this->getAccessToken();
        $url = 'https://open.douyin.com/js/getticket/?access_token=' . $access_token . '&type=jsapi';
        $output = get_curl($url, 0, 0, 0, 0, 0, 0, ['Content-Type: application/json']);
        if (!$output) throw new Exception('jsapiTicket获取失败，请求失败');
        $res = json_decode($output, true);
        if (isset($res['data']['error_code']) && $res['data']['error_code'] == 0 && isset($res['data']['ticket'])) {
            $this->jsapiTicket = $res['data']['ticket'];
            $expire_time = time() + $res['data']['expires_in'];
            $CACHE->save($cachekey, ['ticket' => $this->jsapiTicket, 'expiretime' => date("Y-m-d H:i:s", $expire_time)], $res['expires_in']);
        } elseif (isset($res['data']['description'])) {
            throw new Exception('jsapiTicket获取失败：' . $res['data']['description']);
        } else {
            throw new Exception('jsapiTicket获取失败');
        }
        return $this->jsapiTicket;
    }

    public function getSdkConfig($url)
    {
        $ticket = $this->getJsapiTicket();
        $data = [
            'jsapi_ticket' => $ticket,
            'timestamp' => (string)time(),
            'nonce_str' => random(16),
            'url' => $url
        ];
        $config = [
            'client_key' => $this->client_key,
            'signature' => $this->getSignature($data),
            'timestamp' => $data['timestamp'],
            'nonce_str' => $data['nonce_str'],
            'url' => $url
        ];
        return $config;
    }

    private function getSignature($data)
    {
        ksort($data);
        $params = array();
        foreach ($data as $key => $value) {
            $params[] = "{$key}={$value}";
        }
        return md5(join('&', $params));
    }
}
