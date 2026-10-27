<?php

declare(strict_types=1);

namespace app\common;

use app\logic\SubOrderLogic;
use think\facade\Db;
use think\facade\Config;

abstract class BasePayment implements PaymentInterface
{
    // 支付通道数据
    protected array $channel;
    
    // 支付插件根目录
    protected string $payRoot;

    public function __construct(array $channel)
    {
        $this->channel = $channel;
        $this->payRoot = PLUGIN_ROOT . 'payment/' . $channel['plugin'] . '/';
    }

    abstract public function submit(PaymentContext $ctx): array;

    // 异步回调处理订单支付结果
    protected function processNotify(array $order, $api_trade_no = null, $buyer = null, $bill_trade_no = null, $bill_mch_trade_no = null, $end_time = null)
    {
        (new \app\service\OrderProcessService($this->channel, $order))->processNotify($api_trade_no, $buyer, $bill_trade_no, $bill_mch_trade_no, $end_time);
    }

    // 同步回调处理订单支付结果
    protected function processReturn(array $order, $api_trade_no = null, $buyer = null, $bill_trade_no = null, $bill_mch_trade_no = null, $end_time = null): array
    {
        $jump_url = (new \app\service\OrderProcessService($this->channel, $order))->processReturn($api_trade_no, $buyer, $bill_trade_no, $bill_mch_trade_no, $end_time);
        return ['type' => 'return', 'url' => $jump_url];
    }

    // 转账回调处理
    protected function processTransfer($out_biz_no, $status, $errmsg = null)
    {
        \app\logic\TransferLogic::processNotify($out_biz_no, $status, $errmsg);
    }

    // 分账回调处理
    protected function processProfitSharing($trade_no, $status, $errmsg = null, $settle_no = null)
    {
        \app\logic\ProfitSharingLogic::processNotify($trade_no, $status, $errmsg, $settle_no);
    }

    // 加锁设置订单扩展数据
    protected static function lockPayData($trade_no, $func)
    {
        Db::startTrans();
        try {
            $data = Db::name('order')->where('trade_no', $trade_no)->lock(true)->value('ext');
            if ($data) {
                Db::rollback();
                return unserialize($data);
            }
            $data = $func();
            if ($data) {
                Db::name('order')->where('trade_no', $trade_no)->update(['ext' => serialize($data)]);
            }
            Db::commit();
            return $data;
        } catch (\Exception $e) {
            Db::rollback();
            throw $e;
        }
    }

    // 更新订单信息
    protected function updateOrder($trade_no, $api_trade_no, $buyer = null, $status = null)
    {
        $data = ['api_trade_no' => $api_trade_no];
        if (!empty($buyer)) $data['buyer'] = $buyer;
        if ($status) $data['status'] = $status;
        Db::name('order')->where('trade_no', $trade_no)->where('status', 0)->update($data);
    }

    // 更新订单扩展信息
    protected function updateOrderExt($trade_no, $data)
    {
        Db::name('order')->where('trade_no', $trade_no)->update(['ext' => serialize($data)]);
    }

    // 更新合单状态
    protected function updateOrderCombine($trade_no, $sub_orders = null)
    {
        SubOrderLogic::updateOrderCombine($trade_no, $sub_orders);
    }

    // 获取订单
    protected function getOrder($trade_no)
    {
        return Db::name('order')->alias('A')->leftJoin('type B', 'A.type=B.id')->where('A.trade_no', $trade_no)->field('A.*,B.name typename,B.showname typeshowname')->find();
    }

    // 获取子订单
    protected function getSubOrders($trade_no)
    {
        return SubOrderLogic::getSubOrders($trade_no);
    }

    // 处理子订单
    protected function processSubOrders($trade_no, $sub_orders)
    {
        SubOrderLogic::processSubOrders($trade_no, $sub_orders);
    }

    // 退款子订单
    protected function refundSubOrder($sub_trade_no, $refundmoney = null)
    {
        SubOrderLogic::refundSubOrder($sub_trade_no, $refundmoney);
    }

    // 更新子订单结算状态
    protected function updateSubOrderSettle($sub_trade_no, $settle)
    {
        SubOrderLogic::updateSubOrderSettle($sub_trade_no, $settle);
    }

    /**
     * 渲染消息提示页面
     * @param string $content 消息内容
     * @param int $type 消息类型 1=success 2=info 3=warning 4=danger
     * @param string|false $back 返回链接
     * @return \think\Response
     */
    protected function showMsg(string $content, int $type = 4, string|false $back = false): \think\Response
    {
        $panelMap = [1 => 'success', 2 => 'info', 3 => 'warning', 4 => 'danger'];
        $panel = $panelMap[$type] ?? 'danger';
        return view('common/plugin_msg', [
            'content' => $content,
            'panel' => $panel,
            'back' => $back,
        ]);
    }
}
