<?php

declare(strict_types=1);

namespace app\async;

use app\common\AsyncTaskInterface;
use app\logic\AsyncTaskRunner;
use InvalidArgumentException;
use RuntimeException;
use think\facade\Cache;
use think\facade\Db;
use think\facade\Log;

/**
 * 商户事件消息异步发送
 */
class SendEvent implements AsyncTaskInterface
{
    public const EVENT_COMPLAIN = 'complain';
    public const EVENT_REFUND = 'refund';

    private const FAIL_LIMIT = 10;
    private const FAIL_CACHE_PREFIX = 'event_message_fail_';

    public static function identifier(): string
    {
        return 'send_event';
    }

    public static function dispatch(int $uid, string $event, array $content): bool
    {
        $payload = [
            'uid' => $uid,
            'event' => $event,
            'content' => $content,
        ];
        if (AsyncTaskRunner::tryDispatch(self::identifier(), $payload)) {
            return true;
        }

        (new self())->handle($payload);
        return true;
    }

    public static function testUrl(string $url): void
    {
        $instance = new self();
        if (config_get('proxy', '0') === '2' && !empty(config_get('proxy_apiurl')) && !empty(config_get('proxy_apikey'))) {
            [$statusCode] = $instance->requestByProxyApi($url);
        } else {
            [$statusCode] = $instance->requestByCurl($url);
        }
        if ($statusCode === 0) {
            throw new RuntimeException('未获取到有效HTTP状态码');
        }
    }

    public function handle(array $payload): void
    {
        $uid = (int)($payload['uid'] ?? 0);
        $event = (string)($payload['event'] ?? '');
        $content = $payload['content'] ?? null;
        if ($uid <= 0 || !in_array($event, [self::EVENT_COMPLAIN, self::EVENT_REFUND], true) || !is_array($content)) {
            throw new InvalidArgumentException('事件消息任务参数无效');
        }

        $user = Db::name('user')->where('uid', $uid)->field('uid,email,event_url,event_types')->find();
        if (!$user || empty($user['event_url'])) {
            return;
        }
        $events = array_filter(explode(',', (string)($user['event_types'] ?? '')));
        if (!in_array($event, $events, true)) {
            return;
        }

        try {
            $this->send((string)$user['event_url'], $uid, $event, $content);
            Cache::delete(self::FAIL_CACHE_PREFIX . $uid);
        } catch (\Throwable $e) {
            $this->handleFailure($user, $e->getMessage());
        }
    }

    private function send(string $url, int $uid, string $event, array $content): void
    {
        $json = json_encode(
            $content,
            JSON_UNESCAPED_UNICODE | JSON_UNESCAPED_SLASHES | JSON_INVALID_UTF8_SUBSTITUTE
        );
        if ($json === false) {
            throw new RuntimeException('事件内容编码失败');
        }

        $params = [
            'event' => $event,
            'content' => $json,
            'pid' => (string)$uid,
            'timestamp' => (string)time(),
            'sign_type' => 'RSA',
        ];
        $params['sign'] = \app\lib\Payment::makeSign($params, null);
        if (empty($params['sign'])) {
            throw new RuntimeException('事件消息签名失败');
        }

        $postData = http_build_query($params);
        if ((string)config_get('proxy', '0') === '2') {
            [$statusCode, $response] = $this->requestByProxyApi($url, $postData);
        } else {
            [$statusCode, $response] = $this->requestByCurl($url, $postData);
        }

        if ($statusCode < 200 || $statusCode >= 300) {
            throw new RuntimeException('请求失败：HTTP ' . $statusCode);
        }
        if (strpos($response, 'success') === false && strpos($response, 'SUCCESS') === false && strpos($response, 'Success') === false) {
            throw new RuntimeException('接收方未返回 success');
        }
    }

    /**
     * @return array{0: int, 1: string}
     */
    private function requestByProxyApi(string $url, ?string $postData = null): array
    {
        $encodedUrl = base64_encode($url);
        $encodedPost = $postData !== null ? base64_encode($postData) : null;
        $timestamp = (string)time();
        $signSource = $encodedUrl . ($encodedPost ?? '') . $timestamp . config_get('proxy_apikey');
        $params = [
            'url' => $encodedUrl,
            'timestamp' => $timestamp,
            'sign' => md5($signSource),
            'new' => '1',
        ];
        if ($encodedPost !== null) {
            $params['post'] = $encodedPost;
        }

        $data = get_curl(config_get('proxy_apiurl'), http_build_query($params));
        $result = json_decode((string)$data, true);
        if (!is_array($result) || !isset($result['code'])) {
            throw new RuntimeException('代理API响应无效');
        }
        if ($result['code'] !== 0 || !array_key_exists('content', $result)) {
            throw new RuntimeException('代理API请求失败：' . ($result['msg'] ?? '未知错误'));
        }
        return [
            (int)($result['http_status_code'] ?? 0),
            (string)$result['content'],
        ];
    }

    /**
     * @return array{0: int, 1: string}
     */
    private function requestByCurl(string $url, ?string $postData = null): array
    {
        $ch = curl_init($url);
        $headers = [
            'Accept: */*',
            'Accept-Language: zh-CN,zh;q=0.8',
            'Connection: close',
        ];
        curl_setopt_array($ch, [
            CURLOPT_HTTPHEADER => $headers,
            CURLOPT_RETURNTRANSFER => true,
            CURLOPT_CONNECTTIMEOUT => 3,
            CURLOPT_TIMEOUT => 6,
            CURLOPT_SSL_VERIFYPEER => false,
            CURLOPT_SSL_VERIFYHOST => false,
        ]);
        if ($postData !== null) {
            curl_setopt($ch, CURLOPT_POST, true);
            curl_setopt($ch, CURLOPT_POSTFIELDS, $postData);
        }
        if (config_get('proxy', '0') === '1') {
            $this->applyProxyServer($ch);
        }

        $response = curl_exec($ch);
        $error = curl_error($ch);
        $statusCode = (int)curl_getinfo($ch, CURLINFO_HTTP_CODE);
        curl_close($ch);

        if ($response === false) {
            throw new RuntimeException('请求失败：' . ($error ?: '未知网络错误'));
        }
        return [$statusCode, (string)$response];
    }

    private function applyProxyServer(\CurlHandle $ch): void
    {
        $server = trim(config_get('proxy_server'));
        $port = (int)config_get('proxy_port');
        if ($server === '' || $port <= 0) {
            throw new RuntimeException('代理服务器配置不完整');
        }

        $type = match ((string)config_get('proxy_type')) {
            'https' => CURLPROXY_HTTPS,
            'sock4' => CURLPROXY_SOCKS4,
            'sock5' => CURLPROXY_SOCKS5,
            'sock5h' => CURLPROXY_SOCKS5_HOSTNAME,
            default => CURLPROXY_HTTP,
        };
        curl_setopt($ch, CURLOPT_PROXYAUTH, CURLAUTH_BASIC);
        curl_setopt($ch, CURLOPT_PROXY, $server);
        curl_setopt($ch, CURLOPT_PROXYPORT, $port);
        $user = config_get('proxy_user');
        $password = config_get('proxy_pwd');
        if (!empty($user) && !empty($password)) {
            curl_setopt($ch, CURLOPT_PROXYUSERPWD, $user . ':' . $password);
        }
        curl_setopt($ch, CURLOPT_PROXYTYPE, $type);
    }

    private function handleFailure(array $user, string $error): void
    {
        $uid = (int)$user['uid'];
        $cacheKey = self::FAIL_CACHE_PREFIX . $uid;
        $failures = (int)Cache::get($cacheKey, 0) + 1;
        Cache::set($cacheKey, $failures, 0);
        Log::warning('[商户事件消息][' . $uid . '] 第' . $failures . '次连续发送失败：' . $error);
        if ($failures < self::FAIL_LIMIT) {
            return;
        }

        $updated = Db::name('user')
            ->where('uid', $uid)
            ->where('event_types', '<>', '')
            ->update(['event_types' => '']);
        Cache::delete($cacheKey);
        if (!$updated || empty($user['email'])) {
            return;
        }

        $siteName = (string)config_get('sitename');
        send_mail(
            $user['email'],
            $siteName . ' - 事件消息订阅已关闭',
            '尊敬的商户：您的事件消息订阅地址已连续请求失败' . self::FAIL_LIMIT
            . '次，系统已自动取消全部事件订阅。请检查订阅地址后在商户后台重新开启。<br/>'
            . '<b>订阅地址：</b>' . htmlspecialchars((string)$user['event_url'], ENT_QUOTES, 'UTF-8')
            . '<br/><b>最近错误：</b>' . htmlspecialchars($error, ENT_QUOTES, 'UTF-8')
            . '<br/>----------<br/>' . $siteName . '<br/>' . date('Y-m-d H:i:s')
        );
    }
}
