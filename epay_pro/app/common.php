<?php
// 应用公共文件
use think\facade\Db;
use think\facade\Request;
use think\facade\Config;
use think\facade\Session;
use think\facade\Log;

function get_curl($url, $post = null, $referer = null, $cookie = null, $header = null, $ua = null, $nobaody = null, $addheader = null, $location = null)
{
    $ch = curl_init();
    curl_setopt($ch, CURLOPT_URL, $url);
    curl_setopt($ch, CURLOPT_SSL_VERIFYPEER, false);
    curl_setopt($ch, CURLOPT_SSL_VERIFYHOST, false);
    $httpheader[] = "Accept: */*";
    $httpheader[] = "Accept-Encoding: gzip,deflate,sdch";
    $httpheader[] = "Accept-Language: zh-CN,zh;q=0.8";
    $httpheader[] = "Connection: close";
    if ($addheader) {
        $httpheader = array_merge($httpheader, $addheader);
    }
    curl_setopt($ch, CURLOPT_HTTPHEADER, $httpheader);
    if ($post) {
        curl_setopt($ch, CURLOPT_POST, 1);
        curl_setopt($ch, CURLOPT_POSTFIELDS, $post);
    }
    if ($header) {
        curl_setopt($ch, CURLOPT_HEADER, true);
    }
    if ($cookie) {
        curl_setopt($ch, CURLOPT_COOKIE, $cookie);
    }
    if ($referer) {
        curl_setopt($ch, CURLOPT_REFERER, $referer);
    }
    if ($ua) {
        curl_setopt($ch, CURLOPT_USERAGENT, $ua);
    } else {
        curl_setopt($ch, CURLOPT_USERAGENT, "Mozilla/5.0 (Linux; U; Android 4.0.4; es-mx; HTC_One_X Build/IMM76D) AppleWebKit/534.30 (KHTML, like Gecko) Version/4.0");
    }
    if ($nobaody) {
        curl_setopt($ch, CURLOPT_NOBODY, 1);
    }
    if ($location) {
        curl_setopt($ch, CURLOPT_FOLLOWLOCATION, true);
    }
    curl_setopt($ch, CURLOPT_TIMEOUT, 60);
    curl_setopt($ch, CURLOPT_ENCODING, "gzip");
    curl_setopt($ch, CURLOPT_RETURNTRANSFER, 1);
    $ret = curl_exec($ch);
    curl_close($ch);
    return $ret;
}
function real_ip($type = 0)
{
    $ip = $_SERVER['REMOTE_ADDR'];
    if ($type <= 0 && isset($_SERVER['HTTP_X_FORWARDED_FOR']) && !empty($_SERVER['HTTP_X_FORWARDED_FOR'])) {
        $ips = explode(',', $_SERVER['HTTP_X_FORWARDED_FOR']);
        foreach ($ips as $xip) {
            $xip = trim($xip);
            if (filter_var($xip, FILTER_VALIDATE_IP, FILTER_FLAG_NO_PRIV_RANGE | FILTER_FLAG_NO_RES_RANGE)) {
                $ip = $xip;
                break;
            }
        }
    } elseif ($type <= 0 && isset($_SERVER['HTTP_CLIENT_IP']) && filter_var($_SERVER['HTTP_CLIENT_IP'], FILTER_VALIDATE_IP, FILTER_FLAG_NO_PRIV_RANGE | FILTER_FLAG_NO_RES_RANGE)) {
        $ip = $_SERVER['HTTP_CLIENT_IP'];
    } elseif ($type <= 1 && isset($_SERVER['HTTP_CF_CONNECTING_IP']) && filter_var($_SERVER['HTTP_CF_CONNECTING_IP'], FILTER_VALIDATE_IP, FILTER_FLAG_NO_PRIV_RANGE | FILTER_FLAG_NO_RES_RANGE)) {
        $ip = $_SERVER['HTTP_CF_CONNECTING_IP'];
    } elseif ($type <= 1 && isset($_SERVER['HTTP_X_REAL_IP']) && filter_var($_SERVER['HTTP_X_REAL_IP'], FILTER_VALIDATE_IP, FILTER_FLAG_NO_PRIV_RANGE | FILTER_FLAG_NO_RES_RANGE)) {
        $ip = $_SERVER['HTTP_X_REAL_IP'];
    }
    return $ip;
}
if (!function_exists("is_https")) {
    function is_https()
    {
        if (isset($_SERVER['SERVER_PORT']) && $_SERVER['SERVER_PORT'] == 443) {
            return true;
        } elseif (isset($_SERVER['HTTPS']) && (strtolower($_SERVER['HTTPS']) == 'on' || $_SERVER['HTTPS'] == '1')) {
            return true;
        } elseif (isset($_SERVER['HTTP_X_CLIENT_SCHEME']) && $_SERVER['HTTP_X_CLIENT_SCHEME'] == 'https') {
            return true;
        } elseif (isset($_SERVER['HTTP_X_FORWARDED_PROTO']) && $_SERVER['HTTP_X_FORWARDED_PROTO'] == 'https') {
            return true;
        } elseif (isset($_SERVER['REQUEST_SCHEME']) && $_SERVER['REQUEST_SCHEME'] == 'https') {
            return true;
        } elseif (isset($_SERVER['HTTP_EWS_CUSTOME_SCHEME']) && $_SERVER['HTTP_EWS_CUSTOME_SCHEME'] == 'https') {
            return true;
        }
        return false;
    }
}
function get_ip_region($ip)
{
    if (filter_var($ip, FILTER_VALIDATE_IP, FILTER_FLAG_IPV4)) {
        $family = \app\lib\Ip2region::IPV4;
        $dbFile = app()->getBasePath() . 'data/ip2region_v4.xdb';
    } elseif (filter_var($ip, FILTER_VALIDATE_IP, FILTER_FLAG_IPV6)) {
        $family = \app\lib\Ip2region::IPV6;
        $dbFile = app()->getBasePath() . 'data/ip2region_v6.xdb';
    } else {
        return false;
    }
    if (!is_file($dbFile)) {
        return false;
    }
    try {
        $searcher = \app\lib\Ip2region::newWithFileOnly($family, $dbFile);
        $result = $searcher->search($ip);
        if ($result) {
            $region = explode('|', $result);
            if (count($region) < 3) {
                return false;
            }
            return [
                'country' => $region[0],
                'province' => $region[1],
                'city' => $region[2],
            ];
        }
    } catch (\Exception $e) {
    }
    return false;
}

function get_ip_city($ip)
{
    $r = get_ip_region($ip);
    if ($r) {
        return ($r['country'] == '中国' ? '' : $r['country']) . ($r['province'] ?? '') . ($r['city'] ?? '');
    }
    return '';
}

function send_mail($to, $subject, $body)
{
    $mail_api = config_get('mail_api');
    if (empty($mail_api)) {
        return '未开启邮件发送';
    }
    try {
        \app\lib\Plugin::call('mail', $mail_api, 'send', $to, $subject, $body);
    } catch (\Throwable $e) {
        exception_log($e, 'send_mail');
        return $e->getMessage();
    }
    return true;
}
function send_sms($phone, $tpl_code, $tpl_param)
{
    $sms_api = config_get('sms_api');
    if (empty($sms_api)) {
        return '未开启短信发送';
    }
    try {
        \app\lib\Plugin::call('sms', $sms_api, 'send', $phone, $tpl_code, $tpl_param);
    } catch (\Throwable $e) {
        exception_log($e, 'send_sms');
        return $e->getMessage();
    }
    return true;
}

function checkmobile()
{
    $useragent = strtolower(request()->header('user-agent', ''));
    $ualist = array('android', 'midp', 'nokia', 'mobile', 'iphone', 'ipod', 'blackberry', 'windows phone');
    foreach ($ualist as $ua) {
        if (str_contains($useragent, $ua)) return true;
    }
    if (isset($_SERVER['HTTP_ACCEPT']) && str_contains($_SERVER['HTTP_ACCEPT'], "VND.WAP") || isset($_SERVER['HTTP_VIA']) && str_contains($_SERVER['HTTP_VIA'], "wap")) return true;
    return false;
}
function checkwechat()
{
    if (str_contains(request()->header('user-agent', ''), 'MicroMessenger/') && !str_contains(request()->header('user-agent', ''), 'WindowsWechat'))
        return true;
    else
        return false;
}
function checkalipay()
{
    if (str_contains(request()->header('user-agent', ''), 'AlipayClient/'))
        return true;
    else
        return false;
}
function checkmobileqq()
{
    if (str_contains(request()->header('user-agent', ''), 'QQ/'))
        return true;
    else
        return false;
}
function checkunionpay()
{
    if (str_contains(request()->header('user-agent', ''), 'UnionPay/'))
        return true;
    else
        return false;
}
function get_unionpay_ua()
{
    if (preg_match('/UnionPay\/([0-9\.]+) ([a-zA-Z0-9]+)/', request()->header('user-agent', ''), $matches)) {
        return $matches[0];
    }
    return 'UnionPay/1.0 CloudPay';
}
function checkdouyin()
{
    if (str_contains(request()->header('user-agent', ''), 'aweme'))
        return true;
    else
        return false;
}
function getMobileClientType()
{
    if (checkalipay()) {
        return 'alipay';
    } elseif (checkwechat()) {
        return 'wechat';
    } elseif (checkmobileqq()) {
        return 'qq';
    } elseif (checkdouyin()) {
        return 'douyin';
    } elseif (checkunionpay()) {
        return 'unionpay';
    }
    return '';
}

function authcode($string, $operation = 'DECODE', $key = '', $expiry = 0)
{
    $ckey_length = 4;
    $key = md5($key);
    $keya = md5(substr($key, 0, 16));
    $keyb = md5(substr($key, 16, 16));
    $keyc = $ckey_length ? ($operation == 'DECODE' ? substr($string, 0, $ckey_length) : substr(md5(microtime()), -$ckey_length)) : '';
    $cryptkey = $keya . md5($keya . $keyc);
    $key_length = strlen($cryptkey);
    $string = $operation == 'DECODE' ? base64_decode(substr($string, $ckey_length)) : sprintf('%010d', $expiry ? $expiry + time() : 0) . substr(md5($string . $keyb), 0, 16) . $string;
    $string_length = strlen($string);
    $result = '';
    $box = range(0, 255);
    $rndkey = array();
    for ($i = 0; $i <= 255; $i++) {
        $rndkey[$i] = ord($cryptkey[$i % $key_length]);
    }
    for ($j = $i = 0; $i < 256; $i++) {
        $j = ($j + $box[$i] + $rndkey[$i]) % 256;
        $tmp = $box[$i];
        $box[$i] = $box[$j];
        $box[$j] = $tmp;
    }
    for ($a = $j = $i = 0; $i < $string_length; $i++) {
        $a = ($a + 1) % 256;
        $j = ($j + $box[$a]) % 256;
        $tmp = $box[$a];
        $box[$a] = $box[$j];
        $box[$j] = $tmp;
        $result .= chr(ord($string[$i]) ^ ($box[($box[$a] + $box[$j]) % 256]));
    }
    if ($operation == 'DECODE') {
        if (((int)substr($result, 0, 10) == 0 || (int)substr($result, 0, 10) - time() > 0) && substr($result, 10, 16) == substr(md5(substr($result, 26) . $keyb), 0, 16)) {
            return substr($result, 26);
        } else {
            return '';
        }
    } else {
        return $keyc . str_replace('=', '', base64_encode($result));
    }
}
function random($length, $numeric = 0)
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

function getSid()
{
    return md5(uniqid(mt_rand(), true) . microtime());
}
function getMd5Pwd($pwd, $salt = null)
{
    return md5(md5($pwd) . md5('1277180438' . $salt));
}
function getMillisecond()
{
    list($s1, $s2) = explode(' ', microtime());
    return sprintf('%.0f', (floatval($s1) + floatval($s2)) * 1000);
}
function getSubstr($str, $leftStr, $rightStr)
{
    $left = strpos($str, $leftStr);
    if ($left === false) return '';
    $start = $left + strlen($leftStr);
    $right = strpos($str, $rightStr, $start);
    if ($right !== false) {
        return substr($str, $start, $right - $start);
    } else {
        return substr($str, $start);
    }
}
function isNullOrEmpty($str)
{
    return $str === null || $str === '';
}

/**
 * 过滤 utf8mb4 四字节字符（如 emoji），兼容 utf8 存储
 */
function filter_utf8mb4($str)
{
    if ($str === null || $str === '') {
        return $str;
    }
    return preg_replace('/[\x{10000}-\x{10FFFF}]/u', '', (string)$str);
}

function config_get(string $key, mixed $default = null, bool $force = false): mixed
{
    if ($force) {
        $value = Db::name('config')->where('k', $key)->value('v');
    } else {
        $value = config('sys.' . $key);
    }
    return isNullOrEmpty($value) ? $default : $value;
}

function config_set(string $key, mixed $value, bool $cache = true): bool
{
    if (is_null($value)) {
        Db::name('config')->where('k', $key)->delete();
        return true;
    }
    if ($cache) {
        $res = Db::name('config')->replace()->insert(['k' => $key, 'v' => $value]);
    } else {
        $res = Db::name('config')->replace()->insert(['k' => $key, 'v' => $value, 'cache' => 0]);
    }
    return $res !== false;
}

function plugin_config_get(string $type, string $name): array
{
    $key = 'plugin_' . $type . '_' . $name;
    $config = cache($key);
    if (empty($config) || !is_array($config)) {
        $value = config_get($key, null, true);
        if (empty($value)) {
            $config = [];
        } else {
            $config = unserialize($value);
        }
        cache($key, $config, 0);
    }
    return $config;
}

function plugin_config_set(string $type, string $name, ?array $config): void
{
    $key = 'plugin_' . $type . '_' . $name;
    if (is_null($config)) {
        Db::name('config')->where('k', $key)->delete();
    } else {
        $value = serialize($config);
        config_set($key, $value, false);
    }
    cache($key, null);
}

function getdomain($url)
{
    $arr = parse_url($url);
    if (!isset($arr['host'])) return null;
    $host = $arr['host'];
    if (isset($arr['port']) && $arr['port'] != 80 && $arr['port'] != 443) $host .= ':' . $arr['port'];
    return $host;
}
function get_host($url)
{
    $arr = parse_url($url);
    return $arr['host'];
}
function get_main_host($url)
{
    $arr = parse_url($url);
    $host = $arr['host'];
    if (filter_var($host, FILTER_VALIDATE_IP)) return $host;
    if (substr_count($host, '.') > 1) {
        $host = substr($host, strpos($host, '.') + 1);
    }
    return $host;
}

function checkBlockUser($openid, $trade_no)
{
    Db::name('order')->where('trade_no', $trade_no)->update(['buyer' => $openid]);
    $black = Db::name('blacklist')->where(['type' => 0, 'content' => $openid])->find();
    if ($black) {
        return ['type' => 'error', 'msg' => config_get('blackmsg', '系统异常无法完成付款')];
    }
    if (config_get('pay_userlimit') > 0) {
        $usercount = Db::name('order')->where('buyer', $openid)->where('date', date('Y-m-d'))->where('status', '>', 0)->count();
        if ($usercount >= config_get('pay_userlimit')) {
            return ['type' => 'error', 'msg' => '你今天已无法再发起支付，请明天再试'];
        }
    }
    if (config_get('pay_daymoney') > 0) {
        $daymoney = Db::name('order')->where('buyer', $openid)->where('date', date('Y-m-d'))->where('status', '>', 0)->sum('realmoney');
        if ($daymoney >= config_get('pay_daymoney')) {
            return ['type' => 'error', 'msg' => '你今天已累积支付金额超过' . config_get('pay_daymoney') . '元，请明天再试'];
        }
    }
    return false;
}

function changeUserMoney($uid, $money, $add = true, $type = null, $orderid = null)
{
    if ($money <= 0) return;
    if ($type == '代付退回' && !empty($orderid)) {
        $isrefund = Db::name('record')->where(['uid' => $uid, 'type' => '代付退回', 'trade_no' => $orderid])->value('id');
        if ($isrefund) return;
    }
    Db::startTrans();
    try {
        $oldmoney = Db::name('user')->where('uid', $uid)->lock(true)->value('money');
        if ($oldmoney === null) {
            Db::rollback();
            return false;
        }
        if ($add == true) {
            $action = 1;
            $newmoney = round($oldmoney + $money, 2);
        } else {
            $action = 2;
            $newmoney = round($oldmoney - $money, 2);
        }
        $res = Db::name('user')->where('uid', $uid)->update(['money' => $newmoney]);
        Db::name('record')->insert(['uid' => $uid, 'action' => $action, 'money' => $money, 'oldmoney' => $oldmoney, 'newmoney' => $newmoney, 'type' => $type, 'trade_no' => $orderid, 'date' => date('Y-m-d H:i:s')]);
        Db::commit();
        return $res;
    } catch (\Throwable $e) {
        Db::rollback();
        throw $e;
    }
}

function changeUserMoney2($uid, $oldmoney, $money, $add = true, $type = null, $orderid = null)
{
    if ($money <= 0) return;
    if ($add == true) {
        $action = 1;
        $newmoney = round($oldmoney + $money, 2);
    } else {
        $action = 2;
        $newmoney = round($oldmoney - $money, 2);
    }
    $res = Db::name('user')->where('uid', $uid)->update(['money' => $newmoney]);
    Db::name('record')->insert(['uid' => $uid, 'action' => $action, 'money' => $money, 'oldmoney' => $oldmoney, 'newmoney' => $newmoney, 'type' => $type, 'trade_no' => $orderid, 'date' => date('Y-m-d H:i:s')]);
    return $res;
}

function changeUserGroup($uid, $gid, $endtime = null)
{
    return Db::name('user')->where('uid', $uid)->update(['gid' => $gid, 'endtime' => $endtime ?: null]);
}

function is_idcard($id)
{
    $id = strtoupper($id);
    $regx = "/(^\d{17}([0-9]|X)$)/";
    $arr_split = array();
    if (strlen($id) != 18 || !preg_match($regx, $id)) {
        return false;
    }
    $regx = "/^(\d{6})+(\d{4})+(\d{2})+(\d{2})+(\d{3})([0-9]|X)$/";
    @preg_match($regx, $id, $arr_split);
    $dtm_birth = $arr_split[2] . '/' . $arr_split[3] . '/' . $arr_split[4];
    if (!strtotime($dtm_birth)) //检查生日日期是否正确
    {
        return false;
    } else {
        //检验18位身份证的校验码是否正确。
        //校验位按照ISO 7064:1983.MOD 11-2的规定生成，X可以认为是数字10。
        $arr_int = array(7, 9, 10, 5, 8, 4, 2, 1, 6, 3, 7, 9, 10, 5, 8, 4, 2);
        $arr_ch = array('1', '0', 'X', '9', '8', '7', '6', '5', '4', '3', '2');
        $sign = 0;
        for ($i = 0; $i < 17; $i++) {
            $b = (int) $id[$i];
            $w = $arr_int[$i];
            $sign += $b * $w;
        }
        $n = $sign % 11;
        $val_num = $arr_ch[$n];
        if ($val_num != substr($id, 17, 1)) {
            return false;
        } else {
            return true;
        }
    }
}

function checkRefererHost()
{
    if (!Request::header('referer')) {
        return false;
    }
    $url_arr = parse_url(Request::header('referer'));
    $http_host = Request::header('host');
    if (strpos($http_host, ':')) {
        $http_host = substr($http_host, 0, strpos($http_host, ':'));
    }
    return $url_arr['host'] === $http_host;
}

function randFloat($min = 0, $max = 1)
{
    return $min + mt_rand() / mt_getrandmax() * ($max - $min);
}

function check_cert($idcard, $name, $phone)
{
    $appcode = config_get('cert_appcode');
    $url = 'https://mobile3elements.shumaidata.com/mobile/verify_real_name';
    $post = ['idcard' => $idcard, 'mobile' => $phone, 'name' => $name];
    $data = get_curl($url, http_build_query($post), 0, 0, 0, 0, 0, ['Authorization: APPCODE ' . $appcode]);
    $arr = json_decode($data, true);
    if (isset($arr['code']) && $arr['code'] == 0) {
        if ($arr['result']['res'] == '1') {
            return ['code' => 0, 'msg' => $arr['result']['description']];
        } else {
            return ['code' => -1, 'msg' => $arr['result']['description']];
        }
    } elseif (isset($arr['msg'])) {
        return ['code' => -1, 'msg' => $arr['msg']];
    } else {
        return ['code' => -2, 'msg' => '返回结果解析失败'];
    }
}
function check_corp_cert($companyName, $creditNo, $legalPerson)
{
    $results = \app\lib\AddonManager::executeHook('check_corp_cert', [
        'companyName' => $companyName,
        'creditNo' => $creditNo,
        'legalPerson' => $legalPerson,
    ]);
    foreach ($results as $result) {
        if (is_array($result) && isset($result['code'])) {
            return $result;
        }
    }

    $appcode = config_get('cert_appcode2');
    $url = 'https://companythree.shumaidata.com/companythree/check';
    $post = ['companyName' => $companyName, 'creditNo' => $creditNo, 'legalPerson' => $legalPerson];
    $data = get_curl($url . '?' . http_build_query($post), 0, 0, 0, 0, 0, 0, ['Authorization: APPCODE ' . $appcode]);
    $arr = json_decode($data, true);
    if (isset($arr['code']) && $arr['code'] == 200) {
        if ($arr['data']['result'] == 0) {
            return ['code' => 0, 'msg' => $arr['data']['desc']];
        } else {
            return ['code' => -1, 'msg' => $arr['data']['desc'] == '不一致' ? '公司与法人信息不一致' : $arr['data']['desc']];
        }
    } elseif (isset($arr['msg'])) {
        return ['code' => -1, 'msg' => $arr['msg']];
    } else {
        return ['code' => -2, 'msg' => '返回结果解析失败'];
    }
}
function show_cert_method($certmethod)
{
    if ($certmethod == 1) {
        return '微信快捷认证';
    } elseif ($certmethod == 2) {
        return '手机号三要素认证';
    } elseif ($certmethod == 3) {
        return '人工审核认证';
    } else {
        return '支付宝快捷认证';
    }
}

function get_cert_scan_type(): string
{
    $open = (int) config_get('cert_open');
    if ($open === 2) {
        return 'phone';
    }
    if ($open === 4) {
        return 'wechat';
    }
    if ($open === 6) {
        $results = \app\lib\AddonManager::executeHook('certificate_scan_type', []);
        foreach ($results as $result) {
            if (is_string($result) && in_array($result, ['alipay', 'wechat', 'phone'], true)) {
                return $result;
            }
        }
    }
    return 'alipay';
}

function checkDomain($domain)
{
    if (empty($domain) || !preg_match('/^[-$a-z0-9:_*.]{2,512}$/i', $domain) || (stripos($domain, '.') === false) || substr($domain, -1) == '.' || substr($domain, 0, 1) == '.' || substr($domain, 0, 1) == '*' && substr($domain, 1, 1) != '.' || substr_count($domain, '*') > 1 || strpos($domain, '*') > 0 || strlen($domain) < 4) return false;
    return true;
}

//微信合单支付，返回所有子单金额
function combinepay_submoneys($money)
{
    if (!config_get('wxcombine_open') || !config_get('wxcombine_minmoney')) return false;
    if ($money >= intval(config_get('wxcombine_minmoney') * 100)) {
        $subnum = 3;
        $submoney = intval($money / $subnum);
        $maxsub = intval(config_get('wxcombine_submoney') * 100);
        while ($submoney > $maxsub) {
            $subnum++;
            $submoney = intval($money / $subnum);
            if ($subnum == 50) break;
        }
        $submoneys = [];
        if (config_get('wxcombine_randmoney') == 1) {
            $avg = $money / $subnum;
            $range = max(1, intval($avg * 0.20));
            $lo = max(1, intval($avg) - $range);
            $hi = min($maxsub, intval($avg) + $range);
            $remaining = $money;
            for ($i = 0; $i < $subnum; $i++) {
                if ($i == $subnum - 1) {
                    $submoneys[] = $remaining;
                } else {
                    $leftCount = $subnum - $i;
                    $minAmount = max($lo, $remaining - ($leftCount - 1) * $hi);
                    $maxAmount = min($hi, $remaining - ($leftCount - 1) * $lo);
                    if ($minAmount > $maxAmount) $minAmount = $maxAmount;
                    $amount = mt_rand($minAmount, $maxAmount);
                    $submoneys[] = $amount;
                    $remaining -= $amount;
                }
            }
            shuffle($submoneys);
        } else {
            for ($i = 0; $i < $subnum; $i++) {
                $submoneys[] = $submoney;
            }
            $mod = $money % $subnum;
            if ($mod > 0) {
                for ($i = 0; $i < $mod; $i++) {
                    $submoneys[$i] += 1;
                }
            }
        }
        return $submoneys;
    }
    return false;
}

function get_invite_code($uid)
{
    $str = (string)$uid;
    $tmp = '';
    for ($i = 0; $i < strlen($str); $i++) {
        $tmp .= substr($str, $i, 1) ^ substr(SYS_KEY, $i, 1);
    }
    return str_replace('=', '', base64_encode($tmp));
}

function get_invite_uid($code)
{
    $str = base64_decode($code);
    $tmp = '';
    for ($i = 0; $i < strlen($str); $i++) {
        $tmp .= substr($str, $i, 1) ^ substr(SYS_KEY, $i, 1);
    }
    return $tmp;
}

function currency_convert($from, $to, $amount)
{
    $param = [
        'from' => $from,
        'to' => $to,
        'amount' => $amount
    ];
    $url = 'https://api.exchangerate.host/convert?' . http_build_query($param);
    $data = get_curl($url);
    if ($data !== false) {
        $arr = json_decode($data, true);
        if (isset($arr['success']) && $arr['success'] === true) {
            return $arr['result'];
        }
    }
    throw new Exception('汇率转换失败');
}

//极验3.0服务端验证（支持 Request 参数，兼容 JSON 与 form）
function verify_captcha($user_id = 'public')
{
    $req = request();
    $GtSdk = new \app\lib\GeetestLib(config_get('captcha_id'), config_get('captcha_key'));
    if (config_get('captcha_version') == '1') {
        return $GtSdk->gt4_validate($req->param('captcha_id'), $req->param('lot_number'), $req->param('pass_token'), $req->param('gen_time'), $req->param('captcha_output'));
    } else {
        $data = array(
            'user_id' => $user_id,
            'client_type' => "web",
            'ip_address' => $req->clientip
        );
        if (session('gtserver') == 1) {
            return $GtSdk->success_validate($req->param('geetest_challenge'), $req->param('geetest_validate'), $req->param('geetest_seccode'), $data);
        } else {
            return $GtSdk->fail_validate($req->param('geetest_challenge'), $req->param('geetest_validate'), $req->param('geetest_seccode'));
        }
    }
}

//极验4.0服务端验证
function verify_captcha4()
{
    if (!isset($_POST['captcha_id']) || !isset($_POST['lot_number']) || !isset($_POST['pass_token']) || !isset($_POST['gen_time']) || !isset($_POST['captcha_output'])) return false;
    $real_ip = real_ip();
    $url = 'http://gt4.geetest.com/demov4/demo/login';
    $param = ['captcha_id' => $_POST['captcha_id'], 'lot_number' => $_POST['lot_number'], 'pass_token' => $_POST['pass_token'], 'gen_time' => $_POST['gen_time'], 'captcha_output' => $_POST['captcha_output']];
    $referer = 'http://gt4.geetest.com/demov4/invisible-bind-zh.html';
    $httpheader[] = "X-Real-IP: " . $real_ip;
    $httpheader[] = "X-Forwarded-For: " . $real_ip;
    $data = get_curl($url . '?' . http_build_query($param), 0, $referer, 0, 0, 0, 0, $httpheader);
    if ($data !== false) {
        $arr = json_decode($data, true);
        if (isset($arr['result']) && $arr['result'] == 'success') {
            return true;
        }
    }
    return false;
}

function getGroupConfig($gid)
{
    $input_key = ['settle_open', 'settle_type', 'settle_transfer', 'user_transfer'];
    $grouprow = Db::name('group')->where('gid', $gid)->field('config')->find();
    if (!$grouprow) $grouprow = Db::name('group')->where('gid', 0)->field('config')->find();
    $config = [];
    if (!$grouprow) return $config;
    if ($grouprow['config']) {
        $arr = json_decode($grouprow['config'], true);
        foreach ($arr as $key => $value) {
            if (!isNullOrEmpty($value)) {
                if (in_array($key, $input_key) && $value == '0') continue;
                if ($key == 'settle_type') $value = $value - 1;
                $config[$key] = $value;
            }
        }
    }
    return $config;
}

function mergeGroupConfig($gid)
{
    $groupconfig = getGroupConfig($gid) ?? [];
    Config::set($groupconfig, 'sys');
}

function wxminipay_jump_scheme($wid, $order)
{
    $path = 'pages/pay/pay';
    if (config_get('wxminipay_path')) {
        $path = config_get('wxminipay_path');
    }
    $jump_url = request()->siteurl . 'pay/wxminipay/' . $order['trade_no'] . '/';
    $query = 'money=' . $order['realmoney'] . '&url=' . $jump_url;
    if (config_get('wxminipay_jump_type') == '1') {
        $appid = Db::name('weixin')->where('id', $wid)->value('appid');
        return 'weixin://dl/business/?appid=' . $appid . '&path=' . $path . '&query=' . urlencode($query) . '&env_version=release';
    } else {
        $wechat = new \app\lib\wechat\WechatAPI($wid);
        return $wechat->generate_scheme($path, $query);
    }
}

function wxminipay_jump_path($order)
{
    $path = 'pages/pay/pay';
    if (config_get('wxminipay_path')) {
        $path = config_get('wxminipay_path');
    }
    $jump_url = request()->siteurl . 'pay/wxminipay/' . $order['trade_no'] . '/';
    $extraData = ['money' => $order['realmoney'], 'url' => $jump_url];
    return $path . '?' . http_build_query($extraData);
}

function wechat_oauth($wxinfo)
{
    $oauth = new \app\lib\wechat\WechatOauth($wxinfo['appid'], $wxinfo['appsecret']);
    $code = request()->get('code');
    if ($code) {
        $cacheKey = 'wechat_oauth_openid_' . $code;
        $openid = cache($cacheKey);
        if (!empty($openid)) return $openid;
        $openid = $oauth->GetOpenidFromMp($code);
        cache($cacheKey, $openid, 300);
        return $openid;
    } else {
        if (!empty(config_get('wx_open_url'))) {
            $oauth->setOpenUrl(config_get('wx_open_url'));
        }
        Session::save();
        $oauth->login();
    }
}

function wechat_applet_oauth($trade_no, $code, $wxinfo)
{
    $oauth = new \app\lib\wechat\WechatOauth($wxinfo['appid'], $wxinfo['appsecret']);
    $openid = $oauth->AppGetOpenid($code);
    $phone_code = request()->get('phone_code');
    if (!empty($phone_code)) {
        $wechat = new \app\lib\wechat\WechatAPI($wxinfo['id']);
        $phone_info = $wechat->getPhoneNumber($phone_code);
        if ($phone_info && isset($phone_info['phoneNumber']) && !empty($trade_no)) {
            Db::name('order')->where('trade_no', $trade_no)->update(['buyer' => $openid, 'mobile' => $phone_info['phoneNumber']]);
            $black = Db::name('blacklist')->where(['type' => 0, 'content' => $phone_info['phoneNumber']])->find();
            if ($black) {
                throw new Exception(config_get('blackmsg', '系统异常无法完成付款'));
            }
        }
    }
    return $openid;
}

function alipay_oauth($trade_no = null, $alipay_config = null)
{
    if ($alipay_config && config_get('alipay_web_login_all') == 1 && config_get('alipay_web_login') > 0 || !$alipay_config && config_get('alipay_web_login') > 0) {
        $channel = \app\lib\Channel::get(config_get('alipay_web_login'));
        if ($channel) {
            $alipay_config = \app\common\PaymentConfig::getAlipayConfig($channel);
        }
    }
    if (!$alipay_config) throw new Exception('未配置支付宝网页快捷登录通道');
    try {
        $oauth = new \Alipay\AlipayOauthService($alipay_config);
        $auth_code = request()->get('auth_code');
        if ($auth_code) {
            $result = $oauth->getToken($auth_code);
            if (!empty($result['user_id'])) {
                $user_id = $result['user_id'];
                $user_type = 'userid';
            } else {
                $user_id = $result['open_id'];
                $user_type = 'openid';
            }
            if (config_get('alipay_getmobile') == 1 && $trade_no) {
                $userinfo = $oauth->userinfo($result['access_token']);
                if (isset($userinfo['mobile'])) {
                    Db::name('order')->where('trade_no', $trade_no)->update(['buyer' => $user_id, 'mobile' => $userinfo['mobile']]);
                    $black = Db::name('blacklist')->where(['type' => 0, 'content' => $userinfo['mobile']])->find();
                    if ($black) {
                        throw new Exception(config_get('blackmsg', '系统异常无法完成付款'));
                    }
                }
            }
            return [$user_type, $user_id];
        } else {
            $redirect_uri = (is_https() ? 'https://' : 'http://') . request()->host() . request()->url();
            Session::save();
            if (config_get('alipay_getmobile') == 1 && $trade_no) {
                $oauth->oauth($redirect_uri, null, 'auth_user');
            } else {
                $oauth->oauth($redirect_uri);
            }
        }
    } catch (\Throwable $e) {
        exception_log($e, 'alipay_oauth');
        throw new Exception('支付宝快捷登录失败！' . $e->getMessage());
    }
}

function alipay_mini_oauth($trade_no, $auth_code, $alipay_config = null)
{
    if (config_get('alipay_mini_login') > 0) {
        $channel = \app\lib\Channel::get(config_get('alipay_mini_login'));
        if ($channel) {
            $alipay_config = \app\common\PaymentConfig::getAlipayConfig($channel);
        }
    }
    if (!$alipay_config) throw new Exception('未配置支付宝小程序快捷登录通道');
    try {
        $oauth = new \Alipay\AlipayOauthService($alipay_config);
        $result = $oauth->getToken($auth_code);
        if (!empty($result['user_id'])) {
            $user_id = $result['user_id'];
            $user_type = 'userid';
        } else {
            $user_id = $result['open_id'];
            $user_type = 'openid';
        }
        $phone_data = request()->get('phone_data');
        if (!empty($phone_data) && !empty(config_get('alipay_aes_key')) && !empty($trade_no)) {
            $phone_data = json_decode($phone_data, true);
            if ($phone_data) {
                $mobile = $oauth->decryptMobile($phone_data, config_get('alipay_aes_key'));
                Db::name('order')->where('trade_no', $trade_no)->update(['buyer' => $user_id, 'mobile' => $mobile]);
                $black = Db::name('blacklist')->where(['type' => 0, 'content' => $mobile])->find();
                if ($black) {
                    throw new Exception(config_get('blackmsg', '系统异常无法完成付款'));
                }
            }
        }
        return [$alipay_config['app_id'], $user_type, $user_id];
    } catch (\Throwable $e) {
        exception_log($e, 'alipay_mini_oauth');
        throw new Exception('支付宝快捷登录失败！' . $e->getMessage());
    }
}

function alipaymini_jump_scheme($order, $appid = null)
{
    if (config_get('alipay_mini_login') > 0) {
        $channel = \app\lib\Channel::get(config_get('alipay_mini_login'));
        if ($channel) {
            $appid = $channel['appid'];
        }
    }
    $jump_url = request()->siteurl . 'pay/alipaymini/' . $order['trade_no'] . '/';
    $path = 'pages/pay/pay';
    if (config_get('alipaymini_path')) {
        $path = config_get('alipaymini_path');
    }
    $param = ['money' => $order['realmoney'], 'url' => $jump_url];
    $page = $path . '?' . http_build_query($param);
    $scheme_url = 'alipays://platformapi/startapp?appId=' . $appid . '&page=' . urlencode($page);
    return $scheme_url;
}

function qqpay_oauth($wxinfo)
{
    $callback = request()->siteurl . 'user/connect';
    $QC_config = ['appid' => $wxinfo['appid'], 'appkey' => $wxinfo['appsecret'], 'callback' => $callback];
    $QC = new \app\lib\QQConnect($QC_config);
    $code = request()->get('code');
    if ($code) {
        $cacheKey = 'qqpay_oauth_openid_' . $code;
        $openid = cache($cacheKey);
        if (!empty($openid)) return $openid;
        [$access_token, $openid] = $QC->callback($code);
        cache($cacheKey, $openid, 300);
        return $openid;
    } else {
        $state = 'qqpay_' . base64_encode(request()->url());
        Session::save();
        $login_url = $QC->login($state);
        header('Location: ' . $login_url);
        exit;
    }
}

function getBankCardInfo($cardno)
{
    $url = 'https://ccdcapi.alipay.com/validateAndCacheCardInfo.json?_input_charset=utf-8&cardNo=' . $cardno . '&cardBinCheck=true';
    $data = get_curl($url);
    $arr = json_decode($data, true);
    if ($arr['validated']) {
        $bank_arr = json_decode(file_get_contents(app()->getBasePath() . 'data/bankname.json'), true);
        return ['card_no' => $cardno, 'bank_code' => $arr['bank'], 'bank_name' => $bank_arr[$arr['bank']] ?? $arr['bank'], 'card_type' => $arr['cardType']];
    } else {
        throw new Exception('银行卡号校验失败');
    }
}

function parseSql(string $sql): array
{
    $statements = [];
    $current = '';
    foreach (explode("\n", $sql) as $line) {
        $trimmed = trim($line);
        if ($trimmed === '' || str_starts_with($trimmed, '--')) {
            continue;
        }
        $current .= $line . "\n";
        if (str_ends_with($trimmed, ';')) {
            $statements[] = trim($current);
            $current = '';
        }
    }
    if (trim($current) !== '') {
        $statements[] = trim($current);
    }
    return $statements;
}

function generate_key_pair()
{
    $config = [
        "private_key_bits" => 2048,
        'private_key_type' => OPENSSL_KEYTYPE_RSA,
    ];
    $res = openssl_pkey_new($config);
    if (!$res) {
        touch(app()->getRuntimePath() . 'openssl.cnf');
        $config = [
            "config" => app()->getRuntimePath() . 'openssl.cnf',
            "private_key_bits" => 2048,
            'private_key_type' => OPENSSL_KEYTYPE_RSA,
        ];
        $res = openssl_pkey_new($config);
        if (!$res) return null;
    }
    $privateKey = '';
    openssl_pkey_export($res, $privateKey, null, $config);
    $pubKey = openssl_pkey_get_details($res);
    return ['public_key' => pemToBase64($pubKey["key"]), 'private_key' => pemToBase64($privateKey)];
}

function pemToBase64($data)
{
    $line = explode("\n", $data);
    $base64 = '';
    foreach ($line as $row) {
        if (empty($row) || str_contains($row, '-----BEGIN') || str_contains($row, '-----END')) continue;
        $base64 .= trim($row);
    }
    return $base64;
}

function base64ToPem($data, $type)
{
    if (empty($data) || str_contains($data, '-----BEGIN')) return $data;
    $pem = "-----BEGIN " . $type . "-----\n" .
        wordwrap($data, 64, "\n", true) .
        "\n-----END " . $type . "-----";
    return $pem;
}

function json_response($code = 200, $msg = 'success', $data = null)
{
    $response = [
        'code' => $code,
        'msg' => $msg
    ];
    if ($data !== null) {
        $response['data'] = $data;
    }
    return json($response);
}

function getScanPayType($authCode)
{
    $prefix = substr($authCode, 0, 2);
    $alipay_prefix = ['25', '26', '27', '28', '29', '30'];
    $wxpay_prefix = ['10', '11', '12', '13', '14', '15'];
    $qqpay_prefix = ['91'];
    $bank_prefix = ['62'];
    $ecny_prefix = ['01'];
    if (in_array($prefix, $alipay_prefix)) {
        return 'alipay';
    } elseif (in_array($prefix, $wxpay_prefix)) {
        return 'wxpay';
    } elseif (in_array($prefix, $qqpay_prefix)) {
        return 'qqpay';
    } elseif (in_array($prefix, $bank_prefix)) {
        return 'bank';
    } elseif (in_array($prefix, $ecny_prefix)) {
        return 'ecny';
    } else {
        return 'unknown';
    }
}

function get_server_ip()
{
    $ip = get_curl('https://api.bt.cn/Api/getIpAddress');
    return $ip;
}

function showstar($num)
{
    $data = '';
    for ($i = 0; $i < $num; $i++) {
        $data .= '*';
    }
    return $data;
}

function is_url($url)
{
    if (preg_match('/^(http|https):\/\/[^\s]+/', $url)) {
        return true;
    } else {
        return false;
    }
}

function is_self_url($url)
{
    return strpos($url, request()->siteurl) === 0 || !empty(config_get('localurl_alipay')) && strpos($url, config_get('localurl_alipay')) === 0 || !empty(config_get('localurl_wxpay')) && strpos($url, config_get('localurl_wxpay')) === 0;
}

function get_cdn_public()
{
    if (config_get('cdnpublic') == 1) {
        return '//lib.baomitu.com/';
    } elseif (config_get('cdnpublic') == 2) {
        return 'https://s4.zstatic.net/ajax/libs/';
    } elseif (config_get('cdnpublic') == 4) {
        return 'https://cdnjs.snrat.com/ajax/libs/';
    } else {
        return '/static/libs/';
    }
}

function getCertFilePath($filename, $create = false)
{
    if (empty($filename)) return '';
    $filepath = app()->getRootPath() . 'file' . DIRECTORY_SEPARATOR . 'cert' . DIRECTORY_SEPARATOR . $filename;
    if ($create || is_file($filepath)) {
        return $filepath;
    }
    return '';
}

function exception_log(Throwable $e, ?string $action = null): void
{
    if ($e instanceof think\exception\ErrorException) {
        if (empty($action)) $action = 'ErrorException';
        Log::error(sprintf('[%s] %s in %s:%d', $action, $e->getMessage(), $e->getFile(), $e->getLine()));
    }
}

function deleteDirectory(string $dirname): void
{
    if (!is_dir($dirname)) {
        return;
    }
    $items = new \FilesystemIterator($dirname);
    foreach ($items as $item) {
        if ($item->isDir() && !$item->isLink()) {
            deleteDirectory($item->getPathname());
        } else {
            @unlink($item->getPathname());
        }
    }
    @rmdir($dirname);
}

function delete_cookie_at_path(string $name, string $path): void
{
    setcookie($name, '', [
        'expires'  => time() - 3600,
        'path'     => $path,
    ]);
}
