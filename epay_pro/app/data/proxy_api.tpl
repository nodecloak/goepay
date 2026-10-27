<?php

define('API_KEY', '{apikey}'); //此处填写API密钥


if (function_exists("ignore_user_abort")) @ignore_user_abort(true);

$url = isset($_POST['url']) ? $_POST['url'] : exit('{"code":-1,"msg":"No url"}');
$sign = isset($_POST['sign']) ? $_POST['sign'] : exit('{"code":-1,"msg":"No sign"}');
$timestamp = isset($_POST['timestamp']) ? intval($_POST['timestamp']) : exit('{"code":-1,"msg":"No timestamp"}');
$new = isset($_POST['new']) ? intval($_POST['new']) : 0;
$post = isset($_POST['post']) ? $_POST['post'] : null;

if (abs(time() - $timestamp) > 300) {
    exit('{"code":-1,"msg":"时间戳异常"}');
}
if (md5($url . ($post ?? '') . $timestamp . API_KEY) !== $sign) {
    exit('{"code":-1,"msg":"签名验证失败"}');
}

$url = base64_decode($url);
if (!is_url($url)) {
    exit('{"code":-1,"msg":"URL不合法"}');
}
$post = $post !== null ? base64_decode($post, true) : null;
if ($post === false) {
    exit('{"code":-1,"msg":"POST data error"}');
}

try {
    [$httpStatusCode, $data] = curl_request($url, $post);
} catch (Exception $e) {
    if ($new == 1) {
        $errmsg = $e->getMessage();
        $errno = $e->getCode();
        echo json_encode(['code'=>-1, 'msg'=>$errmsg, 'errno'=>$errno]);
    }
    exit;
}

if (mb_detect_encoding($data, 'GBK')) {
    $data = mb_convert_encoding($data, 'UTF-8', 'GBK');
} else {
    $data = mb_convert_encoding($data, 'UTF-8', 'UTF-8');
}

if (mb_strlen($data) > 200) $data = mb_strcut($data, 0, 200);

if ($new == 1) {
    echo json_encode(['code'=>0, 'content'=>$data, 'http_status_code'=>$httpStatusCode]);
} else {
    echo $data;
}

function curl_request($url, $post = null)
{
    $ch = curl_init($url);
    $httpheader[] = "Accept: */*";
    $httpheader[] = "Accept-Language: zh-CN,zh;q=0.8";
    $httpheader[] = "Connection: close";
    if ($post !== null) {
        curl_setopt($ch, CURLOPT_POST, true);
        curl_setopt($ch, CURLOPT_POSTFIELDS, $post);
    }
    curl_setopt($ch, CURLOPT_HTTPHEADER, $httpheader);
    curl_setopt($ch, CURLOPT_SSL_VERIFYPEER, false);
    curl_setopt($ch, CURLOPT_SSL_VERIFYHOST, false);
    curl_setopt($ch, CURLOPT_RETURNTRANSFER, true);
    curl_setopt($ch, CURLOPT_USERAGENT, 'Mozilla/5.0 (Windows NT 10.0; WOW64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/78.0.3904.108 Safari/537.36');
    curl_setopt($ch, CURLOPT_TIMEOUT, 5);
    $content = curl_exec($ch);
    $errno = curl_errno($ch);
    if ($errno) {
        $errmsg = curl_error($ch);
        curl_close($ch);
        throw new Exception('CURL Error: '.$errmsg, $errno);
    }
    $httpStatusCode = curl_getinfo($ch, CURLINFO_HTTP_CODE);
    curl_close($ch);
    return [$httpStatusCode, $content];
}
function is_url($url)
{
    if (preg_match('/^(http|https):\/\/[^\s]+/', $url)) {
        return true;
    } else {
        return false;
    }
}
