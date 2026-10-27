<?php

namespace app\lib;

use Exception;
use think\facade\Db;
use think\facade\Log;

class MsgNotice
{

    public static function sendAsync($scene, $uid, $param)
    {
        if (\app\logic\AsyncTaskRunner::tryDispatch('send_msg', [
            'scene' => $scene,
            'uid' => $uid,
            'param' => $param,
        ])) {
            return true;
        }
        return self::send($scene, $uid, $param);
    }

    public static function send($scene, $uid, $param)
    {
        $scene_all = ['complain', 'mchrisk'];
        if ($uid == 0) {
            if ($scene == 'wxtransfer') {
                return self::send_wechat_tplmsg($scene, $param['account'], $param);
            }
            if (in_array($scene, $scene_all)) {
                $switch = self::getMessageSwitch($scene . '_all');
            } elseif ($scene == 'internal_transfer') {
                $switch = self::getMessageSwitch('domain');
            } else {
                $switch = self::getMessageSwitch($scene);
            }
            if ($switch == 1) {
                $receiver = config_get('mail_recv') ?: config_get('mail_name');
                return self::send_mail_msg($scene, $receiver, $param);
            } elseif ($switch == 2) {
                return self::send_robot_msg($scene, config_get('msgrobot_url'), $param, true);
            } elseif ($switch == 4) {
                return self::send_telegram_msg($scene, (string) config_get('msgtelegram_chatid'), $param);
            } elseif ($switch == 5) {
                return self::send_admin_qqbot_msg($scene, $param);
            }
        } else {
            $userrow = Db::name('user')->where('uid', $uid)->field('phone,email,wx_uid,telegram_uid,codename,msgconfig,voice_order,voice_devid,print_order,print_config')->find();
            if (!$userrow) return false;
            $msgconfig = @unserialize($userrow['msgconfig'] ?? '');
            $msgconfig = is_array($msgconfig) ? $msgconfig : [];

            $getMsgConfig = function($key) use ($msgconfig) {
                return $msgconfig[$key] ?? null;
            };

            if (config_get('voicenotice') == 1 && $scene == 'order' && $userrow['voice_order'] == 1 && $param['name'] != '付款码收款') {
                self::send_voice($userrow['voice_devid'], $param['type'], $param['money']);
            }
            if (config_get('orderprint') == 1 && $scene == 'order' && ($userrow['print_order'] == 2 || $userrow['print_order'] == 1 && $param['tid'] == 3)) {
                $param['codename'] = $userrow['codename'];
                $print_config = @unserialize($userrow['print_config'] ?? '');
                if (is_array($print_config) && !empty($print_config['devid'])) {
                    (new Printer(config_get('print_appid'), config_get('print_appsecret')))->print($print_config['devid'], $param, $print_config['count'], $print_config['voice'] == 1, true);
                }
            }

            if ($scene == 'order' && $getMsgConfig('order_money') > 0 && $param['money'] < $getMsgConfig('order_money')) return false;
            if ($scene == 'balance') $param['msgmoney'] = $getMsgConfig('balance_money');

            $mailRobotSwitchOk = $scene === 'daily_report' || self::getMessageSwitch($scene) == 1;
            if ($getMsgConfig($scene) == 1 && !empty($userrow['wx_uid'])) {
                self::send_wechat_tplmsg($scene, $userrow['wx_uid'], $param);
            } elseif ($getMsgConfig($scene) == 2 && !empty($userrow['email']) && $mailRobotSwitchOk) {
                self::send_mail_msg($scene, $userrow['email'], $param);
            } elseif ($getMsgConfig($scene) == 3 && !empty($userrow['phone'])) {
                if ($scene == 'balance') {
                    $tpl_param = ['code' => $param['msgmoney']];
                    send_sms($userrow['phone'], 'balance', $tpl_param);
                } elseif ($scene == 'complain') {
                    $tpl_param = ['code' => $param['trade_no']];
                    send_sms($userrow['phone'], 'complain', $tpl_param);
                }
            } elseif ($getMsgConfig($scene) == 4 && !empty($getMsgConfig('msgrobot_url')) && $mailRobotSwitchOk) {
                self::send_robot_msg($scene, $getMsgConfig('msgrobot_url'), $param);
            } elseif ($getMsgConfig($scene) == 5 && !empty($userrow['telegram_uid']) && config_get('telegramnotice') == 1) {
                self::send_telegram_msg($scene, (string) $userrow['telegram_uid'], $param);
            } elseif ($getMsgConfig($scene) == 6 && !empty($getMsgConfig('qqbot_openid')) && config_get('qqbotnotice') == 1) {
                self::send_qqbot_msg($scene, $msgconfig, $param);
            }
            if ($scene == 'group' && config_get('msgconfig_group') == 1 && !empty($userrow['email'])) {
                self::send_mail_msg($scene, $userrow['email'], $param);
            } elseif ($scene == 'domain_audit' && config_get('msgconfig_domain_audit') == 1 && !empty($userrow['email'])) {
                self::send_mail_msg($scene, $userrow['email'], $param);
            }
            if ($scene == 'group' && !empty($userrow['phone'])) {
                $tpl_param = ['code' => $param['group']];
                send_sms($userrow['phone'], 'group', $tpl_param);
            }

            if (in_array($scene, $scene_all)) {
                $switch = self::getMessageSwitch($scene . '_all');
                if ($switch == 1) {
                    $receiver = config_get('mail_recv') ?: config_get('mail_name');
                    self::send_mail_msg($scene, $receiver, $param);
                } elseif ($switch == 2) {
                    return self::send_robot_msg($scene, config_get('msgrobot_url'), $param, true);
                } elseif ($switch == 3 && !empty(config_get('msgrobot_phone'))) {
                    $tpl_param = ['code' => $param['trade_no']];
                    return send_sms(config_get('msgrobot_phone'), 'complain', $tpl_param);
                } elseif ($switch == 4) {
                    return self::send_telegram_msg($scene, (string) config_get('msgtelegram_chatid'), $param);
                } elseif ($switch == 5) {
                    return self::send_admin_qqbot_msg($scene, $param);
                }
            }
        }
    }

    /**
     * 管理员系统告警使用风控提醒渠道发送。
     * msgconfig_risk 兼容原有取值：0 邮件、1 群机器人、2 Telegram、3 QQ。
     */
    public static function sendAdminMessage($title, $content)
    {
        $switch = (int) config_get('msgconfig_risk', 0);
        if ($switch === 1 && !empty(config_get('msgrobot_url'))) {
            return self::robot_webhook(config_get('msgrobot_url'), $title, $content, true);
        }
        if ($switch === 2 && !empty(config_get('msgtelegram_chatid'))) {
            return self::send_telegram_content((string) config_get('msgtelegram_chatid'), $title, $content);
        }
        if ($switch === 3) {
            $config = self::get_admin_qqbot_config();
            if (!empty($config['qqbot_openid'])) {
                return self::send_qqbot_content($config, $title, $content);
            }
        }

        $receiver = config_get('mail_recv') ?: config_get('mail_name');
        return send_mail($receiver, $title, $content);
    }

    public static function sendAdminMessageAsync($title, $content)
    {
        if (\app\logic\AsyncTaskRunner::tryDispatch('send_admin_message', [
            'title' => $title,
            'content' => $content
        ])) {
            return true;
        }
        return self::sendAdminMessage($title, $content);
    }

    /**
     * https://acnlfexk4r1d.feishu.cn/wiki/SwDcwFwR4iTc5jk6BPTctAWXnDc
     */
    public static function send_voice($devid, $type, $money)
    {
        $url = 'http://iot.solomo-info.com:9306/admin/common/msgpush';
        $param = [
            'agent_id' => config_get('voice_username'),
            'agent_secret' => config_get('voice_apikey'),
            'sbx_id' => $devid,
            'msg' => $type . '收款' . $money . '元',
            'debug' => 0,
        ];
        $data = get_curl($url, json_encode($param), 0, 0, 0, 0, 0, ['Content-Type: application/json']);
        $result = json_decode($data, true);
        if (isset($result['result']) && $result['result'] == 'success') {
            return true;
        } else {
            $errmsg = '发送云喇叭消息失败，' . ($result['msg'] ?? $data ?? '未知错误');
            cache('voiceerrmsg', ['errmsg' => $errmsg, 'time' => date('Y-m-d H:i:s')], 86400);
            Log::error('[send_voice]' . $errmsg);
            return false;
        }
    }

    public static function send_wechat_tplmsg($scene, $openid, $param)
    {
        $wid = config_get('login_wx');
        if ($scene == 'order') {
            $template_id = config_get('wxnotice_tpl_order');
            if (strlen($param['out_trade_no']) > 32) $param['out_trade_no'] = substr($param['out_trade_no'], 0, 32);
            if (mb_strlen($param['name']) > 20) $param['name'] = mb_substr($param['name'], 0, 20);
            $data = [];
            if (config_get('wxnotice_tpl_order_no')) $data[config_get('wxnotice_tpl_order_no')] = ['value' => $param['trade_no']];
            if (config_get('wxnotice_tpl_order_name')) $data[config_get('wxnotice_tpl_order_name')] = ['value' => $param['name']];
            if (config_get('wxnotice_tpl_order_money')) $data[config_get('wxnotice_tpl_order_money')] = ['value' => '¥' . $param['money']];
            if (config_get('wxnotice_tpl_order_time')) $data[config_get('wxnotice_tpl_order_time')] = ['value' => $param['time']];
            if (config_get('wxnotice_tpl_order_outno')) $data[config_get('wxnotice_tpl_order_outno')] = ['value' => $param['out_trade_no']];
            $jumpurl = request()->siteurl . 'user/#/trade/order';
        } elseif ($scene == 'settle') {
            $template_id = config_get('wxnotice_tpl_settle');
            $data = [];
            if (config_get('wxnotice_tpl_settle_type')) $data[config_get('wxnotice_tpl_settle_type')] = ['value' => '结算成功'];
            if (config_get('wxnotice_tpl_settle_account')) $data[config_get('wxnotice_tpl_settle_account')] = ['value' => $param['account']];
            if (config_get('wxnotice_tpl_settle_money')) $data[config_get('wxnotice_tpl_settle_money')] = ['value' => '¥' . $param['money']];
            if (config_get('wxnotice_tpl_settle_realmoney')) $data[config_get('wxnotice_tpl_settle_realmoney')] = ['value' => '¥' . $param['realmoney']];
            if (config_get('wxnotice_tpl_settle_time')) $data[config_get('wxnotice_tpl_settle_time')] = ['value' => $param['time']];
            $jumpurl = isset($param['jumpurl']) ? $param['jumpurl'] : request()->siteurl . 'user/settle.php';
        } elseif ($scene == 'login') {
            $template_id = config_get('wxnotice_tpl_login');
            $data = [];
            if (config_get('wxnotice_tpl_login_user')) $data[config_get('wxnotice_tpl_login_user')] = ['value' => $param['user']];
            if (config_get('wxnotice_tpl_login_time')) $data[config_get('wxnotice_tpl_login_time')] = ['value' => $param['time']];
            if (config_get('wxnotice_tpl_login_name')) $data[config_get('wxnotice_tpl_login_name')] = ['value' => config_get('sitename')];
            if (config_get('wxnotice_tpl_login_ip')) $data[config_get('wxnotice_tpl_login_ip')] = ['value' => $param['clientip']];
            if (config_get('wxnotice_tpl_login_iploc')) $data[config_get('wxnotice_tpl_login_iploc')] = ['value' => $param['ipinfo']];
            $jumpurl = request()->siteurl . 'user/';
        } elseif ($scene == 'complain') {
            $template_id = config_get('wxnotice_tpl_complain');
            $data = [];
            if (mb_strlen($param['name']) > 20) $param['name'] = mb_substr($param['name'], 0, 20);
            if (mb_strlen($param['reason']) > 20) $param['reason'] = mb_substr($param['reason'], 0, 20);
            if (config_get('wxnotice_tpl_complain_order_no')) $data[config_get('wxnotice_tpl_complain_order_no')] = ['value' => $param['trade_no']];
            if (config_get('wxnotice_tpl_complain_time')) $data[config_get('wxnotice_tpl_complain_time')] = ['value' => $param['time']];
            if (config_get('wxnotice_tpl_complain_reason')) $data[config_get('wxnotice_tpl_complain_reason')] = ['value' => $param['content']];
            if (config_get('wxnotice_tpl_complain_type')) $data[config_get('wxnotice_tpl_complain_type')] = ['value' => $param['type']];
            if (config_get('wxnotice_tpl_complain_name')) $data[config_get('wxnotice_tpl_complain_name')] = ['value' => $param['name']];
            $jumpurl = request()->siteurl . 'user/';
        } elseif ($scene == 'balance') {
            $template_id = config_get('wxnotice_tpl_balance');
            $data = [];
            if (config_get('wxnotice_tpl_balance_user')) $data[config_get('wxnotice_tpl_balance_user')] = ['value' => $param['user']];
            if (config_get('wxnotice_tpl_balance_time')) $data[config_get('wxnotice_tpl_balance_time')] = ['value' => $param['time']];
            if (config_get('wxnotice_tpl_balance_money')) $data[config_get('wxnotice_tpl_balance_money')] = ['value' => $param['money']];
            if (config_get('wxnotice_tpl_balance_msg')) $data[config_get('wxnotice_tpl_balance_msg')] = ['value' => '为避免造成订单失败，请及时充值'];
            $jumpurl = request()->siteurl . 'user/';
        } elseif ($scene == 'wxtransfer') {
            $template_id = config_get('wxnotice_tpl_wxtransfer');
            $data = [];
            if (config_get('wxnotice_tpl_wxtransfer_money')) $data[config_get('wxnotice_tpl_wxtransfer_money')] = ['value' => '¥' . $param['money']];
            if (config_get('wxnotice_tpl_wxtransfer_time')) $data[config_get('wxnotice_tpl_wxtransfer_time')] = ['value' => $param['time']];
            $jumpurl = isset($param['jumpurl']) ? $param['jumpurl'] : request()->siteurl . 'user/';
        }
        if (empty($template_id) || empty($wid)) return false;

        $wechat = new \app\lib\wechat\WechatAPI($wid);
        try {
            return $wechat->sendTemplateMessage($openid, $template_id, $jumpurl, $data);
        } catch (Exception $e) {
            $errmsg = $e->getMessage();
            cache('wxtplerrmsg', ['errmsg' => $errmsg, 'time' => date('Y-m-d H:i:s')], 86400);
            Log::error('[send_wechat_tplmsg]' . $errmsg);
            return false;
        }
    }

    private static function send_mail_msg($scene, $receiver, $param)
    {
        [$title, $content] = self::get_msg_tpl($scene, $param);
        if (empty($content)) return;
        $result = send_mail($receiver, $title, $content);
        if ($result === true) return true;

        if (!empty($result)) {
            cache('mailerrmsg', ['errmsg' => $result, 'time' => date('Y-m-d H:i:s')], 86400);
        }
        return false;
    }

    private static function send_telegram_msg($scene, $chatId, $param)
    {
        [$title, $content] = self::get_msg_tpl($scene, $param);
        if (empty($content)) return false;
        return self::send_telegram_content($chatId, $title, $content);
    }

    private static function send_telegram_content($chatId, $title, $content)
    {
        if (empty($chatId) || empty($content)) return false;
        try {
            return TelegramConnect::sendMessage((string) $chatId, $title, $content);
        } catch (Exception $e) {
            Log::error('[send_telegram_msg]' . $e->getMessage());
            return false;
        }
    }

    private static function send_qqbot_msg($scene, $msgconfig, $param)
    {
        [$title, $content] = self::get_msg_tpl($scene, $param);
        if (empty($content) || empty($msgconfig['qqbot_openid'])) return false;
        return self::send_qqbot_content($msgconfig, $title, $content);
    }

    private static function send_admin_qqbot_msg($scene, $param)
    {
        [$title, $content] = self::get_msg_tpl($scene, $param);
        if (empty($content)) return false;
        return self::send_qqbot_content(self::get_admin_qqbot_config(), $title, $content);
    }

    private static function send_qqbot_content($config, $title, $content)
    {
        if (empty($config['qqbot_openid']) || empty($content)) return false;
        try {
            return QqBot::fromMsgConfig($config)->sendMarkdown($config['qqbot_openid'], QqBot::toMarkdown($title, $content));
        } catch (Exception $e) {
            Log::error('[send_qqbot_msg]' . $e->getMessage());
            return false;
        }
    }

    private static function get_admin_qqbot_config()
    {
        return [
            'qqbot_appid' => trim((string) config_get('qqbot_appid')),
            'qqbot_appsecret' => trim((string) config_get('qqbot_appsecret')),
            'qqbot_openid' => trim((string) config_get('qqbot_openid')),
        ];
    }

    private static function send_robot_msg($scene, $url, $param, $admin = false)
    {
        if (empty($url)) return false;
        [$title, $content] = self::get_msg_tpl($scene, $param);
        if (empty($content)) return;

        return self::robot_webhook($url, $title, $content, $admin);
    }

    public static function robot_webhook($url, $title, $content, $admin = false)
    {
        $content = str_replace(['*', '<br/>', '<b>', '</b>'], ['\*', "\n", '**', '**'], $content);

        if (strpos($url, 'oapi.dingtalk.com')) {
            $content = '### ' . $title . "  \n " . str_replace("\n", "  \n ", $content);
            $post = [
                'msgtype' => 'markdown',
                'markdown' => [
                    'title' => $title,
                    'text' => $content,
                ],
            ];
        } elseif (strpos($url, 'qyapi.weixin.qq.com')) {
            $content = '## ' . $title . "\n" . $content;
            $post = [
                'msgtype' => 'markdown',
                'markdown' => [
                    'content' => $content,
                ],
            ];
        } elseif (strpos($url, 'open.feishu.cn') || strpos($url, 'open.larksuite.com')) {
            $content = str_replace(['\*', '**'], ['*', ''], strip_tags($content));
            $post = [
                'msg_type' => 'text',
                'content' => [
                    'text' => $content,
                ],
            ];
        } else {
            if ($admin) {
                cache('mailerrmsg', ['errmsg' => '不支持的Webhook地址', 'time' => date('Y-m-d H:i:s')], 86400);
            }
            return false;
        }
        $result = get_curl($url, json_encode($post), 0, 0, 0, 0, 0, ['Content-Type: application/json; charset=UTF-8']);
        if ($result !== false) {
            $arr = json_decode($result, true);
            if (isset($arr['errcode']) && $arr['errcode'] == 0 || isset($arr['code']) && $arr['code'] == 0) {
                return true;
            } else {
                Log::error('[send_robot_msg]' . $result);
                if ($admin) {
                    cache('mailerrmsg', ['errmsg' => $result, 'time' => date('Y-m-d H:i:s')], 86400);
                }
            }
        } else {
            Log::error('[send_robot_msg]接口请求失败');
        }
        return false;
    }

    private static function get_msg_tpl($scene, $param)
    {
        $sitename = config_get('sitename');
        $title = '';
        $content = '';
        if ($scene == 'regaudit') {
            $title = '新注册商户待审核提醒';
            $content = '尊敬的' . $sitename . '管理员，网站有新注册的商户待审核，请及时前往用户列表审核处理。<br/><b>商户ID：</b>' . $param['uid'] . '<br/><b>注册账号：</b>' . $param['account'] . '<br/><b>注册时间：</b>' . date('Y-m-d H:i:s');
        } elseif ($scene == 'apply') {
            $title = '新的提现待处理提醒';
            $content = '尊敬的' . $sitename . '管理员，商户' . $param['uid'] . '发起了手动提现申请，请及时处理。<br/><b>商户ID：</b>' . $param['uid'] . '<br/><b>提现方式：</b>' . $param['type'] . '<br/><b>提现金额：</b>' . $param['realmoney'] . '<br/><b>提交时间：</b>' . date('Y-m-d H:i:s');
        } elseif ($scene == 'domain') {
            $title = '新的授权支付域名待审核提醒';
            $content = '尊敬的' . $sitename . '管理员，商户' . $param['uid'] . '提交了新的授权支付域名，请及时审核处理。<br/><b>商户ID：</b>' . $param['uid'] . '<br/><b>授权域名：</b>' . $param['domain'] . '<br/><b>提交时间：</b>' . date('Y-m-d H:i:s');
        } elseif ($scene == 'internal_transfer') {
            $title = '新的站内转账待审核提醒';
            $content = '尊敬的' . $sitename . '管理员，商户' . $param['uid'] . '发起了站内转账，请及时审核处理。<br/><b>发起方商户ID：</b>' . $param['uid'] . '<br/><b>收款方商户ID：</b>' . $param['touid'] . '<br/><b>转账金额：</b>¥' . $param['money'] . '<br/><b>提交时间：</b>' . date('Y-m-d H:i:s');
        } elseif ($scene == 'order') {
            $title = '新订单通知 - ' . $sitename;
            $content = '尊敬的商户，您有一条新订单通知。<br/><b>商品名称：</b>' . $param['name'] . '<br/><b>订单金额：</b>¥' . $param['money'] . '<br/><b>支付方式：</b>' . $param['type'] . '<br/><b>商户订单号：</b>' . $param['out_trade_no'] . '<br/><b>系统订单号：</b>' . $param['trade_no'] . '<br/><b>支付完成时间：</b>' . $param['time'];
        } elseif ($scene == 'settle') {
            $title = '结算完成通知 - ' . $sitename;
            $content = '尊敬的商户，今日结算已完成，请查收。<br/><b>结算金额：</b>¥' . $param['money'] . '<br/><b>实际到账：</b>¥' . $param['realmoney'] . '<br/><b>结算账号：</b>' . $param['account'] . '<br/><b>结算完成时间：</b>' . $param['time'];
        } elseif ($scene == 'login') {
            $title = '账号登录通知 - ' . $sitename;
            $content = '尊敬的商户，您的账号<b>' . $param['user'] . '</b>已于' . $param['time'] . '成功登录到商户平台。<br/><b>登录IP：</b>' . $param['clientip'] . '<br/><b>登录时间：</b>' . $param['time'];
        } elseif ($scene == 'complain') {
            $title = '支付交易投诉通知 - ' . $sitename;
            $content = '尊敬的商户，' . $param['type'] . '！<br/><b>系统订单号：</b>' . $param['trade_no'] . '<br/><b>投诉原因：</b>' . $param['title'] . '<br/><b>投诉详情：</b>' . $param['content'] . '<br/><b>商品名称：</b>' . $param['name'] . '<br/><b>订单金额：</b>¥' . $param['money'] . '<br/><b>投诉时间：</b>' . $param['time'];
        } elseif ($scene == 'mchrisk') {
            $title = '渠道商户违规处置通知 - ' . $sitename;
            $content = '尊敬的商户，您有新的渠道商户违规处置记录！<br/><b>渠道子商户号：</b>' . $param['mchid'] . '<br/><b>商户名称：</b>' . $param['mchname'] . '<br/><b>风险类型：</b>' . $param['risk_desc'] . '<br/><b>管控能力：</b>' . $param['punish_type'] . '<br/><b>管控开始时间：</b>' . $param['punish_time'] . '<br/><b>解除方式：</b>' . $param['recover_way'];
        } elseif ($scene == 'balance') {
            $title = '商户余额不足提醒 - ' . $sitename;
            $content = '尊敬的商户，您的手续费余额不足' . $param['msgmoney'] . '元，为避免造成订单失败请及时充值。<br/>当前余额：' . $param['money'] . '元';
        } elseif ($scene == 'group') {
            $title = '会员用户组到期提醒 - ' . $sitename;
            $content = '尊敬的商户，您的购买的会员用户组 <b>' . $param['group'] . '</b> 已于 ' . $param['endtime'] . ' 到期，请及时前往商户平台续费。';
        } elseif ($scene == 'domain_audit') {
            $title = '授权域名审核结果通知 - ' . $sitename;
            $content = '尊敬的商户，您添加的授权域名 <b>' . $param['domain'] . '</b> '.($param['result'] == 1 ? '审核通过' : '审核未通过').'，请及时前往商户平台查看。';
        } elseif ($scene == 'channellimit') {
            $title = '支付通道达到限额提醒 - ' . $sitename;
            $content = '尊敬的管理员：支付通道"<b>' . $param['name'] . '</b>"今日已达到限额，该通道今日将不再接收新订单。<br/><b>通道ID：</b>' . $param['id'] . '<br/><b>限额原因：</b>' . $param['reason'] . '<br/><b>触发时间：</b>' . date('Y-m-d H:i:s');
        } elseif ($scene == 'daily_report') {
            $title = '收款日报通知 - ' . $sitename;
            $content = '尊敬的商户，以下是您 <b>' . $param['date'] . '</b> 的收款日报。<br/>'
                . '<b>订单金额总计：</b>¥' . $param['money'] . '<br/>';
            foreach ($param['type_money'] ?? [] as $item) {
                $content .= '<b>' . $item['name'] . '：</b>¥' . $item['money'] . '<br/>';
            }
            $content .= '<b>分成金额总计：</b>¥' . $param['getmoney'] . '<br/>'
                . '<b>订单总数：</b>' . $param['order_count'] . '<br/>'
                . '<b>成功订单数量：</b>' . $param['success_count'] . '<br/>'
                . '<b>订单成功率：</b>' . $param['success_rate'] . '%';
            if (!empty($param['has_transfer'])) {
                $content .= '<br/><b>代付总额：</b>¥' . $param['transfer_total'];
                foreach ($param['transfer_types'] ?? [] as $item) {
                    $content .= '<br/><b>' . $item['name'] . '代付：</b>¥' . $item['money'];
                }
            }
        }
        return [$title, $content];
    }

    private static function getMessageSwitch($scene)
    {
        return config_get('msgconfig_' . $scene, '0');
    }
}
