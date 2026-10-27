<?php

declare(strict_types=1);

namespace app\controller\payment;

use app\BaseController;
use app\lib\QqBot as QqBotApi;
use think\facade\Cache;
use think\facade\Db;
use think\facade\Log;

/**
 * QQ机器人Webhook回调
 */
class Qqbot extends BaseController
{
    public function webhook()
    {
        $uidParam = $this->request->param('uid');
        $isAdmin = $uidParam === null || $uidParam === '';
        $uid = (int) $uidParam;
        $body = $this->request->getContent();
        if ((!$isAdmin && $uid <= 0) || $body === '') {
            return response('bad request', 400);
        }

        if ($isAdmin) {
            $msgconfig = [
                'qqbot_appid' => trim((string) config_get('qqbot_appid')),
                'qqbot_appsecret' => trim((string) config_get('qqbot_appsecret')),
                'qqbot_openid' => trim((string) config_get('qqbot_openid')),
            ];
        } else {
            $userrow = Db::name('user')->where('uid', $uid)->field('uid,msgconfig')->find();
            if (!$userrow) {
                return response('user not found', 404);
            }
            $msgconfig = $this->decodeMsgConfig($userrow['msgconfig'] ?? '');
        }

        $appId = trim($msgconfig['qqbot_appid'] ?? '');
        $appSecret = trim($msgconfig['qqbot_appsecret'] ?? '');
        if ($appId === '' || $appSecret === '') {
            return response('not configured', 400);
        }

        $headerAppId = $this->request->header('x-bot-appid', '');
        if ($headerAppId !== '' && $headerAppId !== $appId) {
            return response('appid mismatch', 403);
        }

        $payload = json_decode($body, true);
        if (!is_array($payload)) {
            return response('invalid body', 400);
        }

        try {
            $bot = new QqBotApi($appId, $appSecret);
            $op = (int) ($payload['op'] ?? 0);
            if ($op === 13) {
                $data = is_array($payload['d'] ?? null) ? $payload['d'] : [];
                return json($bot->signValidation((string) ($data['event_ts'] ?? ''), (string) ($data['plain_token'] ?? '')));
            }

            $timestamp = $this->request->header('x-signature-timestamp', '');
            $signature = $this->request->header('x-signature-ed25519', '');
            if (!$bot->verifySignature($timestamp, $signature, $body)) {
                return response('invalid signature', 401);
            }

            if (strtoupper((string) ($payload['t'] ?? '')) === 'C2C_MESSAGE_CREATE') {
                $event = is_array($payload['d'] ?? null) ? $payload['d'] : [];
                $openid = $event['author']['user_openid'] ?? '';
                if ($openid !== '' && ($msgconfig['qqbot_openid'] ?? '') !== $openid) {
                    $msgconfig['qqbot_openid'] = $openid;
                    if ($isAdmin) {
                        config_set('qqbot_openid', $openid);
                        Cache::delete('configs');
                    } else {
                        Db::name('user')->where('uid', $uid)->update(['msgconfig' => serialize($msgconfig)]);
                    }
                    try {
                        $bot->sendMarkdown(
                            $openid,
                            "# 绑定成功\n\n您已成功绑定"
                            . ($isAdmin ? '管理员' : '商户')
                            . "QQ机器人消息通知，"
                            . config_get('sitename')
                            . "平台通知将通过本机器人发送。",
                            (string) ($event['id'] ?? '')
                        );
                    } catch (\Throwable $e) {
                        Log::error('[qqbotWebhook] bind reply failed: ' . $e->getMessage());
                    }
                }
            }
            return response('', 200);
        } catch (\Throwable $e) {
            exception_log($e, 'QQ机器人Webhook');
            return response('error', 500);
        }
    }

    private function decodeMsgConfig(?string $raw): array
    {
        $msgconfig = @unserialize($raw ?? '');
        return is_array($msgconfig) ? $msgconfig : [];
    }
}
