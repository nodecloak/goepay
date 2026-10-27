<?php

declare(strict_types=1);

namespace app\controller\user;

use app\BaseController;
use app\service\ProfitSharingService;
use think\facade\Db;

/**
 * 分账管理
 */
class ProfitSharing extends BaseController
{
    private ProfitSharingService $service;

    protected function initialize(): void
    {
        parent::initialize();
        $this->service = new ProfitSharingService();
    }

    /** 分账规则列表（仅当前用户） */
    public function receiverList()
    {
        $data = $this->service->getReceiverList($this->request);
        return json_response(200, 'ok', $data);
    }

    /** 分账记录列表 */
    public function orderList()
    {
        $data = $this->service->getOrderList($this->request);
        return json_response(200, 'ok', $data);
    }

    /** 分账订单导出 */
    public function orderExport()
    {
        $starttime = $this->request->get('starttime', '');
        $endtime = $this->request->get('endtime', '');
        if ($starttime === '' || $endtime === '') {
            return $this->sysmsg('请选择时间范围');
        }
        $result = $this->service->exportOrders($this->request, $starttime, $endtime);
        return $this->fileResponse($result['content'], $result['filename']);
    }

    /** 分账统计 */
    public function statistics()
    {
        $result = $this->service->getOrderStatistics($this->request);
        return json_response(200, 'ok', $result);
    }

    /** 获取单条分账规则详情 */
    public function get_receiver()
    {
        $id = $this->request->get('id/d');
        $row = $this->service->getReceiver($id);
        if (!$row) return json_response(400, '当前分账规则不存在！');
        return json_response(200, 'ok', $row);
    }

    /** 新增分账规则（子通道须属于当前用户） */
    public function add_receiver()
    {
        $subchannel = $this->request->post('subchannel/d', 0);
        $subrow = Db::name('subchannel')->where(['id' => $subchannel, 'uid' => $this->request->uid])->find();
        if (!$subrow) return json_response(400, '当前子通道不存在！');
        $mode = $this->request->post('mode/d', 0);
        $info = $this->request->post('info', '', 'trim');
        $minmoney = $this->request->post('minmoney', '', 'trim');
        if (!$subrow['channel'] || $info === '') {
            return json_response(400, '必填项不能为空');
        }
        if (Db::name('psreceiver')->where('subchannel', $subchannel)->find()) {
            return json_response(400, '该进件商户已存在分账规则');
        }
        $this->service->addReceiver($this->request->uid, $subrow['channel'], $subchannel, $info, $minmoney, $mode);
        return json_response(200, '新增分账规则成功！');
    }

    /** 编辑分账规则 */
    public function edit_receiver()
    {
        $id = $this->request->post('id/d', 0);
        $row = Db::name('psreceiver')->where(['id' => $id, 'uid' => $this->request->uid])->find();
        if (!$row) {
            return json_response(400, '当前分账规则不存在！');
        }
        $subchannel = $this->request->post('subchannel/d', 0);
        $subrow = Db::name('subchannel')->where(['id' => $subchannel, 'uid' => $this->request->uid])->find();
        if (!$subrow) {
            return json_response(400, '当前子通道不存在！');
        }
        $mode = $this->request->post('mode/d', 0);
        $info = $this->request->post('info', '', 'trim');
        $minmoney = $this->request->post('minmoney', '', 'trim');
        if (!$subrow['channel'] || $info === '') {
            return json_response(400, '必填项不能为空');
        }
        if (Db::name('psreceiver')->where('subchannel', $subchannel)->where('id', '<>', $id)->find()) {
            return json_response(400, '该进件商户已存在分账规则');
        }
        try {
            $this->service->editReceiver($id, $this->request->uid, $subrow['channel'], $subchannel, $info, $minmoney, $mode, $row);
        } catch (\Exception $e) {
            exception_log($e, '修改分账规则');
            return json_response(400, $e->getMessage());
        }
        return json_response(200, '修改分账规则成功！');
    }

    /** 设置分账规则状态 */
    public function set_receiver()
    {
        $id = $this->request->post('id/d', 0);
        $status = $this->request->post('status/d', 0);
        try {
            $this->service->setReceiverStatus($id, $status);
        } catch (\Exception $e) {
            exception_log($e, '设置分账规则状态');
            return json_response(400, $e->getMessage());
        }
        return json_response(200, '状态修改成功！');
    }

    /** 删除分账规则 */
    public function del_receiver()
    {
        $id = $this->request->post('id/d', 0);
        $this->service->deleteReceiver($id);
        return json_response(200, '删除分账规则成功！');
    }

    /** 提交分账 */
    public function submit()
    {
        $id = $this->request->post('id/d', 0);
        try {
            $result =$this->service->submitOrder($id);
        } catch (\Exception $e) {
            exception_log($e, '提交分账');
            return json_response(400, $e->getMessage());
        }
        return json_response(200, 'ok', $result);
    }

    /** 查询分账结果 */
    public function query()
    {
        $id = $this->request->post('id/d', 0);
        try {
            $result = $this->service->queryOrder($id);
        } catch (\Exception $e) {
            exception_log($e, '查询分账结果');
            return json_response(400, $e->getMessage());
        }
        return json_response(200, 'ok', $result);
    }

    /** 取消分账（解冻） */
    public function unfreeze()
    {
        $id = $this->request->post('id/d', 0);
        try {
            $result = $this->service->unfreezeOrder($id);
        } catch (\Exception $e) {
            exception_log($e, '取消分账');
            return json_response(400, $e->getMessage());
        }
        return json_response(200, 'ok', $result);
    }

    /** 分账回退 */
    public function return_()
    {
        $id = $this->request->post('id/d', 0);
        $money = $this->request->post('money', '', 'trim');
        try {
            $result = $this->service->returnOrder($id, $money);
        } catch (\Exception $e) {
            exception_log($e, '分账回退');
            return json_response(400, $e->getMessage());
        }
        return json_response(200, 'ok', $result);
    }

    /** 进件子商户列表（支持分账的插件，用于分账规则） */
    public function subchannelList()
    {
        $support_plugins = \app\logic\ProfitSharingLogic::getPlugins();
        $mode_plugins = \app\logic\ProfitSharingLogic::getModePlugins();
        $paytypelist = Db::name('type')->order('id', 'asc')->field('id,name,showname')->select()->toArray();
        $paytypeMap = array_column($paytypelist, 'showname', 'id');
        $paytypesMap = array_column($paytypelist, 'name', 'id');

        $channels = Db::name('subchannel')->alias('A')
            ->leftJoin('channel B', 'A.channel=B.id')
            ->where('A.uid', $this->request->uid)
            ->whereIn('B.plugin', $support_plugins)
            ->field('A.id, A.channel, A.apply_id, A.name, B.plugin, B.type')
            ->order('A.id', 'asc')
            ->select();
        $list = [];
        foreach ($channels as $row) {
            $row['typename'] = $paytypesMap[$row['type']] ?? '';
            $row['typeshowname'] = $paytypeMap[$row['type']] ?? '';
            $row['plugin_mode'] = in_array($row['plugin'], $mode_plugins);
            $list[] = $row;
        }
        return json_response(200, 'ok', $list);
    }
}
