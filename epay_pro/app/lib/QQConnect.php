<?php

namespace app\lib;

/**
 * QQ互联登录
 */
class QQConnect
{
    const GET_AUTH_CODE_URL = 'https://graph.qq.com/oauth2.0/authorize';
    const GET_ACCESS_TOKEN_URL = 'https://graph.qq.com/oauth2.0/token';
    const GET_OPENID_URL = 'https://graph.qq.com/oauth2.0/me';

    private $appid;
    private $appkey;
    private $callback;

    function __construct($QC_config)
    {
        $this->appid = $QC_config['appid'];
        $this->appkey = $QC_config['appkey'];
        $this->callback = $QC_config['callback'];
    }

    public function login($state = '')
    {
        $keysArr = [
            'response_type' => 'code',
            'client_id' => $this->appid,
            'redirect_uri' => $this->callback,
            'state' => $state
        ];
        $login_url =  self::GET_AUTH_CODE_URL . '?' . http_build_query($keysArr);
        return $login_url;
    }

    public function callback($code)
    {
        $keysArr = [
            'grant_type' => 'authorization_code',
            'client_id' => $this->appid,
            'client_secret' => $this->appkey,
            'code' => $code,
            'redirect_uri' => $this->callback,
            'fmt' => 'json',
            'need_openid' => '1',
        ];
        $token_url = self::GET_ACCESS_TOKEN_URL . '?' . http_build_query($keysArr);
        $response = get_curl($token_url);
        if (!$response) throw new \Exception('QQ互联登录失败，请求失败');
        $arr = json_decode($response, true);
        if (isset($arr['access_token'])) {
            return [$arr['access_token'], $arr['openid']];
        } elseif (isset($arr['error'])) {
            throw new \Exception('QQ互联登录失败 [' . $arr['error'] . ']' . $arr['error_description']);
        } else {
            throw new \Exception('QQ互联登录失败，原因未知');
        }
    }
}
