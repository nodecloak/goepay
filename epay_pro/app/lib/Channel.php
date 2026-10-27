<?php

namespace app\lib;

use think\facade\Db;

class Channel
{

	public static function get($id, $channelinfo = null)
	{
		$value = Db::name('channel')->where('id', $id)->find();
		if (!$value) return null;
		$channel = ['id' => $value['id'], 'name' => $value['name'], 'mode' => $value['mode'], 'type' => $value['type'], 'plugin' => $value['plugin'], 'apptype' => $value['apptype'], 'appwxmp' => $value['appwxmp'], 'appwxa' => $value['appwxa'], 'costrate' => $value['costrate'], 'daytop' => $value['daytop'], 'daymaxorder' => $value['daymaxorder'], 'ordername' => $value['ordername'], 'ordername_rules' => $value['ordername_rules'], 'payfee_lessthan' => $value['payfee_lessthan'] ?? null, 'payfee_mincost' => $value['payfee_mincost'] ?? null];

		if (empty($value['config'])) return $channel;

		$config = json_decode($value['config'], true);
		if (!empty($channelinfo) && !empty($config) && is_array($config)) {
			$arr = json_decode($channelinfo, true);
			if (is_array($arr)) {
				foreach ($config as $configkey => $configrow) {
					if ($configrow && substr($configrow, 0, 1) == '[') {
						$key = substr($configrow, 1, -1);
						$config[$configkey] = isset($arr[$key]) ? $arr[$key] : $configrow;
					}
				}
			}
		}

		if (!empty($config)) {
			$channel = array_merge($channel, $config);
		}
		return $channel;
	}

	public static function getSub($id)
	{
		$value = Db::name('subchannel')->alias('B')->join('channel A', 'B.channel=A.id')->where('B.id', $id)->field('A.*,B.info,B.paymin sub_paymin,B.paymax sub_paymax,B.id subid,B.name subname')->find();
		if (!$value) return null;
		[$paymin, $paymax] = self::getEffectivePayLimit($value['paymin'], $value['paymax'], $value['sub_paymin'], $value['sub_paymax']);
		$channel = ['id' => $value['id'], 'subid' => $value['subid'], 'name' => $value['name'], 'subname' => $value['subname'], 'mode' => $value['mode'], 'type' => $value['type'], 'plugin' => $value['plugin'], 'apptype' => $value['apptype'], 'appwxmp' => $value['appwxmp'], 'appwxa' => $value['appwxa'], 'costrate' => $value['costrate'], 'daytop' => $value['daytop'], 'daymaxorder' => $value['daymaxorder'], 'paymin' => $paymin, 'paymax' => $paymax, 'ordername' => $value['ordername'], 'ordername_rules' => $value['ordername_rules'], 'payfee_lessthan' => $value['payfee_lessthan'] ?? null, 'payfee_mincost' => $value['payfee_mincost'] ?? null];

		if (empty($value['config'])) return $channel;

		$config = json_decode($value['config'], true);
		if (!empty($value['info']) && !empty($config) && is_array($config)) {
			$arr = json_decode($value['info'], true);
			if (is_array($arr)) {
				foreach ($config as $configkey => $configrow) {
					if ($configrow && substr($configrow, 0, 1) == '[') {
						$key = substr($configrow, 1, -1);
						$config[$configkey] = isset($arr[$key]) ? $arr[$key] : $configrow;
					}
				}
			}
			if (isset($arr['apptype']) && !empty($arr['apptype'])) {
				$channel['apptype'] = $arr['apptype'];
			}
			if (isset($arr['appwxmp']) && $arr['appwxmp'] > 0) {
				$channel['appwxmp'] = $arr['appwxmp'];
				$channel['subappwxmp'] = 1;
			}
			if (isset($arr['appwxa']) && $arr['appwxa'] > 0) {
				$channel['appwxa'] = $arr['appwxa'];
				$channel['subappwxa'] = 1;
			}
		}
		if (!empty($config)) {
			$channel = array_merge($channel, $config);
		}
		return $channel;
	}

	public static function getGroup($gid)
	{
		$group = Db::name('group')->where('gid', $gid)->find();
		if (!$group) $group = Db::name('group')->where('gid', 0)->find();
		$info = json_decode($group['info'] ?? '{}', true);
		if (!is_array($info)) $info = [];

		$rows = Db::name('type')->where('status', 1)->field('id,name')->select();
		$paytype = [];
		foreach ($rows as $row) {
			$paytype[$row['id']] = $row['name'];
		}

		$subchannel_type = [];
		$subchannel_type_ids = [];
		foreach ($info as $id => $row) {
			if (!isset($paytype[$id]) || !is_array($row)) continue;
			if ((int) ($row['channel'] ?? 0) == -2) {
				$subchannel_type[] = $paytype[$id];
				$subchannel_type_ids[] = $id;
			}
		}
		$group['subchannel_type'] = $subchannel_type;
		$group['subchannel_type_ids'] = $subchannel_type_ids;
		return $group;
	}

	public static function info($id, $gid = 0)
	{
		$value = Db::name('channel')->where('id', $id)->field('id,plugin,type,rate,apptype,mode,paymin,paymax,rate_limit_seconds,rate_limit_count,payfee_lessthan,payfee_mincost')->find();
		if (!$value) return null;
		$money_rate = $value['rate'] ?? 0;
		if ($gid > 0) $groupinfo = Db::name('group')->where('gid', $gid)->value('info');
		if (empty($groupinfo)) $groupinfo = Db::name('group')->where('gid', 0)->value('info');
		if ($groupinfo) {
			$info = json_decode($groupinfo, true);
			if (is_array($info) && isset($info[$value['type']])) {
				$groupinfo = $info[$value['type']];
				if (is_array($groupinfo) && isset($groupinfo['rate']) && $groupinfo['rate'] !== '') {
					$money_rate = $groupinfo['rate'];
				}
			}
		}
		return ['typeid' => $value['type'], 'plugin' => $value['plugin'], 'channel' => $value['id'], 'subchannel' => 0, 'rate' => $money_rate, 'apptype' => $value['apptype'], 'mode' => $value['mode'], 'paymin' => $value['paymin'], 'paymax' => $value['paymax'], 'rate_limit_seconds' => $value['rate_limit_seconds'], 'rate_limit_count' => $value['rate_limit_count'], 'payfee_lessthan' => $value['payfee_lessthan'] ?? null, 'payfee_mincost' => $value['payfee_mincost'] ?? null];
	}

	public static function getWeixin($id)
	{
		$value = Db::name('weixin')->where('id', $id)->find();
		return $value;
	}

	// 支付提交处理（输入支付方式名称）
	public static function submit($type, $uid = 0, $gid = 0, $money = 0, $sub_mch_id = 0, $sub_channel_id = 0, $is_mobile = false)
	{
		if ($is_mobile) {
			$paytype = Db::name('type')->where('name', $type)->whereRaw('(device=0 OR device=2)')->field('id,name,status')->find();
		} else {
			$paytype = Db::name('type')->where('name', $type)->whereRaw('(device=0 OR device=1)')->field('id,name,status')->find();
		}
		if (!$paytype || $paytype['status'] == 0) return null;
		$typeid = $paytype['id'];
		$typename = $paytype['name'];

		return self::getSubmitInfo($typeid, $typename, $uid, $gid, $money, $sub_mch_id, $sub_channel_id);
	}

	// 支付提交处理2（输入支付方式ID）
	public static function submit2($typeid, $uid = 0, $gid = 0, $money = 0, $sub_mch_id = 0, $sub_channel_id = 0)
	{
		$paytype = Db::name('type')->where('id', $typeid)->field('id,name,status')->find();
		if (!$paytype || $paytype['status'] == 0) return null;
		$typename = $paytype['name'];

		return self::getSubmitInfo($typeid, $typename, $uid, $gid, $money, $sub_mch_id, $sub_channel_id);
	}

	//获取通道、插件、费率信息
	public static function getSubmitInfo($typeid, $typename, $uid, $gid, $money, $sub_mch_id = 0, $sub_channel_id = 0)
	{
		if ($gid > 0) $groupinfo = Db::name('group')->where('gid', $gid)->value('info');
		if (empty($groupinfo)) $groupinfo = Db::name('group')->where('gid', 0)->value('info');
		if ($groupinfo) {
			$info = json_decode($groupinfo, true);
			if (isset($info[$typeid]) && is_array($info[$typeid])) {
				$channel = $info[$typeid]['channel'];
				$money_rate = $info[$typeid]['rate'] ?? null;
				$is_roll = isset($info[$typeid]['type']) && $info[$typeid]['type'] == 'roll';
			} else {
				$channel = -1;
				$money_rate = null;
				$is_roll = false;
			}
			if ($channel == 0) { //当前商户关闭该通道
				return false;
			} elseif ($channel == -1) { //随机可用通道
				$rows = Db::name('channel')->where('type', $typeid)->where('status', 1)->where('daystatus', 0)->field('id,plugin,status,rate,apptype,mode,paymin,paymax,timestart,timestop,rate_limit_seconds,rate_limit_count,payfee_lessthan,payfee_mincost')->order('id', 'asc')->select();
				if (count($rows) > 0) {
					$newrows = [];
					foreach ($rows as $row) {
						if ($money > 0 && !empty($row['paymin']) && $row['paymin'] > 0 && $money < $row['paymin']) continue;
						if ($money > 0 && !empty($row['paymax']) && $row['paymax'] > 0 && $money > $row['paymax']) continue;
						if (self::isRateLimited($row)) continue;
						if (!isNullOrEmpty($row['timestart']) && !isNullOrEmpty($row['timestop']) && ($row['timestart'] > 0 || $row['timestop'] > 0)) {
							$hour = date('H');
							if ($row['timestart'] < $row['timestop']) {
								if ($hour < $row['timestart'] || $hour > $row['timestop']) continue;
							} else {
								if ($hour < $row['timestart'] && $hour > $row['timestop']) continue;
							}
						}
						$newrows[] = $row;
					}
					if (count($newrows) == 0) return false;
					$row = $newrows[array_rand($newrows)];
					if (empty($money_rate)) $money_rate = $row['rate'];
					return ['typeid' => $typeid, 'typename' => $typename, 'plugin' => $row['plugin'], 'channel' => $row['id'], 'subchannel' => 0, 'rate' => $money_rate, 'apptype' => $row['apptype'], 'mode' => $row['mode'], 'paymin' => $row['paymin'], 'paymax' => $row['paymax'], 'payfee_lessthan' => $row['payfee_lessthan'] ?? null, 'payfee_mincost' => $row['payfee_mincost'] ?? null];
				}
			} elseif ($channel == -4) { //顺序可用通道
				$rows = Db::name('channel')->where('type', $typeid)->where('status', 1)->where('daystatus', 0)->field('id,plugin,status,rate,apptype,mode,paymin,paymax,timestart,timestop,rate_limit_seconds,rate_limit_count,payfee_lessthan,payfee_mincost')->order('usetime', 'asc')->order('id', 'asc')->select();
				if (count($rows) > 0) {
					$newrows = [];
					foreach ($rows as $row) {
						if ($money > 0 && !empty($row['paymin']) && $row['paymin'] > 0 && $money < $row['paymin']) continue;
						if ($money > 0 && !empty($row['paymax']) && $row['paymax'] > 0 && $money > $row['paymax']) continue;
						if (self::isRateLimited($row)) continue;
						if (!isNullOrEmpty($row['timestart']) && !isNullOrEmpty($row['timestop']) && ($row['timestart'] > 0 || $row['timestop'] > 0)) {
							$hour = date('H');
							if ($row['timestart'] < $row['timestop']) {
								if ($hour < $row['timestart'] || $hour > $row['timestop']) continue;
							} else {
								if ($hour < $row['timestart'] && $hour > $row['timestop']) continue;
							}
						}
						$newrows[] = $row;
					}
					if (count($newrows) == 0) return false;
					$row = $newrows[0];
					Db::name('channel')->where('id', $row['id'])->update(['usetime' => (int)(microtime(true) * 1000)]);
					if (empty($money_rate)) $money_rate = $row['rate'];
					return ['typeid' => $typeid, 'typename' => $typename, 'plugin' => $row['plugin'], 'channel' => $row['id'], 'subchannel' => 0, 'rate' => $money_rate, 'apptype' => $row['apptype'], 'mode' => $row['mode'], 'paymin' => $row['paymin'], 'paymax' => $row['paymax'], 'payfee_lessthan' => $row['payfee_lessthan'] ?? null, 'payfee_mincost' => $row['payfee_mincost'] ?? null];
				}
			} elseif ($channel == -5) { //首个可用通道
				$rows = Db::name('channel')->where('type', $typeid)->where('status', 1)->where('daystatus', 0)->field('id,plugin,status,rate,apptype,mode,paymin,paymax,timestart,timestop,rate_limit_seconds,rate_limit_count,payfee_lessthan,payfee_mincost')->order('id', 'asc')->select();
				if (count($rows) > 0) {
					$newrows = [];
					foreach ($rows as $row) {
						if ($money > 0 && !empty($row['paymin']) && $row['paymin'] > 0 && $money < $row['paymin']) continue;
						if ($money > 0 && !empty($row['paymax']) && $row['paymax'] > 0 && $money > $row['paymax']) continue;
						if (self::isRateLimited($row)) continue;
						if (!isNullOrEmpty($row['timestart']) && !isNullOrEmpty($row['timestop']) && ($row['timestart'] > 0 || $row['timestop'] > 0)) {
							$hour = date('H');
							if ($row['timestart'] < $row['timestop']) {
								if ($hour < $row['timestart'] || $hour > $row['timestop']) continue;
							} else {
								if ($hour < $row['timestart'] && $hour > $row['timestop']) continue;
							}
						}
						$newrows[] = $row;
					}
					if (count($newrows) == 0) return false;
					$row = $newrows[0];
					if (empty($money_rate)) $money_rate = $row['rate'];
					return ['typeid' => $typeid, 'typename' => $typename, 'plugin' => $row['plugin'], 'channel' => $row['id'], 'subchannel' => 0, 'rate' => $money_rate, 'apptype' => $row['apptype'], 'mode' => $row['mode'], 'paymin' => $row['paymin'], 'paymax' => $row['paymax'], 'payfee_lessthan' => $row['payfee_lessthan'] ?? null, 'payfee_mincost' => $row['payfee_mincost'] ?? null];
				}
			} elseif ($channel == -2) { //用户自定义子通道
				$roll_type = (int) Db::name('user')->where('uid', $uid)->value('roll_type');
				$query = Db::name('subchannel')->alias('B')->join('channel A', 'B.channel=A.id')->where('B.uid', $uid)->where('A.type', $typeid)->where('A.status', 1)->where('B.status', 1)->where('A.daystatus', 0)->field('A.id,plugin,A.status,rate,apptype,mode,A.paymin,A.paymax,B.paymin sub_paymin,B.paymax sub_paymax,B.id subid,B.weight,timestart,timestop,A.rate_limit_seconds,A.rate_limit_count,A.payfee_lessthan,A.payfee_mincost');
				if ($sub_mch_id > 0) $query = $query->where('B.apply_id', $sub_mch_id);
				if ($sub_channel_id > 0) $query = $query->where('B.id', $sub_channel_id);
				if ($roll_type == 1) $query = $query->order('B.usetime', 'asc');
				$rows = $query->order('B.id', 'asc')->select();
				if (count($rows) > 0) {
					$newrows = [];
					foreach ($rows as $row) {
						if ($money > 0 && !empty($row['paymin']) && $row['paymin'] > 0 && $money < $row['paymin']) continue;
						if ($money > 0 && !empty($row['paymax']) && $row['paymax'] > 0 && $money > $row['paymax']) continue;
						if ($money > 0 && !empty($row['sub_paymin']) && $row['sub_paymin'] > 0 && $money < $row['sub_paymin']) continue;
						if ($money > 0 && !empty($row['sub_paymax']) && $row['sub_paymax'] > 0 && $money > $row['sub_paymax']) continue;
						if ($sub_channel_id == 0 && self::isRateLimited($row)) continue;
						if (!isNullOrEmpty($row['timestart']) && !isNullOrEmpty($row['timestop']) && ($row['timestart'] > 0 || $row['timestop'] > 0)) {
							$hour = date('H');
							if ($row['timestart'] < $row['timestop']) {
								if ($hour < $row['timestart'] || $hour > $row['timestop']) continue;
							} else {
								if ($hour < $row['timestart'] && $hour > $row['timestop']) continue;
							}
						}
						$newrows[] = $row;
					}
					if (count($newrows) == 0) return false;

					if ($roll_type == 1) {
						// 按顺序依次轮询：取usetime最早的（已按usetime asc排序）
						$row = $newrows[0];
					} elseif ($roll_type == 2) {
						// 按权重随机轮询
						$weightArr = [];
						foreach ($newrows as $r) {
							$w = intval($r['weight']);
							if ($w <= 0) $w = 1;
							$weightArr[] = ['name' => $r, 'weight' => $w];
						}
						$row = self::random_weight($weightArr);
						if (!$row) $row = $newrows[0];
					} elseif ($roll_type == 3) {
						// 仅使用第一个已启用的
						$row = $newrows[0];
					} else {
						// 默认：随机可用通道
						$row = $newrows[array_rand($newrows)];
					}

					if (empty($money_rate)) $money_rate = $row['rate'];
					Db::name('subchannel')->where('id', $row['subid'])->update(['usetime' => (int)(microtime(true) * 1000)]);
					[$paymin, $paymax] = self::getEffectivePayLimit($row['paymin'], $row['paymax'], $row['sub_paymin'], $row['sub_paymax']);
					return ['typeid' => $typeid, 'typename' => $typename, 'plugin' => $row['plugin'], 'channel' => $row['id'], 'subchannel' => $row['subid'], 'rate' => $money_rate, 'apptype' => $row['apptype'], 'mode' => $row['mode'], 'paymin' => $paymin, 'paymax' => $paymax, 'rate_limit_seconds' => $sub_channel_id == 0 ? 0 : $row['rate_limit_seconds'], 'rate_limit_count' => $sub_channel_id == 0 ? 0 : $row['rate_limit_count'], 'payfee_lessthan' => $row['payfee_lessthan'] ?? null, 'payfee_mincost' => $row['payfee_mincost'] ?? null];
				}
			} elseif ($channel == -3) { //随机可用轮询组
				$rows = Db::name('roll')->where('type', $typeid)->where('status', 1)->select()->toArray();
				if (count($rows) > 0) {
					$row = $rows[array_rand($rows)];
					$is_roll = true;
					$channel = $row['id'];
					goto ROLL_START;
				}
				return false;
			} else {
				ROLL_START:
				if ($is_roll) { //解析轮询组
					$channel = self::getChannelFromRoll($channel, $money);
					if (!$channel || $channel == 0) { //当前轮询组未开启
						return false;
					}
				}
				//获取轮询组对应通道
				$row = Db::name('channel')->where('id', $channel)->field('plugin,status,rate,apptype,mode,paymin,paymax,timestart,timestop,rate_limit_seconds,rate_limit_count,payfee_lessthan,payfee_mincost')->find();
				if (!$row || $row['status'] == 0) return false;
				if (empty($money_rate)) $money_rate = $row['rate'];
				return ['typeid' => $typeid, 'typename' => $typename, 'plugin' => $row['plugin'], 'channel' => $channel, 'subchannel' => 0, 'rate' => $money_rate, 'apptype' => $row['apptype'], 'mode' => $row['mode'], 'paymin' => $row['paymin'], 'paymax' => $row['paymax'], 'timestart' => $row['timestart'], 'timestop' => $row['timestop'], 'rate_limit_seconds' => $is_roll ? 0 : $row['rate_limit_seconds'], 'rate_limit_count' => $is_roll ? 0 : $row['rate_limit_count'], 'payfee_lessthan' => $row['payfee_lessthan'] ?? null, 'payfee_mincost' => $row['payfee_mincost'] ?? null];
			}
		} else {
			//未设置用户组
			$row = Db::name('channel')->where('type', $typeid)->where('status', 1)->where('daystatus', 0)->field('id,plugin,status,rate,apptype,mode,paymin,paymax,timestart,timestop,rate_limit_seconds,rate_limit_count,payfee_lessthan,payfee_mincost')->orderRaw('rand()')->find();
			if ($row) {
				return ['typeid' => $typeid, 'typename' => $typename, 'plugin' => $row['plugin'], 'channel' => $row['id'], 'subchannel' => 0, 'rate' => $row['rate'], 'apptype' => $row['apptype'], 'mode' => $row['mode'], 'paymin' => $row['paymin'], 'paymax' => $row['paymax'], 'timestart' => $row['timestart'], 'timestop' => $row['timestop'], 'rate_limit_seconds' => $row['rate_limit_seconds'], 'rate_limit_count' => $row['rate_limit_count'], 'payfee_lessthan' => $row['payfee_lessthan'] ?? null, 'payfee_mincost' => $row['payfee_mincost'] ?? null];
			}
		}
		return false;
	}

	// 获取当前商户可用支付方式
	public static function getTypes($uid, $gid = 0)
	{
		if (checkmobile() == true) {
			$rows = Db::name('type')->where('status', 1)->whereRaw('(device=0 OR device=2)')->order('id', 'asc')->select();
		} else {
			$rows = Db::name('type')->where('status', 1)->whereRaw('(device=0 OR device=1)')->order('id', 'asc')->select();
		}
		$paytype = [];
		foreach ($rows as $row) {
			$paytype[$row['id']] = $row;
		}
		if ($gid > 0) $groupinfo = Db::name('group')->where('gid', $gid)->value('info');
		if (empty($groupinfo)) $groupinfo = Db::name('group')->where('gid', 0)->value('info');
		if ($groupinfo) {
			$info = json_decode($groupinfo, true);
			foreach ($info as $id => $row) {
				if (!isset($paytype[$id])) continue;
				if ($row['channel'] == 0) {
					unset($paytype[$id]);
				} elseif ($row['channel'] == -1 || $row['channel'] == -4 || $row['channel'] == -5) {
					$channel = Db::name('channel')->where('type', $id)->where('status', 1)->field('rate,status')->find();
					if (!$channel) {
						unset($paytype[$id]);
					} elseif (empty($row['rate'])) {
						$paytype[$id]['rate'] = $channel['rate'];
					} else {
						$paytype[$id]['rate'] = $row['rate'];
					}
				} elseif ($row['channel'] == -2) {
					$channel = Db::name('subchannel')->alias('B')->join('channel A', 'B.channel=A.id')->where('B.uid', $uid)->where('A.type', $id)->where('A.status', 1)->where('B.status', 1)->field('A.id,A.status,rate')->find();
					if (!$channel) {
						unset($paytype[$id]);
					} elseif (empty($row['rate'])) {
						$paytype[$id]['rate'] = $channel['rate'];
					} else {
						$paytype[$id]['rate'] = $row['rate'];
					}
				} elseif ($row['channel'] == -3) {
					$channel = Db::name('roll')->where('type', $id)->where('status', 1)->field('id,status')->find();
					if (!$channel) {
						unset($paytype[$id]);
					} else {
						$paytype[$id]['rate'] = $row['rate'] ?? null;
					}
				} else {
					if ($row['type'] == 'roll') {
						$status = Db::name('roll')->where('id', $row['channel'])->value('status');
					} else {
						$status = Db::name('channel')->where('id', $row['channel'])->value('status');
						if (empty($row['rate'])) $row['rate'] = Db::name('channel')->where('id', $row['channel'])->value('rate');
					}
					if (!$status || $status == 0) unset($paytype[$id]);
					else $paytype[$id]['rate'] = $row['rate'] ?? null;
				}
			}
		} else {
			foreach ($paytype as $id => $row) {
				$status = Db::name('channel')->where('type', $id)->where('status', 1)->value('status');
				if (!$status || $status == 0) unset($paytype[$id]);
				else {
					$paytype[$id]['rate'] = Db::name('channel')->where('type', $id)->where('status', 1)->value('rate');
				}
			}
		}
		return $paytype;
	}

	static public function getSubChannelIdsByDays(int $channelid, int $days = 15): array
	{
		$list = Db::name('subchannel')->where('channel', $channelid)->where('paytime', '>=', date('Y-m-d', strtotime('-' . $days . ' days')))->column('id');
		return $list;
	}

	//根据轮询组ID获取支付通道ID
	static private function getChannelFromRoll($channel, $money)
	{
		$row = Db::name('roll')->where('id', $channel)->find();
		if ($row['status'] == 1) {
			$info = self::rollinfo_decode($row['info'], true);

			//先根据支付金额与限额过滤可用支付通道
			$channelids = [];
			foreach ($info as $inforow) {
				$channelids[] = $inforow['name'];
			}
			$rows = Db::name('channel')->whereIn('id', $channelids)->where('status', 1)->where('daystatus', 0)->field('id,paymin,paymax,timestart,timestop,rate_limit_seconds,rate_limit_count')->orderRaw('FIELD(id, ' . implode(',', $channelids) . ')')->select();
			$newids = [];
			foreach ($rows as $channelrow) {
				if ($money > 0 && !empty($channelrow['paymin']) && $channelrow['paymin'] > 0 && $money < $channelrow['paymin']) continue;
				if ($money > 0 && !empty($channelrow['paymax']) && $channelrow['paymax'] > 0 && $money > $channelrow['paymax']) continue;
				if (self::isRateLimited($channelrow)) continue;
				if (!isNullOrEmpty($channelrow['timestart']) && !isNullOrEmpty($channelrow['timestop']) && ($channelrow['timestart'] > 0 || $channelrow['timestop'] > 0)) {
					$hour = date('H');
					if ($channelrow['timestart'] < $channelrow['timestop']) {
						if ($hour < $channelrow['timestart'] || $hour > $channelrow['timestop']) continue;
					} else {
						if ($hour < $channelrow['timestart'] && $hour > $channelrow['timestop']) continue;
					}
				}
				$newids[] = $channelrow['id'];
			}
			if (count($newids) == 0) return false;

			$newinfo = [];
			foreach ($info as $inforow) {
				if (in_array($inforow['name'], $newids)) $newinfo[] = $inforow;
			}

			if ($row['kind'] == 3) {
				$maxWeight = max(array_column($newinfo, 'weight'));
				$topids = [];
				foreach ($newinfo as $inforow) {
					if ($inforow['weight'] == $maxWeight) $topids[] = $inforow['name'];
				}
				return $topids[array_rand($topids)];
			} elseif ($row['kind'] == 2) {
				return $newids[0];
			} elseif ($row['kind'] == 1) {
				$channel = self::random_weight($newinfo);
			} else {
				$index = $row['index'] % count($newinfo);
				$channel = $newinfo[$index]['name'];
				$index = ($row['index'] + 1) % count($newinfo);
				Db::name('roll')->where('id', $row['id'])->update(['index' => $index]);
			}
			return $channel;
		}
		return false;
	}

	static private function isRateLimited($channel)
	{
		$rateLimitSeconds = (int) ($channel['rate_limit_seconds'] ?? 0);
		$rateLimitCount = (int) ($channel['rate_limit_count'] ?? 0);
		if ($rateLimitSeconds <= 0 || $rateLimitCount <= 0) return false;

		$sinceTime = date('Y-m-d H:i:s', time() - $rateLimitSeconds);
		$recentCount = Db::name('order')->where('channel', $channel['id'])->where('addtime', '>=', $sinceTime)->count();
		return $recentCount >= $rateLimitCount;
	}

	//解析轮询组info
	static private function rollinfo_decode($content)
	{
		$result = [];
		$arr = explode(',', $content);
		foreach ($arr as $row) {
			if (empty($row)) continue;
			$a = explode(':', $row);
			$result[] = ['name' => (int) ($a[0] ?? 0), 'weight' => (int) ($a[1] ?? 0)];
		}
		return $result;
	}

	//加权随机
	static private function random_weight($arr)
	{
		$weightSum = 0;
		foreach ($arr as $value) {
			$weightSum += intval($value['weight']);
		}
		if ($weightSum <= 0) return false;
		$randNum = mt_rand(1, $weightSum);
		foreach ($arr as $v) {
			if ($randNum <= $v['weight']) {
				return $v['name'];
			}
			$randNum -= $v['weight'];
		}
	}

	static private function getEffectivePayLimit($paymin, $paymax, $subPaymin, $subPaymax)
	{
		$minItems = [];
		if (!empty($paymin) && $paymin > 0) $minItems[] = $paymin;
		if (!empty($subPaymin) && $subPaymin > 0) $minItems[] = $subPaymin;

		$maxItems = [];
		if (!empty($paymax) && $paymax > 0) $maxItems[] = $paymax;
		if (!empty($subPaymax) && $subPaymax > 0) $maxItems[] = $subPaymax;

		$effectiveMin = '';
		foreach ($minItems as $item) {
			if ($effectiveMin === '' || (float) $item > (float) $effectiveMin) $effectiveMin = $item;
		}

		$effectiveMax = '';
		foreach ($maxItems as $item) {
			if ($effectiveMax === '' || (float) $item < (float) $effectiveMax) $effectiveMax = $item;
		}

		return [$effectiveMin, $effectiveMax];
	}

	static private function in_range($range, $money)
	{
		if (empty($range)) return true;
		$range = explode(',', $range);
		foreach ($range as $row) {
			if (strpos($row, '-') !== false) {
				$minmax = explode('-', $row);
				if ($money >= intval($minmax[0]) && $money <= intval($minmax[1])) {
					return true;
				}
			} else {
				if ($money == intval($row)) {
					return true;
				}
			}
		}
		return false;
	}
}
