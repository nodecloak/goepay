ALTER TABLE `pre_psorder`
ADD COLUMN `return_money` decimal(10,2) NOT NULL DEFAULT '0.00';

ALTER TABLE `pre_subchannel`
ADD COLUMN `paymin` varchar(10) DEFAULT NULL,
ADD COLUMN `paymax` varchar(10) DEFAULT NULL;

ALTER TABLE `pre_channel`
ADD COLUMN `paytime` datetime DEFAULT NULL;

ALTER TABLE `pre_subchannel`
ADD COLUMN `paytime` datetime DEFAULT NULL;

CREATE TABLE IF NOT EXISTS `pre_crontab` (
  `id` int(11) unsigned NOT NULL AUTO_INCREMENT,
  `task` varchar(64) NOT NULL,
  `name` varchar(100) NOT NULL,
  `description` varchar(255) NOT NULL DEFAULT '',
  `plugin` varchar(64) NOT NULL DEFAULT '',
  `frequency` enum('second','minute','hour','day','once') NOT NULL DEFAULT 'minute',
  `interval` int(10) unsigned NOT NULL DEFAULT '1',
  `execute_at` datetime DEFAULT NULL,
  `last_run_time` datetime DEFAULT NULL,
  `next_run_time` datetime DEFAULT NULL,
  `params` text DEFAULT NULL,
  `enabled` tinyint(1) NOT NULL DEFAULT '1',
  `last_status` tinyint(1) NOT NULL DEFAULT '0',
  `last_error` varchar(1000) DEFAULT NULL,
  `lock_token` varchar(64) DEFAULT NULL,
  `locked_until` datetime DEFAULT NULL,
  PRIMARY KEY (`id`),
  UNIQUE KEY `task` (`task`),
  KEY `enabled_next_run` (`enabled`,`next_run_time`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8 COMMENT='计划任务';

ALTER TABLE `pre_crontab`
CHANGE COLUMN `interval_seconds` `interval` int(10) unsigned NOT NULL DEFAULT '1';

INSERT IGNORE INTO `pre_crontab` (`task`,`name`,`description`,`frequency`,`interval`,`enabled`) VALUES
('order','系统每日数据维护','清理过期数据、重置通道状态、检测用户组到期等(请保持开启)','day',1,1),
('settle','自动生成结算列表','根据商户余额与结算配置自动生成待结算记录','hour',1,1),
('notify','订单异步通知重试','异步通知失败的订单重试发送异步通知','second',10,0),
('order_query','未支付订单查询补单','对未支付状态的订单调用一次查单接口，防止因未收到通知导致漏单','minute',1,0),
('profitsharing','订单分账与延迟结算','处理待分账订单及支付宝微信延迟结算','minute',1,0),
('check','支付风控检测','根据风控设置，检测支付通道、成功率、投诉等风险','minute',1,1),
('complain','交易投诉同步','从支付通道同步最新投诉记录','minute',1,1),
('applyrefresh','进件商户状态刷新','定时查询审核中的进件商户并刷新状态','minute',5,0),
('balance_notice','商户余额不足提醒','向余额低于预警值的商户发送提醒，需先在设置中开启','minute',1,0),
('transfer','商户余额自动转账','超过指定余额的自动执行支付宝转账，需先在设置中开启','minute',1,0),
('daily_report','收款日报发送','向已开启收款日报的商户发送前一日收款统计','hour',1,0);

CREATE TABLE IF NOT EXISTS `pre_complaintask` (
  `id` int(11) unsigned NOT NULL AUTO_INCREMENT,
  `channel` int(11) unsigned NOT NULL,
  `status` tinyint(1) NOT NULL DEFAULT '0',
  `addtime` datetime NOT NULL,
  `lasttime` datetime DEFAULT NULL,
  `laststatus` tinyint(1) NOT NULL DEFAULT '0',
  `nexttime` datetime DEFAULT NULL,
  `reason` varchar(1000) DEFAULT NULL,
  `days` int(10) unsigned NOT NULL DEFAULT '0',
  `frequency` int(10) unsigned NOT NULL DEFAULT '6',
  `num` int(10) unsigned NOT NULL DEFAULT '20',
  PRIMARY KEY (`id`),
  UNIQUE KEY `channel` (`channel`),
  KEY `status_nexttime` (`status`,`nexttime`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8 COMMENT='投诉同步任务';

ALTER TABLE `pre_order`
DROP COLUMN `domain2`,
DROP COLUMN `invite`,
DROP COLUMN `invitemoney`,
DROP COLUMN `profits2`;

ALTER TABLE `pre_order`
ADD COLUMN `sub_mchid` varchar(50) DEFAULT NULL;

ALTER TABLE `pre_applymerchant`
ADD COLUMN `refreshtime` datetime DEFAULT NULL;

ALTER TABLE `pre_channel`
ADD COLUMN `ordername` varchar(255) DEFAULT NULL,
ADD COLUMN `ordername_rules` text DEFAULT NULL;

ALTER TABLE `pre_channel`
ADD COLUMN `payfee_lessthan` varchar(10) DEFAULT NULL,
ADD COLUMN `payfee_mincost` varchar(10) DEFAULT NULL;

ALTER TABLE `pre_user`
ADD COLUMN `telegram_uid` varchar(32) DEFAULT NULL;

ALTER TABLE `pre_user`
ADD COLUMN `event_url` varchar(500) DEFAULT NULL,
ADD COLUMN `event_types` varchar(100) NOT NULL DEFAULT '';

CREATE TABLE IF NOT EXISTS `pre_onecode` (
  `id` int(11) unsigned NOT NULL AUTO_INCREMENT,
  `name` varchar(50) NOT NULL,
  `label` varchar(100) NOT NULL,
  `qr_width` int(11) NOT NULL DEFAULT '380',
  `qr_height` int(11) NOT NULL DEFAULT '380',
  `foreground` varchar(20) NOT NULL DEFAULT '#555555',
  `background` varchar(20) NOT NULL DEFAULT '#FFFFFF',
  `img_width` int(11) NOT NULL DEFAULT '900',
  `img_height` int(11) NOT NULL DEFAULT '1200',
  `font` varchar(100) NOT NULL DEFAULT '60px 黑体',
  `font_color` varchar(20) NOT NULL DEFAULT '#FFFFFF',
  `rec_name_left` varchar(20) DEFAULT NULL,
  `rec_name_top` int(11) NOT NULL DEFAULT '0',
  `word` tinyint(1) NOT NULL DEFAULT '0',
  `qr_left` int(11) NOT NULL DEFAULT '0',
  `qr_top` int(11) NOT NULL DEFAULT '0',
  `url` varchar(500) NOT NULL,
  `sort` int(11) NOT NULL DEFAULT '0',
  `status` tinyint(1) NOT NULL DEFAULT '1',
  PRIMARY KEY (`id`),
  UNIQUE KEY `name` (`name`),
  KEY `sort_status` (`sort`,`status`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8 COMMENT='聚合收款码样式';

INSERT IGNORE INTO `pre_onecode` (`id`,`name`,`label`,`qr_width`,`qr_height`,`foreground`,`background`,`img_width`,`img_height`,`font`,`font_color`,`rec_name_left`,`rec_name_top`,`qr_left`,`qr_top`,`url`,`sort`,`status`,`word`) VALUES
(1, 'default', '默认', 380, 380, '#555', '#FFFFFF', 862, 1132, '60px ''黑体''', '#FFFFFF', '', 210, 245, 326, '/static/files/qrcode.png', 0, 1, 2),
(2, 'dongxue', '冬雪', 364, 364, '#E3D6C6', '#FFFFFF', 900, 1200, '70px ''黑体''', '#FFFFFF', '', 178, 270, 340, 'https://img.alicdn.com/imgextra/i2/2027555802/O1CN0178X3hU1sjMWfmkoh5_!!2027555802.png', 1, 1, 2),
(3, 'pikaqiu', '皮卡丘', 316, 316, '#FBCE29', '#FFFFFF', 900, 1200, '70px ''黑体''', '#FFFFFF', '', 178, 290, 367, 'https://img.alicdn.com/imgextra/i3/2027555802/O1CN01ETL8eZ1sjMWf7GHlX_!!2027555802.png', 2, 1, 2),
(4, 'kanuobudingmao', '布叮猫', 316, 316, '#E1C6B1', '#FFFFFF', 900, 1200, '70px ''黑体''', '#E1C6B1', '', 178, 290, 367, 'https://img.alicdn.com/imgextra/i4/2027555802/O1CN01bFGxSs1sjMWcWW1uR_!!2027555802.png', 3, 1, 2),
(5, 'niannianyouyu', '年年有余', 370, 370, '#E62601', '#FFFFFF', 900, 1200, '60px ''黑体''', '#FFFFFF', '', 255, 270, 320, 'https://img.alicdn.com/imgextra/i1/2027555802/O1CN01T0g4kQ1sjMWf7HUZP_!!2027555802.png', 4, 1, 2),
(6, 'xiaohuangren', '小黄人', 520, 520, '#F7D25D', '#FFFFFF', 900, 1200, '70px ''黑体''', 'transparent', '', 178, 183, 98, 'https://img.alicdn.com/imgextra/i4/2027555802/O1CN019MNjbn1sjMWe1zPkK_!!2027555802.png', 5, 1, 0),
(7, 'qitao', '乞讨', 520, 520, '#212121', 'transparent', 900, 1200, '70px ''黑体''', 'transparent', '', 178, 93, 98, 'https://img.alicdn.com/imgextra/i4/2027555802/O1CN01vCBhEb1sjMWbLYdfv_!!2027555802.png', 6, 1, 0),
(8, 'baobei', '宝贝', 500, 500, '#A95B5E', 'transparent', 900, 1200, '70px ''黑体''', 'transparent', '', 178, 200, 370, 'https://img.alicdn.com/imgextra/i3/2027555802/O1CN01ABc7nR1sjMWbLWQRm_!!2027555802.png', 7, 1, 0),
(9, 'toushi', '投食', 400, 400, '#FDA3B8', 'transparent', 900, 1200, '70px ''黑体''', 'transparent', '', 178, 408, 605, 'https://img.alicdn.com/imgextra/i4/2027555802/O1CN01mO4EYP1sjMWeSZtzX_!!2027555802.png', 8, 1, 0),
(10, 'gongzhu', '公主', 520, 520, '#F36C79', 'transparent', 900, 1200, '70px ''黑体''', 'transparent', '', 178, 185, 370, 'https://img.alicdn.com/imgextra/i4/2027555802/O1CN01LDfMYs1sjMWhTHmPR_!!2027555802.png', 9, 1, 0),
(11, 'qiuzanzhu', '求赞助', 460, 460, '#F36C79', 'transparent', 900, 1200, '70px ''黑体''', 'transparent', '', 178, 220, 425, 'https://img.alicdn.com/imgextra/i2/2027555802/O1CN01EAhVdL1sjMWe1yXjJ_!!2027555802.png', 10, 1, 0),
(12, 'huanyingdashang', '欢迎打赏', 510, 510, '#F00', 'transparent', 900, 1200, '70px ''黑体''', 'transparent', '', 178, 195, 625, 'https://img.alicdn.com/imgextra/i4/2027555802/O1CN01Yuipi71sjMWhTI32u_!!2027555802.png', 11, 1, 0),
(13, 'yinlian', '银联', 330, 330, '#fff', 'transparent', 900, 1200, '70px ''黑体''', 'transparent', '', 178, 190, 180, 'https://img.alicdn.com/imgextra/i4/2027555802/O1CN01DU5ECy1sjMWe205L5_!!2027555802.png', 12, 1, 0),
(14, 'yitiji', '一体机', 380, 380, '#FB7C2F', 'transparent', 900, 1200, '70px ''黑体''', 'transparent', '', 178, 260, 190, 'https://img.alicdn.com/imgextra/i4/2027555802/O1CN01B4y11v1sjMWeSGuWy_!!2027555802.png', 13, 1, 0),
(15, 'maomi', '猫咪', 480, 480, '#FDD699', 'transparent', 900, 1200, '70px ''黑体''', 'transparent', '', 178, 210, 150, 'https://img.alicdn.com/imgextra/i4/2027555802/O1CN017OCNEy1sjMWfe9Z3U_!!2027555802.png', 14, 1, 0),
(16, 'longmao', '龙猫', 464, 464, '#7394B6', 'transparent', 900, 1200, '70px ''黑体''', 'transparent', '', 178, 218, 214, 'https://img.alicdn.com/imgextra/i4/2027555802/O1CN01Jb4sxE1sjMWWkB2GX_!!2027555802.png', 15, 1, 0),
(17, 'alipay', '支付宝', 707, 707, '#000', '#FFFFFF', 1170, 1755, '58px ''Arial''', '#3C3A3B', '577', 1475, 231, 655, '/static/files/qrcode_alipay.jpg', 16, 1, 1),
(18, 'wxpay', '微信支付', 486, 486, '#000', '#FFFFFF', 1118, 1524, '53px ''Arial''', '#191919', '555', 968, 315, 382, '/static/files/qrcode_wxpay.png', 17, 1, 1);

CREATE TABLE IF NOT EXISTS `pre_psinternal` (
  `id` int(11) unsigned NOT NULL AUTO_INCREMENT,
  `uid` int(11) unsigned NOT NULL,
  `type` int(11) NOT NULL DEFAULT '0',
  `info` varchar(2048) NOT NULL DEFAULT '',
  `status` tinyint(1) NOT NULL DEFAULT '1',
  `addtime` datetime DEFAULT NULL,
  PRIMARY KEY (`id`),
  UNIQUE KEY `uid_type` (`uid`,`type`),
  KEY `status` (`status`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8 COMMENT='站内分账规则';

CREATE TABLE IF NOT EXISTS `pre_internal_transfer` (
  `id` int(11) unsigned NOT NULL AUTO_INCREMENT,
  `uid` int(11) unsigned NOT NULL COMMENT '发起方商户ID',
  `touid` int(11) unsigned NOT NULL COMMENT '收款方商户ID',
  `money` decimal(10,2) NOT NULL,
  `status` tinyint(1) NOT NULL DEFAULT '0' COMMENT '0待审核 1成功 2已拒绝',
  `admin` tinyint(1) NOT NULL DEFAULT '0' COMMENT '1管理员发起',
  `reason` varchar(255) DEFAULT NULL COMMENT '拒绝原因',
  `addtime` datetime DEFAULT NULL,
  `endtime` datetime DEFAULT NULL,
  PRIMARY KEY (`id`),
  KEY `uid` (`uid`),
  KEY `touid` (`touid`),
  KEY `status` (`status`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8 COMMENT='站内转账';

ALTER TABLE `pre_complaintask`
ADD COLUMN `num` int(10) unsigned NOT NULL DEFAULT '20';