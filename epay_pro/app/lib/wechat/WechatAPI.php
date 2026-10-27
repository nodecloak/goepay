<?php

namespace app\lib\wechat;

use Exception;
use think\facade\Db;

class WechatAPI
{
    private $wid;
    private $accessToken;
    private $jsapiTicket;

    public function __construct($id)
    {
        $this->wid = $id;
    }

    public function getAccessToken($force = false)
    {
        if (!empty($this->accessToken)) return $this->accessToken;
        Db::startTrans();
        try {
            $row = Db::name('weixin')->where('id', $this->wid)->lock(true)->find();
            if (!$row) throw new Exception('记录不存在');
            if ($row['access_token'] && strtotime($row['expiretime']) - 200 >= time() && !$force) {
                Db::rollback();
                $this->accessToken = $row['access_token'];
                return $this->accessToken;
            }
            $appid = $row['appid'];
            $secret = $row['appsecret'];
            $url = "https://api.weixin.qq.com/cgi-bin/stable_token";
            $post = json_encode(['grant_type' => 'client_credential', 'appid' => $appid, 'secret' => $secret]);
            $output = get_curl($url, $post);
            $res = json_decode($output, true);
            if (isset($res['access_token'])) {
                $this->accessToken = $res['access_token'];
                $expire_time = time() + $res['expires_in'];
                Db::name('weixin')->where('id', $this->wid)->update(['access_token' => $this->accessToken, 'updatetime' => date('Y-m-d H:i:s'), 'expiretime' => date("Y-m-d H:i:s", $expire_time)]);
            } elseif (isset($res['errmsg'])) {
                throw new Exception('AccessToken获取失败：' . $res['errmsg']);
            } else {
                throw new Exception('AccessToken获取失败');
            }
            Db::commit();
            return $this->accessToken;
        } catch (Exception $e) {
            Db::rollback();
            throw $e;
        }
    }

    public function generate_scheme($path, $query, $expire = 600)
    {
        $access_token = $this->getAccessToken();
        $url = "https://api.weixin.qq.com/wxa/generatescheme?access_token=" . $access_token;
        $data = ['jump_wxa' => ['path' => $path, 'query' => $query]];
        if ($expire > 0) {
            $data['is_expire'] = true;
            $data['expire_time'] = time() + $expire;
        }
        $output = get_curl($url, json_encode($data));
        $res = json_decode($output, true);
        if (isset($res['errcode']) && $res['errcode'] == 0) {
            return $res['openlink'] ?? '';
        } else {
            throw new Exception('urlscheme生成失败：' . ($res['errmsg'] ?? $output ?? '未知错误'));
        }
    }

    public function generate_link($path, $query, $expire = 600)
    {
        $access_token = $this->getAccessToken();
        $url = "https://api.weixin.qq.com/wxa/generate_urllink?access_token=" . $access_token;
        $data = ['path' => $path, 'query' => $query];
        if ($expire > 0) {
            $data['expire_type'] = 0;
            $data['expire_time'] = time() + $expire;
        }
        $output = get_curl($url, json_encode($data));
        $res = json_decode($output, true);
        if (isset($res['errcode']) && $res['errcode'] == 0) {
            return $res['url_link'] ?? '';
        } else {
            throw new Exception('url_link生成失败：' . ($res['errmsg'] ?? $output ?? '未知错误'));
        }
    }

    public function getPhoneNumber($code)
    {
        $access_token = $this->getAccessToken();
        $url = "https://api.weixin.qq.com/wxa/business/getuserphonenumber?access_token=" . $access_token;
        $data = ['code' => $code];
        $output = get_curl($url, json_encode($data));
        $res = json_decode($output, true);
        if (isset($res['errcode']) && $res['errcode'] == 0) {
            return $res['phone_info'] ?? [];
        } else {
            throw new Exception('获取手机号码失败：' . ($res['errmsg'] ?? $output ?? '未知错误'));
        }
    }

    //发送微信公众号模板消息
    public function sendTemplateMessage($openid, $template_id, $jumpurl, $data)
    {
        $access_token = $this->getAccessToken();
        $url = 'https://api.weixin.qq.com/cgi-bin/message/template/send?access_token=' . $access_token;
        $post = [
            'touser' => $openid,
            'template_id' => $template_id,
            'url' => $jumpurl,
            'data' => $data
        ];
        $response = get_curl($url, json_encode($post));
        $res = json_decode($response, true);
        if (isset($res['errcode']) && $res['errcode'] == 0) {
            return true;
        } else {
            throw new Exception('模板消息发送失败：' . ($res['errmsg'] ?? $response ?? '未知错误'));
        }
    }

    public function getJsapiTicket($force = false)
    {
        if (!empty($this->jsapiTicket)) return $this->jsapiTicket;

        $cachekey = 'wx_jsapi_ticket_' . $this->wid;
        $row = cache($cachekey);
        if ($row && !empty($row['ticket']) && strtotime($row['expiretime']) - 200 >= time() && !$force) {
            $this->jsapiTicket = $row['ticket'];
            return $this->jsapiTicket;
        }

        $access_token = $this->getAccessToken();
        $url = 'https://api.weixin.qq.com/cgi-bin/ticket/getticket?access_token=' . $access_token . '&type=jsapi';
        $output = get_curl($url);
        $res = json_decode($output, true);
        if (isset($res['ticket'])) {
            $this->jsapiTicket = $res['ticket'];
            $expire_time = time() + $res['expires_in'];
            cache($cachekey, ['ticket' => $this->jsapiTicket, 'expiretime' => date("Y-m-d H:i:s", $expire_time)], $res['expires_in']);
        } elseif (isset($res['errmsg'])) {
            throw new Exception('JsapiTicket获取失败：' . $res['errmsg']);
        } else {
            throw new Exception('JsapiTicket获取失败：' . ($output ?? '未知错误'));
        }
        return $this->jsapiTicket;
    }

    public function getJsapiConfig($appid, $url, $jsApiList, $debug = false)
    {
        $ticket = $this->getJsapiTicket();
        $data = [
            'jsapi_ticket' => $ticket,
            'timestamp' => time(),
            'noncestr' => random(16),
            'url' => $url
        ];
        $config = [
            'debug' => $debug,
            'appId' => $appid,
            'timestamp' => $data['timestamp'],
            'nonceStr' => $data['noncestr'],
            'signature' => $this->getSignature($data),
            'jsApiList' => $jsApiList
        ];
        return $config;
    }

    public function getSignature($data)
    {
        ksort($data);
        $params = array();
        foreach ($data as $key => $value) {
            $params[] = "{$key}={$value}";
        }
        return sha1(join('&', $params));
    }

    //发送客服消息
    public function sendCustomMessage($openid, $msgtype, $content)
    {
        $access_token = $this->getAccessToken();
        $url = 'https://api.weixin.qq.com/cgi-bin/message/custom/send?access_token=' . $access_token;
        $data = [
            'touser' => $openid,
            'msgtype' => $msgtype,
            $msgtype => $content
        ];
        $output = get_curl($url, json_encode($data, JSON_UNESCAPED_UNICODE | JSON_UNESCAPED_SLASHES));
        $res = json_decode($output, true);
        if (isset($res['errcode']) && $res['errcode'] == 0) {
            return true;
        } else {
            throw new Exception('发送客服消息失败：' . ($res['errmsg'] ?? $output ?? '未知错误'));
        }
    }
}
