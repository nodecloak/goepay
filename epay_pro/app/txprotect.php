<?php
//屏蔽各种蜘蛛与非正常浏览器
$user_agent = request()->header('user-agent', '');
$referer = request()->header('referer', '');
$accept = request()->header('accept');
$accept_language = request()->header('accept-language');
if ($allow_search ?? false) {
	if (strpos($user_agent, 'Baiduspider') !== false || strpos($user_agent, '360Spider') !== false || strpos($user_agent, 'YisouSpider') !== false || strpos($user_agent, 'Sogou web spider') !== false || strpos($user_agent, 'Sogou inst spider') !== false || strpos($user_agent, 'bingbot/') !== false || strpos($user_agent, 'Googlebot/') !== false || strpos($user_agent, 'Bytespider') !== false) {
		return;
	}
}
if (strpos($user_agent, 'Baiduspider') !== false || strpos($user_agent, '360Spider') !== false || strpos($user_agent, 'YisouSpider') !== false || strpos($user_agent, 'Sogou web spider') !== false || strpos($user_agent, 'Sogou inst spider') !== false || strpos($user_agent, 'bingbot/') !== false || strpos($user_agent, 'Googlebot/') !== false || strpos($user_agent, 'Bytespider') !== false || strpos($user_agent, 'GPTBot/') !== false || strpos($user_agent, 'python-requests') !== false || strpos($user_agent, 'aiohttp/') !== false || strpos($user_agent, 'MJ12bot') !== false || strpos($user_agent, 'SemrushBot') !== false || strpos($user_agent, 'AhrefsBot') !== false || strpos($user_agent, 'DotBot') !== false || strpos($user_agent, 'CensysInspect/') !== false || strpos($referer, '.tr.com') !== false || strpos($referer, '.wsd.com') !== false || strpos($referer, '.oa.com') !== false || strpos($referer, '.cm.com') !== false || strpos($referer, '/membercomprehensive/') !== false || strpos($referer, 'www.internalrequests.org') !== false || !isset($accept) || empty($user_agent) || stripos($user_agent, 'manager') !== false || strpos($user_agent, 'ozilla') !== false && strpos($user_agent, 'Mozilla') === false || strpos($user_agent, "Windows NT 6.1") !== false && $accept == '*/*' || strpos($user_agent, "Windows NT 5.1") !== false && $accept == '*/*' || strpos($accept, "vnd.wap.wml") !== false && strpos($user_agent, "Windows NT 5.1") !== false || strpos($user_agent, "Alibaba.Security.Heimdall") !== false || strpos($user_agent, 'wechatdevtools/') !== false || strpos($user_agent, 'libcurl/') !== false || strpos($user_agent, 'Go-http-client') !== false || strpos($user_agent, 'HeadlessChrome') !== false || strpos($user_agent, 'iPhone OS 9_1') !== false && request()->header('connection') == 'close' || strpos($user_agent, 'Chrome/359.0.0.288') !== false || strpos($user_agent, 'iPhone OS 11_0') !== false && strpos($accept_language, 'en') !== false && strpos($accept_language, 'zh') === false || strpos($user_agent, ' QQ/') !== false && strpos($user_agent, ' Edg/') !== false) {
	header("HTTP/1.1 404 Not Found");
	exit;
}
$remoteiplong = bindec(decbin(ip2long(request()->clientip)));
$iptables = '979632128~979763199|1909850112~1909981183|2110914560~2110980095|3664510976~3664642047|3679584256~3679649791|995233792~995237887|2105150464~2105208831|3078732898|2061546274';
foreach (explode('|', $iptables) as $iprows) {
	if (strpos($iprows, '~')) {
		$ipbanrange = explode('~', $iprows);
		if ($remoteiplong >= $ipbanrange[0] && $remoteiplong <= $ipbanrange[1]) {
			header("HTTP/1.1 404 Not Found");
			exit;
		}
	} elseif ($remoteiplong == $iprows) {
		header("HTTP/1.1 404 Not Found");
		exit;
	}
}
