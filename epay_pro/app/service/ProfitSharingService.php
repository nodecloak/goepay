<?php

declare(strict_types=1);

namespace app\service;

use app\controller\admin\ProfitSharing;
use app\logic\ProfitSharingLogic;
use think\facade\Db;
use think\Request;

class ProfitSharingService extends BaseService
{

    /**
     * 获取分账规则列表
     * @param Request $request
     * @return array ['total'=>int, 'records'=>array, 'current'=>int, 'size'=>int]
     */
    public function getReceiverList(Request $request): array
    {
        $paytypelist = Db::name('type')->order('id', 'asc')->field('id,name,showname')->select()->toArray();
        $paytypeMap = array_column($paytypelist, 'showname', 'id');
        $paytypesMap = array_column($paytypelist, 'name', 'id');

        $query = Db::name('psreceiver')->alias('A');
        if ($this->isAdmin) {   
            $value = $request->post('value', '', 'trim');
            if ($value !== '') {
                $column = $request->post('column', '');
                if ($column == 'info') {
                    $query->whereLike('A.info|A.account', '%' . $value . '%');
                } else {
                    $query->where('A.' . $column, $value);
                }
            }
        } else {
            $query->where('A.uid', $request->uid);
            $kw = $request->post('kw', '', 'trim');
            if ($kw !== '') {
                $query->where(function ($q) use ($kw) {
                    $q->where('A.id', $kw)->whereOr('A.info', 'like', '%' . $kw . '%')->whereOr('A.account', 'like', '%' . $kw . '%');
                });
            }
        }
        $current = $request->post('current/d', 1);
        $size = $request->post('size/d', 30);
        $offset = ($current - 1) * $size;
        $total = $query->count();
        $list = $query->leftJoin('channel B', 'A.channel=B.id')
            ->leftJoin('subchannel C', 'A.subchannel=C.id')
            ->field('A.*, B.type, B.name channelname, C.name subchannelname, C.apply_id')
            ->order('A.id', 'desc')
            ->limit($offset, $size)
            ->select();
        $list2 = [];
        foreach ($list as $row) {
            $row['typename'] = $paytypesMap[$row['type']] ?? '';
            $row['typeshowname'] = $paytypeMap[$row['type']] ?? '';
            $list2[] = $row;
        }
        return [
            'total' => $total,
            'records' => $list2,
            'current' => $current,
            'size' => $size,
        ];
    }

    private function buildOrderListQuery(Request $request)
    {
        $query = Db::name('psorder')->alias('A');
        if (!$this->isAdmin) {
            $ridIds = Db::name('psreceiver')->where('uid', $request->uid)->column('id');
            $query->whereIn('A.rid', $ridIds);
        }
        $rid = $request->param('rid', '');
        if ($rid !== '') {
            $query->where('A.rid', (int) $rid);
        }
        $dstatus = $request->param('dstatus');
        if (!isNullOrEmpty($dstatus)) {
            $query->where('A.status', (int) $dstatus);
        }
        $starttime = $request->param('starttime', '');
        if ($starttime !== '') {
            $query->where('A.addtime', '>=', $starttime . ' 00:00:00');
        }
        $endtime = $request->param('endtime', '');
        if ($endtime !== '') {
            $query->where('A.addtime', '<=', $endtime . ' 23:59:59');
        }
        if ($this->isAdmin) {
            $column = $request->param('column', '');
            $value = $request->param('value', '', 'trim');
            if ($value !== '') {
                $query->where('A.' . $column, $value);
            }
        } else {
            $kw = $request->param('kw', '', 'trim');
            if ($kw !== '') {
                $type = $request->param('type/d', 1);
                if ($type == 1) {
                    $query->where('A.trade_no', $kw);
                } elseif ($type == 2) {
                    $query->where('A.api_trade_no', $kw);
                } elseif ($type == 3) {
                    $query->whereLike('A.money', '%' . $kw . '%');
                }
            }
        }
        return $query;
    }

    /**
     * 分账订单列表
     * @param Request $request
     * @return array ['total'=>int, 'records'=>array, 'current'=>int, 'size'=>int]
     */
    public function getOrderList(Request $request): array
    {
        $paytypelist = Db::name('type')->order('id', 'asc')->field('id,name,showname')->select()->toArray();
        $paytype = array_column($paytypelist, 'showname', 'id');
        $paytypes = array_column($paytypelist, 'name', 'id');

        $query = $this->buildOrderListQuery($request);
        $query->leftJoin('psreceiver B', 'A.rid=B.id')
            ->leftJoin('channel C', 'B.channel=C.id')
            ->leftJoin('order D', 'D.trade_no=A.trade_no');
        $current = $request->post('current/d', 1);
        $size = $request->post('size/d', 30);
        $offset = ($current - 1) * $size;
        $total = $query->count();
        $list = $query->field('A.*, C.id channelid, C.name channelname, C.type, D.realmoney ordermoney')
            ->order('A.id', 'desc')
            ->limit($offset, $size)
            ->select();
        $list2 = [];
        foreach ($list as $row) {
            $row['typename'] = $paytypes[$row['type']] ?? '';
            $row['typeshowname'] = $paytype[$row['type']] ?? '';
            $list2[] = $row;
        }
        return [
            'total' => $total,
            'records' => $list2,
            'current' => $current,
            'size' => $size,
        ];
    }

    /**
     * 分账订单导出
     * @param Request $request
     * @param string $starttime
     * @param string $endtime
     * @return array ['content'=>string, 'filename'=>string]
     */
    public function exportOrders(Request $request, string $starttime, string $endtime): array
    {
        $paytype = Db::name('type')->column('showname', 'id') ?: [];
        $query = $this->buildOrderListQuery($request);

        $file = "系统订单号,分账规则,支付方式,订单金额,分账金额,已回退金额,时间,分账状态\r\n";
        $statusMap = [0 => '待分账', 1 => '已提交', 2 => '分账成功', 3 => '分账失败', 4 => '已取消'];
        $rows = $query->leftJoin('psreceiver B', 'A.rid=B.id')->leftJoin('channel C', 'B.channel=C.id')->leftJoin('order D', 'D.trade_no=A.trade_no')->field('A.*,C.id channelid,C.name channelname,C.type,D.realmoney ordermoney')->order('A.id', 'desc')->limit(100000)->select();

        foreach ($rows as $row) {
            $statusText = $statusMap[$row['status']] ?? '待分账';
            $returnMoney = number_format((float) ($row['return_money'] ?? 0), 2, '.', '');
            $file .= '="' . $row['trade_no'] . '",' . $row['rid'] . ',' . ($paytype[$row['type']] ?? '') . ',' . ($row['ordermoney'] ?? '') . ',' . $row['money'] . ',' . $returnMoney . ',' . $row['addtime'] . ',' . $statusText . "\r\n";
        }
        $file = "\xEF\xBB\xBF" . $file;
        $fname = 'psorder_' . $starttime . '_' . $endtime . '.csv';
        return ['content' => $file, 'filename' => $fname];
    }

    /**
     * 分账订单统计
     * @param Request $request
     * @return array ['totalMoney'=>string, 'successMoney'=>string, 'failMoney'=>string, 'totalCount'=>int, 'successCount'=>int, 'failCount'=>int, 'successRate'=>float]
     */
    public function getOrderStatistics(Request $request): array
    {
        $query = $this->buildOrderListQuery($request);
        $result = $query->fieldRaw('
        SUM(A.money) AS totalMoney,
        SUM(CASE WHEN A.status = 2 THEN A.money ELSE 0 END) AS successMoney,
        SUM(CASE WHEN A.status = 3 THEN A.money ELSE 0 END) AS failMoney,
        COUNT(*) AS totalCount,
        SUM(CASE WHEN A.status = 2 THEN 1 ELSE 0 END) AS successCount,
        SUM(CASE WHEN A.status = 3 THEN 1 ELSE 0 END) AS failCount')->select();
        $result = $result[0] ?? [];
        $totalCount = (int) ($result['totalCount'] ?? 0);
        $successCount = (int) ($result['successCount'] ?? 0);
        $successRate = $totalCount > 0 ? round($successCount / $totalCount * 100, 2) : 0;
        return [
            'totalMoney' => number_format((float) ($result['totalMoney'] ?? 0), 2, '.', ''),
            'successMoney' => number_format((float) ($result['successMoney'] ?? 0), 2, '.', ''),
            'failMoney' => number_format((float) ($result['failMoney'] ?? 0), 2, '.', ''),
            'totalCount' => $totalCount,
            'successCount' => $successCount,
            'failCount' => (int) ($result['failCount'] ?? 0),
            'successRate' => $successRate,
        ];
    }


    /**
     * 获取分账规则详情
     * @param int $id
     * @return array|null
     */
    public function getReceiver(int $id): ?array
    {
        $row = Db::name('psreceiver')->where('id', $id)->find();
        if (!$row) return null;
        $row['info'] = !empty($row['info']) ? json_decode($row['info'], true) : [['account' => $row['account'], 'name' => $row['name'], 'rate' => $row['rate']]];
        return $row;
    }

    /**
     * 新增分账规则
     * @param int $uid
     * @param int $channel
     * @param int $subchannel
     * @param string $info
     * @param string $minmoney
     * @param int $mode
     */
    public function addReceiver(int|null $uid, int $channel, int|null $subchannel, string $info, string|null $minmoney, int|null $mode)
    {
        Db::name('psreceiver')->insert([
            'uid' => $uid,
            'channel' => $channel,
            'subchannel' => $subchannel,
            'info' => $info,
            'minmoney' => $minmoney,
            'mode' => $mode,
            'status' => 0,
            'addtime' => date('Y-m-d H:i:s'),
        ]);
    }

    /**
     * 编辑分账规则
     * @param int $id
     * @param int|null $uid
     * @param int $channel
     * @param int $subchannel
     * @param string $info
     * @param string|null $minmoney
     * @param int|null $mode
     * @param array $row
     */
    public function editReceiver(int $id, int|null $uid, int $channel, int|null $subchannel, string $info, string|null $minmoney, int|null $mode, array $row)
    {
        if ($row['status'] == 1 && $channel != $row['channel']) {
            throw new \Exception('请先将状态改为已关闭再切换通道');
        }
        if ($row['status'] == 1 && $info != $row['info']) {
            $channelInfo = $row['uid'] ? Db::name('user')->where('uid', $row['uid'])->value('channelinfo') : null;
            $channelRow = $row['subchannel'] > 0 ? \app\lib\Channel::getSub($row['subchannel']) : \app\lib\Channel::get($row['channel'], $channelInfo);
            if ($channelRow) {
                $new_info = json_decode($info, true);
                $old_info = !empty($row['info']) ? json_decode($row['info'], true) : [['account' => $row['account'], 'name' => $row['name'], 'rate' => $row['rate']]];
                $model = ProfitSharingLogic::getModel($channelRow);
                if (!$model) throw new \Exception('当前分账插件不存在');
                foreach ($new_info ?: [] as $item) {
                    $found = false;
                    foreach ($old_info as $v) {
                        if (($v['account'] ?? '') == ($item['account'] ?? '')) {
                            $found = true;
                            break;
                        }
                    }
                    if (!$found) {
                        $model->addReceiver($item['account'] ?? '', $item['name'] ?? '');
                    }
                }
            }
        }
        Db::name('psreceiver')->where('id', $id)->update([
            'channel' => $channel,
            'uid' => $uid,
            'subchannel' => $subchannel,
            'info' => $info,
            'minmoney' => $minmoney,
            'mode' => $mode,
        ]);
    }

    /**
     * 设置分账规则状态
     * @param int $id
     * @param int $status
     */
    public function setReceiverStatus(int $id, int $status)
    {
        $query = Db::name('psreceiver')->where('id', $id);
        if (!$this->isAdmin) {
            $query->where('uid', request()->uid);
        }
        $row = $query->find();
        if (!$row) throw new \Exception('当前分账规则不存在！');
        $channelInfo = $row['uid'] ? Db::name('user')->where('uid', $row['uid'])->value('channelinfo') : null;
        $channel = $row['subchannel'] > 0 ? \app\lib\Channel::getSub($row['subchannel']) : \app\lib\Channel::get($row['channel'], $channelInfo);
        if (!$channel) throw new \Exception('当前支付通道不存在！');
        $model = ProfitSharingLogic::getModel($channel);
        if (!$model) throw new \Exception('当前分账插件不存在');
        $row['info'] = !empty($row['info']) ? json_decode($row['info'], true) : [['account'=>$row['account'], 'name'=>$row['name'], 'rate'=>$row['rate']]];
        foreach ($row['info'] as $item) {
            if ($status == 1) {
                $model->addReceiver($item['account'], $item['name']);
            } elseif ($status == 0) {
                try {
                    $model->deleteReceiver($item['account']);
                } catch (\Exception $e) {
                    if (strpos($e->getMessage(), '分账接收方不存在') === false) {
                        throw $e;
                    }
                }
            }
        }
        Db::name('psreceiver')->where('id', $id)->update(['status' => $status]);
    }

    /**
     * 删除分账规则
     * @param int $id
     */
    public function deleteReceiver(int $id)
    {
        $query = Db::name('psreceiver')->where('id', $id);
        if (!$this->isAdmin) {
            $query->where('uid', request()->uid);
        }
        $query->delete();
    }

    /**
     * 获取分账订单
     * @param int $id
     * @return array|null
     */
    private function getOrderInfo(int $id): ?array
    {
        $query = Db::name('psorder')->alias('A')
            ->leftJoin('psreceiver B', 'A.rid=B.id')
            ->leftJoin('order C', 'C.trade_no=A.trade_no')
            ->where('A.id', $id);
        if (!$this->isAdmin) {
            $query->where('B.uid', request()->uid);
        }
        $row = $query->field('A.*, B.channel, B.account, B.name, B.rate, B.info, B.uid psuid, C.uid, C.subchannel, C.realmoney ordermoney')
            ->find();
        return $row;
    }

    

    /**
     * 提交分账订单
     * @param int $id
     * @return array
     */
    public function submitOrder(int $id): array
    {
        $row = $this->getOrderInfo($id);
        if (!$row) throw new \Exception('订单不存在');
        if ($row['status'] != 0 && $row['status'] != 3) throw new \Exception('只有待分账的订单才能提交分账');
        return ProfitSharingLogic::submitOrder($row);
    }

    /**
     * 查询分账订单
     * @param int $id
     * @return array
     */
    public function queryOrder(int $id): array
    {
        $row = $this->getOrderInfo($id);
        if (!$row) throw new \Exception('订单不存在');
        if ($row['status'] != 1) throw new \Exception('只有已提交的订单才能查询结果');
        return ProfitSharingLogic::queryOrder($row);
    }

    /**
     * 取消分账订单
     * @param int $id
     * @return array
     */
    public function unfreezeOrder(int $id)
    {
        $row = $this->getOrderInfo($id);
        if (!$row) throw new \Exception('订单不存在');
        if ($row['status'] == 2) throw new \Exception('只有待分账的订单才能取消分账');
        return ProfitSharingLogic::unfreezeOrder($row);
    }

    /**
     * 分账回退订单
     * @param int $id
     * @return array
     */
    public function returnOrder(int $id, string $money)
    {
        $row = $this->getOrderInfo($id);
        if (!$row) throw new \Exception('订单不存在');
        $returnMoney = (float) ($row['return_money'] ?? 0);
        $shareMoney = (float) $row['money'];
        $isPartialCanceled = $row['status'] == 4 && $returnMoney > 0 && $returnMoney < $shareMoney;
        if ($row['status'] != 2 && !$isPartialCanceled) throw new \Exception('只有分账成功或部分回退的订单才能回退');
        if (!is_numeric($money) || !preg_match('/^[0-9]+(\.[0-9]{1,2})?$/', $money)) throw new \Exception('回退金额输入错误');
        $money = number_format((float) $money, 2, '.', '');
        if ((float) $money <= 0) throw new \Exception('回退金额必须大于0');
        if ((float) $money > round($shareMoney - $returnMoney, 2)) throw new \Exception('回退金额不能大于剩余可回退金额');
        return ProfitSharingLogic::returnOrder($row, $money);
    }
}