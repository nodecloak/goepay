<?php

namespace app\lib;

use Exception;
use think\facade\Db;
use think\facade\Log;

/**
 * 微信违规商户处置通知
 * @see https://pay.weixin.qq.com/doc/v3/partner/4012064844
 */
class WxMchRisk
{
    private $channel;
    private $service;

    private static $relate_limitations = ['NO_TRANSACTION_AND_RECHARGE' => '关闭收单和充值', 'NO_PAYMENT' => '关闭付款', 'NO_WITHDRAWAL' => '关闭提现', 'NO_REFUND' => '关闭退款', 'NO_TRANSACTION' => '关闭收单', 'NO_PROFIT_SHARING' => '关闭分账分出', 'NO_PAYMENT_POINT_COMPLETE_ORDER' => '关闭支付分服务结单'];
    private static $recover_way = ['IRRECOVERABLE' => '不可恢复', 'MODIFY_SUBJECT_INFORMATION' => '修改主体资料', 'MODIFY_SETTLE_ACCOUNT_INFORMATION' => '修改结算银行账户', 'VERIFY_INACTIVE_MERCHANT_IDENTITY' => '核实商户身份', 'SUBMIT_OFFLINE_BUSINESS_SCENARIO_INFORMATION' => '提交线下经营场景信息', 'SUBMIT_INFORMATION_FOR_APPEAL' => '提交相关信息申诉', 'RESOLVE_TRANSACTION_DISPUTES' => '解决交易纠纷', 'MODIFY_ADMINISTRATOR_INFORMATION' => '修改超级管理员', 'CALL_CUSTOMER_SERVICE_AT_95017' => '拨打微信支付客服电话95017', 'UPDATE_BUSINESS_SCENARIO_INFORMATION' => '更新经营场景信息', 'SUBMIT_CDD_INFORMATION' => '填写尽调信息', 'WAITING_FOR_PLATFORM_REVIEW' => '等待平台审核', 'SUBMIT_UBO_INFORMATION' => '补充受益所有人信息', 'SIGN_ANTI_FRAUD_PLEDGE_AND_VERIFY_FACE' => '签署反诈承诺书并刷脸核实身份'];

    public function __construct($channel)
    {
        $this->channel = $channel;
    }

    //设置商户违规通知回调地址
    public function setNotifyUrl()
    {
        $notifyUrl = config_get('localurl') . 'pay/mchrisknotify/' . $this->channel['id'] . '/';
        try {
            $this->getService($this->channel);

            $result = $this->queryNotifications();
            if ($result['notify_url'] == $notifyUrl) return ['code' => 0];

            $this->updateNotifications($notifyUrl);

            return ['code' => 0];
        } catch (\WeChatPay\V3\WeChatPayException $e) {
            if ($e->getResponse()['code'] != 'NOT_FOUND') {
                $errmsg = $e->getMessage();
                if (strpos($errmsg, '专线地址有误，') !== false) {
                    $errmsg = '回调地址只能使用https协议';
                }
                return ['code' => -1, 'msg' => $errmsg];
            }
        } catch (Exception $e) {
            return ['code' => -1, 'msg' => $e->getMessage()];
        }

        try {
            $this->createNotifications($notifyUrl);
            return ['code' => 0];
        } catch (Exception $e) {
            $errmsg = $e->getMessage();
            if (strpos($errmsg, '专线地址有误，') !== false) {
                $errmsg = '回调地址只能使用https协议';
            }
            return ['code' => -1, 'msg' => $errmsg];
        }
    }

    //删除商户违规通知回调地址
    public function delNotifyUrl()
    {
        try {
            $this->getService($this->channel);

            $this->queryNotifications();

            $this->deleteNotifications();

            return ['code' => 0];
        } catch (\WeChatPay\V3\WeChatPayException $e) {
            if ($e->getResponse()['code'] == 'NOT_FOUND') {
                return ['code' => 0];
            }
        } catch (Exception $e) {
            return ['code' => -1, 'msg' => $e->getMessage()];
        }
    }

    //商户违规通知回调
    public function notify($params)
    {
        $thirdid = $params['message_content']['business_code'] ?? '';
        $mchid = $params['message_content']['merchant_code'] ?? '';
        $mchname = $params['message_content']['merchant_company_name'] ?? '';
        $business_state = $params['message_content']['business_state'] ?? '';
        $uid = 0;
        $merchant = Db::name('applymerchant')->where('mchid', $mchid)->find();
        if ($merchant) $uid = $merchant['uid'];

        if ($business_state == 'RECOVERY' || $business_state == 'PUNISHMENTCANCELLED') {
            $row = Db::name('mchrisk')->where('thirdid', $thirdid)->find();
            if ($row) {
                Db::name('mchrisk')->where('id', $row['id'])->update(['status' => 1]);
            }
            return;
        }

        try {
            $this->getService($this->channel);
            $path = '/v3/mch-operation-manage/merchant-limitations/sub-mchid/' . $mchid;
            $result = $this->service->execute('GET', $path);
        } catch (Exception $e) {
            Log::error('[商户违规通知回调][' . $this->channel['id'] . ']' . $e->getMessage());
            return;
        }
        $item = array_filter($result['recovery_specifications'] ?? [], function ($v) use ($thirdid) {
            return $v['limitation_case_id'] == $thirdid;
        });
        if (empty($item)) return;
        $item = array_values($item)[0];
        $punish_types = [];
        foreach ($item['relate_limitations'] as $limitations) {
            if (isset(self::$relate_limitations[$limitations])) {
                $punish_types[] = self::$relate_limitations[$limitations];
            } else {
                $punish_types[] = $limitations;
            }
        }
        if (!empty($item['other_relate_limitations'])) {
            $punish_types[] = explode(',', $item['other_relate_limitations']);
        }
        if ((in_array('关闭付款', $punish_types) || in_array('关闭收单', $punish_types)) && $merchant) {
            $this->autoCloseChannel($merchant['id']);
        }
        if (empty($mchname)) $mchname = $merchant['mchname'] ?? '';
        $data = [
            'thirdid' => $thirdid,
            'channel' => $this->channel['id'],
            'uid' => $uid,
            'mchid' => $mchid,
            'mchname' => $mchname,
            'risk_type' => $item['limitation_reason_type'],
            'risk_desc' => $item['limitation_reason'] . (!empty($item['limitation_reason_describe']) ? '（' . $item['limitation_reason_describe'] . '）' : ''),
            'punish_type' => implode('、', $punish_types),
            'punish_time' => $item['limitation_date'] ?? $item['limitation_start_date'] ?? date('Y-m-d H:i:s'),
            'recover_way' => self::$recover_way[$item['recover_way']] ?? '',
            'recover_url' => $item['recover_help_url'] ?? '',
            'status' => 0,
        ];
        $row = Db::name('mchrisk')->where(['channel' => $this->channel['id'], 'thirdid' => $thirdid, 'mchid' => $mchid])->find();
        if ($row) {
            Db::name('mchrisk')->where('id', $row['id'])->update($data);
        } else {
            Db::name('mchrisk')->insert($data);
            \app\lib\MsgNotice::sendAsync('mchrisk', $uid, $data);
        }
    }

    public function notify2($params)
    {
        $uid = 0;
        $thirdid = $params['thirdid'];
        $mchid = $params['mchid'] ?? '';
        $mchname = $params['mchname'] ?? '';
        $merchant = Db::name('applymerchant')->where('mchid', $mchid)->find();
        if ($merchant) {
            $uid = $merchant['uid'];
        } else {
            $report = Db::name('applyreport')->where('submchid', $mchid)->find();
            if ($report) {  
                $merchant = Db::name('applymerchant')->where('id', $report['mid'])->find();
                if ($merchant) {
                    $uid = $merchant['uid'];
                }
            }
        }
        if (empty($mchname)) $mchname = $merchant['mchname'] ?? '';
        $data = [
            'thirdid' => $thirdid,
            'channel' => $this->channel['id'],
            'uid' => $uid,
            'mchid' => $mchid,
            'mchname' => $mchname,
            'risk_type' => $params['risk_type'] ?? '',
            'risk_desc' => $params['risk_desc'] ?? '',
            'punish_type' => $params['punish_type'] ?? '',
            'punish_time' => $params['punish_time'] ?? date('Y-m-d H:i:s'),
            'recover_way' => $params['recover_way'] ?? '',
            'recover_url' => $params['recover_url'] ?? '',
            'status' => 0,
        ];
        $row = Db::name('mchrisk')->where(['channel' => $this->channel['id'], 'thirdid' => $thirdid, 'mchid' => $mchid])->find();
        if ($row) {
            Db::name('mchrisk')->where('id', $row['id'])->update($data);
        } else {
            Db::name('mchrisk')->insert($data);
            \app\lib\MsgNotice::sendAsync('mchrisk', $uid, $data);
        }
    }

    private function autoCloseChannel($apply_id)
    {
        $subchannellist = Db::name('subchannel')->where('apply_id', $apply_id)->select();
        foreach ($subchannellist as $subchannel) {
            Db::name('subchannel')->where('id', $subchannel['id'])->update(['status' => 0]);
        }
    }

    private function getService($channel)
    {
        $wechatpay_config = \app\common\PaymentConfig::getWxpayV3Config($channel);
        $this->service = new \WeChatPay\V3\BaseService($wechatpay_config);
    }

    /**
     * 创建商户违规通知回调地址
     * @param string $url 通知地址
     * @return mixed
     * @throws Exception
     */
    private function createNotifications(string $url)
    {
        $path = '/v3/merchant-risk-manage/violation-notifications';
        $params = [
            'notify_url' => $url
        ];
        return $this->service->execute('POST', $path, $params);
    }

    /**
     * 查询商户违规通知回调地址
     * @return mixed {"notify_url":"通知地址"}
     * @throws Exception
     */
    private function queryNotifications()
    {
        $path = '/v3/merchant-risk-manage/violation-notifications';
        return $this->service->execute('GET', $path);
    }

    /**
     * 更新商户违规通知回调地址
     * @param string $url 通知地址
     * @return mixed
     * @throws Exception
     */
    private function updateNotifications(string $url)
    {
        $path = '/v3/merchant-risk-manage/violation-notifications';
        $params = [
            'notify_url' => $url
        ];
        return $this->service->execute('PUT', $path, $params);
    }

    /**
     * 删除商户违规通知回调地址
     * @return void
     * @throws Exception
     */
    private function deleteNotifications()
    {
        $path = '/v3/merchant-risk-manage/violation-notifications';
        $this->service->execute('DELETE', $path);
    }
}
