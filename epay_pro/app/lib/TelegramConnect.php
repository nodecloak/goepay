<?php

declare(strict_types=1);

namespace app\lib;

/**
 * Telegram OIDC 快捷登录（Authorization Code + PKCE）
 * 不使用官方 JS SDK，直接跳转 oauth.telegram.org 授权页
 */
class TelegramConnect
{
    const GET_AUTH_CODE_URL = 'https://oauth.telegram.org/auth';
    const GET_ACCESS_TOKEN_URL = 'https://oauth.telegram.org/token';
    const GET_JWKS_URL = 'https://oauth.telegram.org/.well-known/jwks.json';
    const ISSUER = 'https://oauth.telegram.org';
    const SCOPE = 'openid profile telegram:bot_access';

    private $appid;
    private $appkey;
    private $callback;

    public function __construct(array $config)
    {
        $this->appid = (string) ($config['appid'] ?? '');
        $this->appkey = (string) ($config['appkey'] ?? '');
        $this->callback = (string) ($config['callback'] ?? '');
    }

    public static function generateCodeVerifier(): string
    {
        return self::b64urlEncode(random_bytes(32));
    }

    public function login(string $state, string $codeVerifier): string
    {
        $keysArr = [
            'client_id' => $this->appid,
            'redirect_uri' => $this->callback,
            'response_type' => 'code',
            'scope' => self::SCOPE,
            'state' => $state,
            'code_challenge' => self::b64urlEncode(hash('sha256', $codeVerifier, true)),
            'code_challenge_method' => 'S256',
        ];
        return self::GET_AUTH_CODE_URL . '?' . http_build_query($keysArr);
    }

    /**
     * 用授权码换取用户 Telegram ID（profile.id，不是 openid/sub）
     */
    public function callback(string $code, string $codeVerifier): string
    {
        $post = [
            'grant_type' => 'authorization_code',
            'code' => $code,
            'redirect_uri' => $this->callback,
            'client_id' => $this->appid,
            'code_verifier' => $codeVerifier,
        ];
        $auth = base64_encode($this->appid . ':' . $this->appkey);
        $response = self::request(self::GET_ACCESS_TOKEN_URL, http_build_query($post), [
            'Authorization: Basic ' . $auth,
            'Content-Type: application/x-www-form-urlencoded',
        ]);
        $arr = json_decode($response, true);
        if (!is_array($arr)) {
            throw new \Exception('Telegram登录失败，响应解析失败');
        }
        if (!empty($arr['error'])) {
            $desc = $arr['error_description'] ?? $arr['error'];
            throw new \Exception('Telegram登录失败 [' . $arr['error'] . ']' . $desc);
        }
        if (empty($arr['id_token'])) {
            throw new \Exception('Telegram登录失败，未返回ID Token');
        }

        $payload = $this->verifyIdToken((string) $arr['id_token']);
        $userId = $payload['id'] ?? null;
        if ($userId === null || $userId === '') {
            throw new \Exception('Telegram登录失败，未获取到用户ID');
        }
        return (string) $userId;
    }

    private function verifyIdToken(string $idToken): array
    {
        $parts = explode('.', $idToken);
        if (count($parts) !== 3) {
            throw new \Exception('Telegram登录失败，ID Token格式错误');
        }
        [$headerB64, $payloadB64, $sigB64] = $parts;
        $header = json_decode(self::b64urlDecode($headerB64), true);
        $payload = json_decode(self::b64urlDecode($payloadB64), true);
        if (!is_array($header) || !is_array($payload)) {
            throw new \Exception('Telegram登录失败，ID Token解析失败');
        }

        $alg = $header['alg'] ?? '';
        if ($alg !== 'RS256') {
            throw new \Exception('Telegram登录失败，暂不支持签名算法 ' . $alg . '，请在 BotFather 中使用 RS256');
        }

        $signature = self::b64urlDecode($sigB64);
        $signed = $headerB64 . '.' . $payloadB64;
        $pem = $this->resolvePublicKey($header['kid'] ?? '');
        $ok = openssl_verify($signed, $signature, $pem, OPENSSL_ALGO_SHA256);
        if ($ok !== 1) {
            throw new \Exception('Telegram登录失败，ID Token签名校验失败');
        }
        return $payload;
    }

    private function resolvePublicKey(string $kid): string
    {
        $response = self::request(self::GET_JWKS_URL);
        $jwks = json_decode($response, true);
        $keys = $jwks['keys'] ?? [];
        if (!is_array($keys) || $keys === []) {
            throw new \Exception('Telegram登录失败，公钥列表为空');
        }
        $jwk = null;
        foreach ($keys as $key) {
            if (!is_array($key)) {
                continue;
            }
            if ($kid !== '' && ($key['kid'] ?? '') === $kid) {
                $jwk = $key;
                break;
            }
            if ($kid === '' && ($key['kty'] ?? '') === 'RSA') {
                $jwk = $key;
                break;
            }
        }
        if ($jwk === null && $kid !== '') {
            foreach ($keys as $key) {
                if (is_array($key) && ($key['kty'] ?? '') === 'RSA') {
                    $jwk = $key;
                    break;
                }
            }
        }
        if ($jwk === null || empty($jwk['n']) || empty($jwk['e'])) {
            throw new \Exception('Telegram登录失败，未找到可用公钥');
        }
        return $this->jwkToPem($jwk);
    }

    /**
     * 向已授权用户发送 Telegram 私聊通知
     */
    public static function sendMessage(string $chatId, string $title, string $content): bool
    {
        $token = trim((string) config_get('login_telegram_token'));
        if ($token === '' || $chatId === '') {
            throw new \Exception('未配置Telegram Bot Token');
        }
        $text = '<strong>' . $title . "</strong>\n" . self::toTelegramHtml($content);
        if (mb_strlen($text) > 4096) {
            $text = mb_substr($text, 0, 4096);
        }
        $url = 'https://api.telegram.org/bot' . $token . '/sendMessage';
        $response = self::request($url, http_build_query([
            'chat_id' => $chatId,
            'text' => $text,
            'parse_mode' => 'HTML',
            'disable_web_page_preview' => 'true',
        ]));
        $arr = json_decode($response, true);
        if (is_array($arr) && !empty($arr['ok'])) {
            return true;
        }
        $desc = is_array($arr) ? ($arr['description'] ?? $response) : $response;
        throw new \Exception('Telegram发送失败：' . $desc);
    }

    private static function toTelegramHtml(string $content): string
    {
        $content = str_replace(['<br/>', '<br />', '<br>'], "\n", $content);
        $content = preg_replace('/<(?!\/?(?:b|strong|i|em|u|s|code|pre|a)(?:\s|>))[^>]+>/i', '', $content) ?? $content;
        return $content;
    }

    /**
     * 请求 Telegram 接口。开启 login_telegram_proxy 时强制走中转代理里的代理服务器（不用代理API）
     */
    private static function request(string $url, ?string $post = null, array $addheader = []): string
    {
        $ch = curl_init();
        curl_setopt($ch, CURLOPT_URL, $url);
        curl_setopt($ch, CURLOPT_SSL_VERIFYPEER, false);
        curl_setopt($ch, CURLOPT_SSL_VERIFYHOST, false);
        $httpheader = [
            'Accept: */*',
            'Accept-Encoding: gzip,deflate,sdch',
            'Accept-Language: zh-CN,zh;q=0.8',
            'Connection: close',
        ];
        if ($addheader) {
            $httpheader = array_merge($httpheader, $addheader);
        }
        curl_setopt($ch, CURLOPT_HTTPHEADER, $httpheader);
        if ($post !== null) {
            curl_setopt($ch, CURLOPT_POST, 1);
            curl_setopt($ch, CURLOPT_POSTFIELDS, $post);
        }
        curl_setopt($ch, CURLOPT_USERAGENT, 'Mozilla/5.0 (Windows NT 10.0; WOW64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/78.0.3904.108 Safari/537.36');
        curl_setopt($ch, CURLOPT_TIMEOUT, 60);
        curl_setopt($ch, CURLOPT_ENCODING, 'gzip');
        curl_setopt($ch, CURLOPT_RETURNTRANSFER, 1);

        if (config_get('login_telegram_proxy') == 1) {
            self::applyProxy($ch);
        }

        $ret = curl_exec($ch);
        if ($ret === false) {
            $err = curl_error($ch);
            curl_close($ch);
            throw new \Exception('Telegram请求失败' . ($err !== '' ? '：' . $err : ''));
        }
        curl_close($ch);
        if ($ret === '' || $ret === true) {
            throw new \Exception('Telegram请求失败');
        }
        return (string) $ret;
    }

    private static function applyProxy(\CurlHandle $ch): void
    {
        $proxyServer = trim((string) config_get('proxy_server'));
        $proxyPort = (int) config_get('proxy_port');
        if ($proxyServer === '' || $proxyPort <= 0) {
            throw new \Exception('Telegram请求失败，未配置中转代理服务器');
        }
        $proxyTypeVal = config_get('proxy_type');
        if ($proxyTypeVal == 'https') {
            $proxyType = CURLPROXY_HTTPS;
        } elseif ($proxyTypeVal == 'sock4') {
            $proxyType = CURLPROXY_SOCKS4;
        } elseif ($proxyTypeVal == 'sock5') {
            $proxyType = CURLPROXY_SOCKS5;
        } elseif ($proxyTypeVal == 'sock5h') {
            $proxyType = CURLPROXY_SOCKS5_HOSTNAME;
        } else {
            $proxyType = CURLPROXY_HTTP;
        }
        curl_setopt($ch, CURLOPT_PROXYAUTH, CURLAUTH_BASIC);
        curl_setopt($ch, CURLOPT_PROXY, $proxyServer);
        curl_setopt($ch, CURLOPT_PROXYPORT, $proxyPort);
        $proxyUser = config_get('proxy_user');
        $proxyPwd = config_get('proxy_pwd');
        if (!empty($proxyUser) && !empty($proxyPwd)) {
            curl_setopt($ch, CURLOPT_PROXYUSERPWD, $proxyUser . ':' . $proxyPwd);
        }
        curl_setopt($ch, CURLOPT_PROXYTYPE, $proxyType);
    }

    private function jwkToPem(array $jwk): string
    {
        $modulus = self::b64urlDecode((string) $jwk['n']);
        $exponent = self::b64urlDecode((string) $jwk['e']);
        $rsaKey = $this->asn1Sequence($this->asn1Integer($modulus) . $this->asn1Integer($exponent));
        $bitString = "\x03" . $this->asn1Length(strlen($rsaKey) + 1) . "\x00" . $rsaKey;
        $algorithmId = hex2bin('300d06092a864886f70d0101010500');
        $der = $this->asn1Sequence($algorithmId . $bitString);
        return "-----BEGIN PUBLIC KEY-----\n" . chunk_split(base64_encode($der), 64, "\n") . "-----END PUBLIC KEY-----";
    }

    private function asn1Integer(string $bytes): string
    {
        $bytes = ltrim($bytes, "\x00");
        if ($bytes === '' || (ord($bytes[0]) & 0x80)) {
            $bytes = "\x00" . $bytes;
        }
        return "\x02" . $this->asn1Length(strlen($bytes)) . $bytes;
    }

    private function asn1Sequence(string $contents): string
    {
        return "\x30" . $this->asn1Length(strlen($contents)) . $contents;
    }

    private function asn1Length(int $length): string
    {
        if ($length <= 0x7F) {
            return chr($length);
        }
        $temp = ltrim(pack('N', $length), "\x00");
        return chr(0x80 | strlen($temp)) . $temp;
    }

    private static function b64urlEncode(string $data): string
    {
        return rtrim(strtr(base64_encode($data), '+/', '-_'), '=');
    }

    private static function b64urlDecode(string $data): string
    {
        $remainder = strlen($data) % 4;
        if ($remainder > 0) {
            $data .= str_repeat('=', 4 - $remainder);
        }
        $decoded = base64_decode(strtr($data, '-_', '+/'), true);
        if ($decoded === false) {
            throw new \Exception('Telegram登录失败，数据解码失败');
        }
        return $decoded;
    }
}
