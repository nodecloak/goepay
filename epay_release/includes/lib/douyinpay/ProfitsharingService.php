<?php

namespace lib\douyinpay;

use Exception;

/**
 * 分账服务类
 */
class ProfitsharingService extends BaseService
{
    public function __construct(array $config)
    {
        parent::__construct($config);
    }

    /**
     * 添加分账接收方
     * @param string $type 分账接收方类型
     * @param string $account 分账接收方账号
     * @param string|null $name 用户姓名(填写后校验)
     * @return mixed
     * @throws Exception
     */
    public function addReceiver(string $type, string $account, ?string $name = null)
    {
        $path = '/v1/trade/profitsharing/receivers/add';
        $params = [
            'appid' => $this->appId,
            'mchid' => $this->mchId,
            'type' => $type,
            'account' => $account,
            'relation_type' => 'SUPPLIER',
        ];
        if (!empty($name)) {
            $params['name'] = $this->rsaEncrypt($name);
        }
        return $this->execute('POST', $path, $params, true);
    }

    /**
     * 删除分账接收方
     * @param string $type 分账接收方类型
     * @param string $account 分账接收方账号
     * @return mixed
     * @throws Exception
     */
    public function deleteReceiver(string $type, string $account)
    {
        $path = '/v1/trade/profitsharing/receivers/delete';
        $params = [
            'appid' => $this->appId,
            'mchid' => $this->mchId,
            'type' => $type,
            'account' => $account,
        ];

        return $this->execute('POST', $path, $params);
    }

    /**
     * 请求分账
     * @param array $params 请求参数
     * @return mixed {"transaction_id":"抖音订单号","out_order_no":"商户分账单号","order_id":"抖音分账单号","status":"分账单状态","receivers":""}
     * @throws Exception
     */
    public function submit(array $params)
    {
        $path = '/v1/trade/profitsharing/orders';
        $publicParams = [
            'appid' => $this->appId,
            'mchid' => $this->mchId,
        ];
        $params = array_merge($publicParams, $params);
        return $this->execute('POST', $path, $params, true);
    }

    /**
     * 查询分账结果
     * @param string $out_order_no 商户分账单号
     * @param string $transaction_id 抖音订单号
     * @return mixed {"transaction_id":"抖音订单号","out_order_no":"商户分账单号","order_id":"抖音分账单号","status":"分账单状态","receivers":""}
     * @throws Exception
     */
    public function query(string $out_order_no, string $transaction_id)
    {
        $path = '/v1/trade/profitsharing/orders/' . $out_order_no;
        $params = [
            'mchid' => $this->mchId,
            'transaction_id' => $transaction_id,
        ];
        return $this->execute('GET', $path, $params);
    }

    /**
     * 解冻剩余资金
     * @param string $out_order_no 商户分账单号
     * @param string $transaction_id 抖音订单号
     * @return mixed {"transaction_id":"抖音订单号","out_order_no":"商户分账单号","order_id":"抖音分账单号"}
     * @throws Exception
     */
    public function unfreeze(string $out_order_no, string $transaction_id)
    {
        $path = '/v1/trade/profitsharing/finish-orders';
        $params = [
            'transaction_id' => $transaction_id,
            'out_order_no' => $out_order_no,
            'description' => '取消分账'
        ];
        return $this->execute('POST', $path, $params);
    }

    /**
     * 查询订单待分账金额
     * @param string $transaction_id 抖音订单号
     * @return mixed {"transaction_id":"抖音订单号","unsplit_amount":"订单剩余待分金额"}
     * @throws Exception
     */
    public function orderAmountQuery(string $transaction_id)
    {
        $path = '/v1/trade/profitsharing/order/' . $transaction_id . '/amounts';
        return $this->execute('GET', $path);
    }

    /**
     * 请求分账回退
     * @param array $params 请求参数
     * @return mixed
     * @throws Exception
     */
    public function return(array $params)
    {
        $path = '/v1/trade/profitsharing/return-orders';
        $publicParams = [
            'mchid' => $this->mchId,
        ];
        $params = array_merge($publicParams, $params);
        return $this->execute('POST', $path, $params);
    }

    /**
     * 查询分账回退结果
     * @param string $out_return_no 商户回退单号
     * @param string $out_order_no 商户分账单号
     * @return mixed
     * @throws Exception
     */
    public function returnQuery(string $out_return_no, string $out_order_no)
    {
        $path = '/v3/profitsharing/return-orders/' . $out_return_no;
        $params = [
            'out_order_no' => $out_order_no
        ];
        return $this->execute('GET', $path, $params);
    }
}
