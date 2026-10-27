<?php

namespace app\lib;
/*
 * 快捷登录接口
 */

class Oauth
{
	private $apiurl;
	private $appid;
	private $appkey;
	private $callback;

	function __construct($config)
	{
		$this->apiurl = $config['apiurl'] . 'connect.php';
		$this->appid = $config['appid'];
		$this->appkey = $config['appkey'];
		$this->callback = $config['callback'];
	}

	//获取登录跳转url
	public function login($type, $state = '')
	{
		$keysArr = array(
			'act' => 'login',
			'appid' => $this->appid,
			'appkey' => $this->appkey,
			'type' => $type,
			'redirect_uri' => $this->callback,
			'state' => $state
		);
		$login_url = $this->apiurl . '?' . http_build_query($keysArr);
		$response = get_curl($login_url);
		if (!$response) throw new \Exception('聚合登录接口请求失败');
		$arr = json_decode($response, true);
		if (isset($arr['code']) && $arr['code'] == 0) {
			return $arr['url'] ?? '';
		} elseif (isset($arr['code'])) {
			throw new \Exception($arr['msg']);
		} else {
			throw new \Exception('获取登录数据失败');
		}
	}

	//登录成功返回网站
	public function callback($code)
	{
		$keysArr = array(
			'act' => 'callback',
			'appid' => $this->appid,
			'appkey' => $this->appkey,
			'code' => $code
		);
		$token_url = $this->apiurl . '?' . http_build_query($keysArr);
		$response = get_curl($token_url);
		if (!$response) throw new \Exception('聚合登录接口请求失败');
		$arr = json_decode($response, true);
		if (isset($arr['code']) && $arr['code'] == 0) {
			return $arr;
		} elseif (isset($arr['code'])) {
			throw new \Exception($arr['msg']);
		} else {
			throw new \Exception('获取登录数据失败');
		}
	}

	//查询用户信息
	public function query($type, $social_uid)
	{
		$keysArr = array(
			'act' => 'query',
			'appid' => $this->appid,
			'appkey' => $this->appkey,
			'type' => $type,
			'social_uid' => $social_uid
		);
		$token_url = $this->apiurl . '?' . http_build_query($keysArr);
		$response = get_curl($token_url);
		if (!$response) throw new \Exception('聚合登录接口请求失败');
		$arr = json_decode($response, true);
		if (isset($arr['code']) && $arr['code'] == 0) {
			return $arr;
		} elseif (isset($arr['code'])) {
			throw new \Exception($arr['msg']);
		} else {
			throw new \Exception('获取登录数据失败');
		}
	}
}
