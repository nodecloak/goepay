DROP TABLE IF EXISTS `pre_config`;
create table `pre_config` (
`k` varchar(32) NOT NULL,
`v` text NULL,
`cache` tinyint(1) NOT NULL DEFAULT '1',
PRIMARY KEY  (`k`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8;

INSERT INTO `pre_config` VALUES ('version', '2070', 1);
INSERT INTO `pre_config` VALUES ('cron_mode', 'url', 1);
INSERT INTO `pre_config` VALUES ('async_mode', '0', 1);
INSERT INTO `pre_config` VALUES ('template_home', 'default', 1);
INSERT INTO `pre_config` VALUES ('template_pay', 'default', 1);
INSERT INTO `pre_config` VALUES ('template_qrpay', 'default', 1);
INSERT INTO `pre_config` VALUES ('sitename', '聚合支付平台', 1);
INSERT INTO `pre_config` VALUES ('title', '聚合支付 - 行业领先的免签约支付平台', 1);
INSERT INTO `pre_config` VALUES ('keywords', '聚合支付,支付宝免签约即时到账,财付通免签约,微信免签约支付,QQ钱包免签约,免签约支付', 1);
INSERT INTO `pre_config` VALUES ('description', '聚合支付是XX公司旗下的免签约支付产品，完美解决支付难题，一站式接入支付宝，微信，财付通，QQ钱包,微信wap，帮助开发者快速集成到自己相应产品，效率高，见效快，费率低！', 1);
INSERT INTO `pre_config` VALUES ('orgname', 'XX公司', 1);
INSERT INTO `pre_config` VALUES ('kfqq', '123456789', 1);
INSERT INTO `pre_config` VALUES ('template', 'default', 1);
INSERT INTO `pre_config` VALUES ('blockname', '', 1);
INSERT INTO `pre_config` VALUES ('blockalert', '温馨提醒该商品禁止出售，如有疑问请联系网站客服！', 1);
INSERT INTO `pre_config` VALUES ('settle_open', '1', 1);
INSERT INTO `pre_config` VALUES ('settle_type', '1', 1);
INSERT INTO `pre_config` VALUES ('settle_money', '30', 1);
INSERT INTO `pre_config` VALUES ('settle_rate', '0.5', 1);
INSERT INTO `pre_config` VALUES ('settle_fee_min', '0.1', 1);
INSERT INTO `pre_config` VALUES ('settle_fee_max', '20', 1);
INSERT INTO `pre_config` VALUES ('settle_alipay', '1', 1);
INSERT INTO `pre_config` VALUES ('settle_wxpay', '1', 1);
INSERT INTO `pre_config` VALUES ('settle_qqpay', '1', 1);
INSERT INTO `pre_config` VALUES ('settle_bank', '0', 1);
INSERT INTO `pre_config` VALUES ('transfer_alipay', '0', 1);
INSERT INTO `pre_config` VALUES ('transfer_wxpay', '0', 1);
INSERT INTO `pre_config` VALUES ('transfer_qqpay', '0', 1);
INSERT INTO `pre_config` VALUES ('transfer_name', '聚合支付平台', 1);
INSERT INTO `pre_config` VALUES ('transfer_desc', '聚合支付平台自动结算', 1);
INSERT INTO `pre_config` VALUES ('login_qq', '0', 1);
INSERT INTO `pre_config` VALUES ('login_telegram', '0', 1);
INSERT INTO `pre_config` VALUES ('login_alipay', '0', 1);
INSERT INTO `pre_config` VALUES ('login_alipay_channel', '0', 1);
INSERT INTO `pre_config` VALUES ('login_wx', '0', 1);
INSERT INTO `pre_config` VALUES ('login_wx_channel', '0', 1);
INSERT INTO `pre_config` VALUES ('reg_open', '1', 1);
INSERT INTO `pre_config` VALUES ('reg_pay', '0', 1);
INSERT INTO `pre_config` VALUES ('reg_pay_uid', '1000', 1);
INSERT INTO `pre_config` VALUES ('verifytype', '1', 1);
INSERT INTO `pre_config` VALUES ('test_open', '1', 1);
INSERT INTO `pre_config` VALUES ('test_pay_uid', '1000', 1);
INSERT INTO `pre_config` VALUES ('mail_api', '', 1);
INSERT INTO `pre_config` VALUES ('sms_api', '', 1);
INSERT INTO `pre_config` VALUES ('captcha_open', '1', 1);
INSERT INTO `pre_config` VALUES ('captcha_id', '', 1);
INSERT INTO `pre_config` VALUES ('captcha_key', '', 1);
INSERT INTO `pre_config` VALUES ('onecode', '1', 1);
INSERT INTO `pre_config` VALUES ('scanpay_open', '1', 1);
INSERT INTO `pre_config` VALUES ('recharge', '1', 1);
INSERT INTO `pre_config` VALUES ('internal_transfer', '0', 1);
INSERT INTO `pre_config` VALUES ('pageordername', '1', 1);
INSERT INTO `pre_config` VALUES ('notifyordername', '1', 1);
INSERT INTO `pre_config` VALUES ('user_refund', '1', 1);
INSERT INTO `pre_config` VALUES ('cdnpublic', '0', 1);
INSERT INTO `pre_config` VALUES ('ip_type', '2', 1);
INSERT INTO `pre_config` VALUES ('applyments_open', '1', 1);
INSERT INTO `pre_config` VALUES ('mchrisk_open', '1', 1);

DROP TABLE IF EXISTS `pre_crontab`;
CREATE TABLE `pre_crontab` (
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

INSERT INTO `pre_crontab` (`task`,`name`,`description`,`frequency`,`interval`,`enabled`) VALUES
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

DROP TABLE IF EXISTS `pre_anounce`;
create table `pre_anounce` (
  `id` int(11) unsigned NOT NULL auto_increment,
  `content` text DEFAULT NULL,
  `color` varchar(10) DEFAULT NULL,
  `sort` int(11) NOT NULL DEFAULT '1',
  `addtime` datetime DEFAULT NULL,
  `status` tinyint(1) NOT NULL DEFAULT '1',
PRIMARY KEY  (`id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8;

DROP TABLE IF EXISTS `pre_type`;
CREATE TABLE `pre_type` (
  `id` int(11) unsigned NOT NULL auto_increment,
  `name` varchar(30) NOT NULL,
  `device` int(1) unsigned NOT NULL DEFAULT '0',
  `showname` varchar(30) NOT NULL,
  `status` tinyint(1) NOT NULL DEFAULT '0',
 PRIMARY KEY (`id`),
 KEY `name` (`name`,`device`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8;

INSERT INTO `pre_type` VALUES (1, 'alipay', 0, '支付宝', 1);
INSERT INTO `pre_type` VALUES (2, 'wxpay', 0, '微信支付', 1);
INSERT INTO `pre_type` VALUES (3, 'qqpay', 0, 'QQ钱包', 0);
INSERT INTO `pre_type` VALUES (4, 'bank', 0, '银联支付', 0);
INSERT INTO `pre_type` VALUES (5, 'jdpay', 0, '京东支付', 0);
INSERT INTO `pre_type` VALUES (6, 'paypal', 0, 'PayPal', 0);
INSERT INTO `pre_type` VALUES (7, 'douyinpay', 0, '抖音支付', 0);

DROP TABLE IF EXISTS `pre_plugin`;
CREATE TABLE `pre_plugin` (
  `id` varchar(40) NOT NULL,
  `name` varchar(20) NOT NULL,
  `type` varchar(20) NOT NULL,
  `title` varchar(60) NOT NULL,
  `desc` varchar(500) DEFAULT NULL,
  `version` varchar(10) DEFAULT NULL,
  `author` varchar(60) DEFAULT NULL,
  `link` varchar(255) DEFAULT NULL,
  `icon` varchar(255) DEFAULT NULL,
  `status` tinyint(1) NOT NULL DEFAULT '0',
  `pay_depends` varchar(100) DEFAULT NULL,
  `pay_types` varchar(100) DEFAULT NULL,
  `trans_types` varchar(100) DEFAULT NULL,
  `ext` text DEFAULT NULL,
 PRIMARY KEY (`id`),
 UNIQUE KEY `name_type` (`name`,`type`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8;

DROP TABLE IF EXISTS `pre_channel`;
CREATE TABLE `pre_channel` (
  `id` int(11) unsigned NOT NULL auto_increment,
  `mode` tinyint(1) DEFAULT 0,
  `type` int(11) unsigned NOT NULL,
  `plugin` varchar(30) NOT NULL,
  `name` varchar(30) NOT NULL,
  `rate` decimal(5,2) NOT NULL DEFAULT '100.00',
  `status` tinyint(1) NOT NULL DEFAULT '0',
  `apptype` varchar(50) DEFAULT NULL,
  `daytop` int(10) DEFAULT 0,
  `daystatus` tinyint(1) DEFAULT 0,
  `paymin` varchar(10) DEFAULT NULL,
  `paymax` varchar(10) DEFAULT NULL,
  `appwxmp` int(11) DEFAULT NULL,
  `appwxa` int(11) DEFAULT NULL,
  `costrate` decimal(5,2) DEFAULT NULL,
  `config` text DEFAULT NULL,
  `daymaxorder` int(10) DEFAULT 0,
  `timestart` int(11) DEFAULT NULL,
  `timestop` int(11) DEFAULT NULL,
  `usetime` bigint(13) NOT NULL DEFAULT '0',
  `paytime` datetime DEFAULT NULL,
  `rate_limit_seconds` int(10) NOT NULL DEFAULT '0',
  `rate_limit_count` int(10) NOT NULL DEFAULT '0',
  `ordername` varchar(255) DEFAULT NULL,
  `ordername_rules` text DEFAULT NULL,
  `payfee_lessthan` varchar(10) DEFAULT NULL,
  `payfee_mincost` varchar(10) DEFAULT NULL,
 PRIMARY KEY (`id`),
 KEY `type` (`type`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8;

DROP TABLE IF EXISTS `pre_roll`;
CREATE TABLE `pre_roll` (
  `id` int(11) unsigned NOT NULL auto_increment,
  `type` int(11) unsigned NOT NULL,
  `name` varchar(30) NOT NULL,
  `kind` int(1) unsigned NOT NULL DEFAULT '0',
  `info` text DEFAULT NULL,
  `status` tinyint(1) NOT NULL DEFAULT '0',
  `index` int(11) NOT NULL DEFAULT '0',
 PRIMARY KEY (`id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8 AUTO_INCREMENT=101;

DROP TABLE IF EXISTS `pre_weixin`;
CREATE TABLE `pre_weixin` (
  `id` int(11) unsigned NOT NULL auto_increment,
  `type` tinyint(4) unsigned NOT NULL DEFAULT '0',
  `name` varchar(30) NOT NULL,
  `status` tinyint(1) NOT NULL DEFAULT '0',
  `appid` varchar(150) DEFAULT NULL,
  `appsecret` varchar(250) DEFAULT NULL,
  `access_token` varchar(300) DEFAULT NULL,
  `addtime` datetime DEFAULT NULL,
  `updatetime` datetime DEFAULT NULL,
  `expiretime` datetime DEFAULT NULL,
 PRIMARY KEY (`id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8;

DROP TABLE IF EXISTS `pre_order`;
CREATE TABLE `pre_order` (
  `trade_no` char(19) NOT NULL,
  `out_trade_no` varchar(150) NOT NULL,
  `api_trade_no` varchar(150) DEFAULT NULL,
  `uid` int(11) unsigned NOT NULL,
  `tid` tinyint(11) unsigned NOT NULL DEFAULT '0',
  `type` int(10) unsigned NOT NULL,
  `channel` int(10) unsigned NOT NULL,
  `name` varchar(64) NOT NULL,
  `money` decimal(10,2) NOT NULL,
  `realmoney` decimal(10,2) DEFAULT NULL,
  `getmoney` decimal(10,2) DEFAULT NULL,
  `profitmoney` decimal(10,2) DEFAULT NULL,
  `refundmoney` decimal(10,2) DEFAULT NULL,
  `notify_url` varchar(255) DEFAULT NULL,
  `return_url` varchar(255) DEFAULT NULL,
  `param` varchar(255) DEFAULT NULL,
  `addtime` datetime NOT NULL,
  `endtime` datetime DEFAULT NULL,
  `date` date DEFAULT NULL,
  `domain` varchar(64) DEFAULT NULL,
  `ip` varchar(50) DEFAULT NULL,
  `buyer` varchar(100) DEFAULT NULL,
  `status` tinyint(1) NOT NULL DEFAULT '0',
  `notify` int(5) NOT NULL DEFAULT '0',
  `notifytime` datetime DEFAULT NULL,
  `combine` tinyint(1) NOT NULL DEFAULT '0',
  `profits` int(11) NOT NULL DEFAULT '0',
  `settle` tinyint(1) NOT NULL DEFAULT '0',
  `subchannel` int(11) NOT NULL DEFAULT '0',
  `payurl` varchar(500) DEFAULT NULL,
  `ext` text DEFAULT NULL,
  `version` tinyint(1) NOT NULL DEFAULT '0',
  `bill_trade_no` varchar(150) DEFAULT NULL,
  `bill_mch_trade_no` varchar(150) DEFAULT NULL,
  `mobile` varchar(100) DEFAULT NULL,
  `cert_info` varchar(300) DEFAULT NULL,
  `province` varchar(2) DEFAULT NULL,
  `sub_mchid` varchar(50) DEFAULT NULL,
 PRIMARY KEY (`trade_no`),
 KEY `uid` (`uid`),
 KEY `channel` (`channel`),
 KEY `out_trade_no` (`out_trade_no`,`uid`),
 KEY `api_trade_no` (`api_trade_no`),
 KEY `date` (`date`),
 KEY `addtime` (`addtime`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8;

DROP TABLE IF EXISTS `pre_group`;
CREATE TABLE `pre_group` (
  `gid` int(11) unsigned NOT NULL auto_increment,
  `name` varchar(30) NOT NULL,
  `info` varchar(1024) DEFAULT NULL,
  `isbuy` tinyint(1) NOT NULL DEFAULT 0,
  `price` decimal(10,2) DEFAULT NULL,
  `sort` int(10) NOT NULL DEFAULT 0,
  `expire` int(10) NOT NULL DEFAULT 0,
  `config` text DEFAULT NULL,
  `settings` text DEFAULT NULL,
  `visible` varchar(30) DEFAULT NULL,
  `index` int(11) NOT NULL DEFAULT 0,
  `desc` varchar(500) DEFAULT NULL,
 PRIMARY KEY (`gid`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8;

INSERT INTO `pre_group` (`gid`, `name`, `info`) VALUES
(0, '默认用户组', '{"1":{"type":"","channel":"-1","rate":""},"2":{"type":"","channel":"-1","rate":""},"3":{"type":"","channel":"-1","rate":""}}');
UPDATE `pre_group` SET `gid` = '0' WHERE `gid` = 1;

DROP TABLE IF EXISTS `pre_user`;
CREATE TABLE `pre_user` (
  `uid` int(11) unsigned NOT NULL auto_increment,
  `gid` int(11) unsigned NOT NULL DEFAULT 0,
  `upid` int(11) unsigned NOT NULL DEFAULT 0,
  `key` varchar(32) NOT NULL,
  `pwd` varchar(32) DEFAULT NULL,
  `account` varchar(128) DEFAULT NULL,
  `username` varchar(128) DEFAULT NULL,
  `codename` varchar(32) DEFAULT NULL,
  `settle_id` tinyint(4) NOT NULL DEFAULT '1',
  `alipay_uid` varchar(128) DEFAULT NULL,
  `qq_uid` varchar(32) DEFAULT NULL,
  `wx_uid` varchar(32) DEFAULT NULL,
  `wxa_uid` varchar(32) DEFAULT NULL,
  `telegram_uid` varchar(32) DEFAULT NULL,
  `money` decimal(10,2) NOT NULL,
  `email` varchar(32) DEFAULT NULL,
  `phone` varchar(20) DEFAULT NULL,
  `qq` varchar(20) DEFAULT NULL,
  `url` varchar(64) DEFAULT NULL,
  `cert` tinyint(4) NOT NULL DEFAULT '0',
  `certtype` tinyint(4) NOT NULL DEFAULT '0',
  `certmethod` tinyint(4) NOT NULL DEFAULT '0',
  `certno` varchar(18) DEFAULT NULL,
  `certname` varchar(32) DEFAULT NULL,
  `certtime` datetime DEFAULT NULL,
  `certtoken` varchar(64) DEFAULT NULL,
  `certcorpno` varchar(30) DEFAULT NULL,
  `certcorpname` varchar(80) DEFAULT NULL,
  `addtime` datetime DEFAULT NULL,
  `lasttime` datetime DEFAULT NULL,
  `endtime` datetime DEFAULT NULL,
  `level` tinyint(1) NOT NULL DEFAULT '1',
  `pay` tinyint(1) NOT NULL DEFAULT '1',
  `settle` tinyint(1) NOT NULL DEFAULT '1',
  `keylogin` tinyint(1) NOT NULL DEFAULT '1',
  `apply` tinyint(1) NOT NULL DEFAULT '0',
  `mode` tinyint(4) NOT NULL DEFAULT '0',
  `mode_feerate` varchar(10) DEFAULT NULL,
  `status` tinyint(4) NOT NULL DEFAULT '0',
  `refund` tinyint(1) NOT NULL DEFAULT '1',
  `transfer` tinyint(1) NOT NULL DEFAULT '0',
  `keytype` tinyint(1) NOT NULL DEFAULT '0',
  `publickey` varchar(500) DEFAULT NULL,
  `event_url` varchar(500) DEFAULT NULL,
  `event_types` varchar(100) NOT NULL DEFAULT '',
  `channelinfo` text DEFAULT NULL,
  `ordername` varchar(255) DEFAULT NULL,
  `ordername_rules` text DEFAULT NULL,
  `msgconfig` text DEFAULT NULL,
  `remain_money` varchar(20) DEFAULT NULL,
  `open_code` tinyint(1) NOT NULL DEFAULT '0',
  `deposit` decimal(10,2) DEFAULT NULL,
  `voice_order` tinyint(1) NOT NULL DEFAULT '0',
  `voice_devid` varchar(30) DEFAULT NULL,
  `print_order` tinyint(1) NOT NULL DEFAULT '0',
  `print_config` varchar(300) DEFAULT NULL,
  `pay_maxmoney` varchar(10) DEFAULT NULL,
  `pay_minmoney` varchar(10) DEFAULT NULL,
  `totp` tinyint(1) NOT NULL DEFAULT '0',
  `totp_secret` varchar(255) DEFAULT NULL,
  `roll_type` tinyint(1) NOT NULL DEFAULT '0',
 PRIMARY KEY (`uid`),
 KEY `email` (`email`),
 KEY `phone` (`phone`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8 AUTO_INCREMENT=1000;

DROP TABLE IF EXISTS `pre_settle`;
CREATE TABLE `pre_settle` (
  `id` int(11) NOT NULL auto_increment,
  `uid` int(11) NOT NULL,
  `batch` varchar(20) DEFAULT NULL,
  `auto` tinyint(1) NOT NULL DEFAULT '1',
  `type` tinyint(1) NOT NULL DEFAULT '1',
  `account` varchar(128) NOT NULL,
  `username` varchar(128) NOT NULL,
  `money` decimal(10,2) NOT NULL,
  `realmoney` decimal(10,2) NOT NULL,
  `addtime` datetime DEFAULT NULL,
  `endtime` datetime DEFAULT NULL,
  `status` tinyint(1) NOT NULL DEFAULT '0',
  `transfer_no` varchar(64) DEFAULT NULL,
  `transfer_channel` int(10) unsigned DEFAULT NULL,
  `transfer_status` tinyint(1) NOT NULL DEFAULT '0',
  `transfer_result` varchar(64) DEFAULT NULL,
  `transfer_date` datetime DEFAULT NULL,
  `transfer_ext` text DEFAULT NULL,
  `result` varchar(64) DEFAULT NULL,
 PRIMARY KEY (`id`),
 KEY `uid` (`uid`),
 KEY `batch` (`batch`),
 KEY `transfer_no` (`transfer_no`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8;

DROP TABLE IF EXISTS `pre_log`;
CREATE TABLE `pre_log` (
  `id` int(11) NOT NULL AUTO_INCREMENT,
  `uid` int(11) NOT NULL DEFAULT '0',
  `type` varchar(20) NULL,
  `date` datetime NOT NULL,
  `ip` varchar(50) DEFAULT NULL,
  `city` varchar(20) DEFAULT NULL,
  `data` text NULL,
  PRIMARY KEY (`id`),
  KEY `logincheck` (`ip`,`date`,`uid`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8;

DROP TABLE IF EXISTS `pre_record`;
CREATE TABLE `pre_record` (
  `id` int(11) NOT NULL AUTO_INCREMENT,
  `uid` int(11) NOT NULL,
  `action` tinyint(1) NOT NULL DEFAULT '0',
  `money` decimal(10,2) NOT NULL,
  `oldmoney` decimal(10,2) NOT NULL,
  `newmoney` decimal(10,2) NOT NULL,
  `type` varchar(20) DEFAULT NULL,
  `trade_no` varchar(64) DEFAULT NULL,
  `date` datetime NOT NULL,
  PRIMARY KEY (`id`),
  KEY `uid` (`uid`),
  KEY `trade_no` (`trade_no`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8;

DROP TABLE IF EXISTS `pre_batch`;
CREATE TABLE `pre_batch` (
  `batch` varchar(20) NOT NULL,
  `allmoney` decimal(10,2) NOT NULL,
  `count` int(11) NOT NULL DEFAULT '0',
  `time` datetime DEFAULT NULL,
  `status` tinyint(1) NOT NULL DEFAULT '0',
 PRIMARY KEY (`batch`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8;

DROP TABLE IF EXISTS `pre_regcode`;
CREATE TABLE `pre_regcode` (
  `id` int(11) NOT NULL AUTO_INCREMENT,
  `uid` int(11) NOT NULL DEFAULT '0',
  `scene` varchar(20) NOT NULL DEFAULT '',
  `type` tinyint(1) NOT NULL DEFAULT '0',
  `code` varchar(32) NOT NULL,
  `to` varchar(32) DEFAULT NULL,
  `time` int(11) NOT NULL,
  `ip` varchar(50) DEFAULT NULL,
  `status` tinyint(1) NOT NULL DEFAULT '0',
  `errcount` int(11) NOT NULL DEFAULT '0',
  PRIMARY KEY (`id`),
  KEY `code` (`to`,`type`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8;

DROP TABLE IF EXISTS `pre_risk`;
CREATE TABLE `pre_risk` (
  `id` int(11) NOT NULL AUTO_INCREMENT,
  `uid` int(11) NOT NULL DEFAULT '0',
  `type` tinyint(1) NOT NULL DEFAULT '0',
  `url` varchar(64) DEFAULT NULL,
  `content` varchar(64) DEFAULT NULL,
  `date` datetime DEFAULT NULL,
  `status` tinyint(1) NOT NULL DEFAULT '0',
  PRIMARY KEY (`id`),
  KEY `uid` (`uid`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8;

DROP TABLE IF EXISTS `pre_domain`;
CREATE TABLE `pre_domain` (
  `id` int(11) unsigned NOT NULL auto_increment,
  `uid` int(11) NOT NULL DEFAULT '0',
  `domain` varchar(128) NOT NULL,
  `status` tinyint(1) NOT NULL DEFAULT '0',
  `addtime` datetime DEFAULT NULL,
  `endtime` datetime DEFAULT NULL,
 PRIMARY KEY (`id`),
 KEY `domain` (`domain`,`uid`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8;

DROP TABLE IF EXISTS `pre_psreceiver`;
CREATE TABLE `pre_psreceiver` (
  `id` int(11) unsigned NOT NULL auto_increment,
  `channel` int(11) NOT NULL,
  `subchannel` int(11) DEFAULT NULL,
  `mode` tinyint(1) NOT NULL DEFAULT '0',
  `uid` int(11) DEFAULT NULL,
  `account` varchar(128) NOT NULL,
  `name` varchar(50) DEFAULT NULL,
  `rate` varchar(10) DEFAULT NULL,
  `minmoney` varchar(10) DEFAULT NULL,
  `status` tinyint(1) NOT NULL DEFAULT '0',
  `addtime` datetime DEFAULT NULL,
  `info` varchar(1024) DEFAULT NULL,
 PRIMARY KEY (`id`),
 KEY `channel` (`channel`,`uid`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8;

DROP TABLE IF EXISTS `pre_psinternal`;
CREATE TABLE `pre_psinternal` (
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

DROP TABLE IF EXISTS `pre_internal_transfer`;
CREATE TABLE `pre_internal_transfer` (
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

DROP TABLE IF EXISTS `pre_psorder`;
CREATE TABLE `pre_psorder` (
  `id` int(11) unsigned NOT NULL auto_increment,
  `rid` int(11) NOT NULL,
  `trade_no` char(19) NOT NULL,
  `api_trade_no` varchar(150) NOT NULL,
  `sub_trade_no` varchar(25) DEFAULT NULL,
  `settle_no` varchar(150) DEFAULT NULL,
  `money` decimal(10,2) NOT NULL,
  `return_money` decimal(10,2) NOT NULL DEFAULT '0.00',
  `status` tinyint(1) NOT NULL DEFAULT '0',
  `result` text DEFAULT NULL,
  `addtime` datetime DEFAULT NULL,
  `delay` tinyint(1) NOT NULL DEFAULT '0',
  `retry` tinyint(1) NOT NULL DEFAULT '0',
  `rdata` text DEFAULT NULL,
 PRIMARY KEY (`id`),
 KEY `trade_no` (`trade_no`),
 KEY `addtime` (`addtime`,`delay`,`status`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8;

DROP TABLE IF EXISTS `pre_subchannel`;
CREATE TABLE `pre_subchannel` (
  `id` int(11) unsigned NOT NULL auto_increment,
  `channel` int(11) NOT NULL,
  `uid` int(11) NOT NULL,
  `name` varchar(30) NOT NULL,
  `status` tinyint(1) NOT NULL DEFAULT '0',
  `info` text DEFAULT NULL,
  `addtime` datetime DEFAULT NULL,
  `usetime` bigint(13) NOT NULL DEFAULT '0',
  `paytime` datetime DEFAULT NULL,
  `apply_id` int(11) DEFAULT NULL,
  `weight` int(11) NOT NULL DEFAULT '1',
  `paymin` varchar(10) DEFAULT NULL,
  `paymax` varchar(10) DEFAULT NULL,
 PRIMARY KEY (`id`),
 KEY `channel` (`channel`),
 KEY `uid` (`uid`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8;

DROP TABLE IF EXISTS `pre_blacklist`;
CREATE TABLE `pre_blacklist` (
  `id` int(11) unsigned NOT NULL auto_increment,
  `type` tinyint(1) NOT NULL DEFAULT '0',
  `content` varchar(50) NOT NULL,
  `addtime` datetime NOT NULL,
  `endtime` datetime DEFAULT NULL,
  `remark` varchar(80) DEFAULT NULL,
 PRIMARY KEY (`id`),
 UNIQUE KEY `content`(`content`,`type`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8;

DROP TABLE IF EXISTS `pre_transfer`;
CREATE TABLE `pre_transfer` (
  `biz_no` char(19) NOT NULL,
  `out_biz_no` varchar(150) NOT NULL DEFAULT '',
  `pay_order_no` varchar(80) DEFAULT NULL,
  `uid` int(11) NOT NULL,
  `type` varchar(10) NOT NULL,
  `channel` int(10) unsigned NOT NULL,
  `account` varchar(128) NOT NULL,
  `username` varchar(128) DEFAULT NULL,
  `money` decimal(10,2) NOT NULL,
  `costmoney` decimal(10,2) DEFAULT NULL,
  `addtime` datetime DEFAULT NULL,
  `paytime` datetime DEFAULT NULL,
  `status` tinyint(1) NOT NULL DEFAULT '0',
  `api` tinyint(1) NOT NULL DEFAULT '0',
  `desc` varchar(80) DEFAULT NULL,
  `result` varchar(80) DEFAULT NULL,
  `ext` text DEFAULT NULL,
 PRIMARY KEY (`biz_no`),
 KEY `uid` (`uid`),
 KEY `out_biz_no` (`out_biz_no`,`uid`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8;

DROP TABLE IF EXISTS `pre_invitecode`;
CREATE TABLE `pre_invitecode` (
  `id` int(11) unsigned NOT NULL auto_increment,
  `code` varchar(40) NOT NULL,
  `addtime` datetime NOT NULL,
  `usetime` datetime DEFAULT NULL,
  `uid` int(11) DEFAULT NULL,
  `status` tinyint(1) NOT NULL DEFAULT 0,
 PRIMARY KEY (`id`),
 KEY `code` (`code`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8;

DROP TABLE IF EXISTS `pre_refundorder`;
CREATE TABLE `pre_refundorder` (
  `refund_no` char(19) NOT NULL,
  `out_refund_no` varchar(150) NOT NULL,
  `trade_no` char(19) NOT NULL,
  `uid` int(11) NOT NULL DEFAULT '0',
  `money` decimal(10,2) NOT NULL,
  `reducemoney` decimal(10,2) DEFAULT NULL,
  `status` tinyint(1) NOT NULL DEFAULT '0',
  `addtime` datetime DEFAULT NULL,
  `endtime` datetime DEFAULT NULL,
 PRIMARY KEY (`refund_no`),
 KEY `out_refund_no` (`out_refund_no`,`uid`),
 KEY `trade_no` (`trade_no`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8;

DROP TABLE IF EXISTS `pre_suborder`;
CREATE TABLE `pre_suborder` (
  `sub_trade_no` varchar(25) NOT NULL,
  `trade_no` char(19) NOT NULL,
  `api_trade_no` varchar(150) DEFAULT NULL,
  `money` decimal(10,2) NOT NULL,
  `refundmoney` decimal(10,2) DEFAULT NULL,
  `status` tinyint(1) NOT NULL DEFAULT '0',
  `settle` tinyint(1) NOT NULL DEFAULT '0',
 PRIMARY KEY (`sub_trade_no`),
 KEY `trade_no` (`trade_no`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8;

DROP TABLE IF EXISTS `pre_complain`;
CREATE TABLE `pre_complain` (
  `id` int(11) unsigned NOT NULL auto_increment,
  `paytype` int(11) NOT NULL,
  `channel` int(11) NOT NULL,
  `subchannel` int(11) NOT NULL DEFAULT '0',
  `source` tinyint(1) NOT NULL DEFAULT '0',
  `uid` int(11) NOT NULL,
  `trade_no` char(19) NOT NULL,
  `trade_no_list` varchar(500) DEFAULT NULL,
  `thirdid` varchar(100) NOT NULL,
  `type` varchar(30) NOT NULL,
  `title` varchar(300) DEFAULT NULL,
  `content` text DEFAULT NULL,
  `times` int(11) NOT NULL DEFAULT '1',
  `money` decimal(10,2) DEFAULT NULL,
  `status` tinyint(1) NOT NULL DEFAULT '0',
  `phone` varchar(30) DEFAULT NULL,
  `addtime` datetime NOT NULL,
  `edittime` datetime DEFAULT NULL,
  `thirdmchid` varchar(30) DEFAULT NULL,
  `info` text DEFAULT NULL,
 PRIMARY KEY (`id`),
 KEY `uid` (`uid`),
 KEY `thirdid` (`thirdid`,`channel`),
 KEY `addtime` (`addtime`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8;

DROP TABLE IF EXISTS `pre_complaintask`;
CREATE TABLE `pre_complaintask` (
  `id` int(11) unsigned NOT NULL auto_increment,
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

DROP TABLE IF EXISTS `pre_applychannel`;
CREATE TABLE `pre_applychannel` (
  `id` int(11) unsigned NOT NULL auto_increment,
  `type` varchar(20) NOT NULL,
  `channel` int(11) NOT NULL,
  `name` varchar(30) NOT NULL,
  `desc` varchar(200) DEFAULT NULL,
  `price` decimal(10,2) DEFAULT NULL,
  `sort` int(10) NOT NULL DEFAULT 0,
  `gid` int(11) unsigned NOT NULL DEFAULT '0',
  `addtime` datetime NOT NULL,
  `status` tinyint(1) NOT NULL DEFAULT '0',
  `config` text DEFAULT NULL,
 PRIMARY KEY (`id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8;

DROP TABLE IF EXISTS `pre_applymerchant`;
CREATE TABLE `pre_applymerchant` (
  `id` int(11) unsigned NOT NULL auto_increment,
  `cid` int(11) unsigned NOT NULL,
  `uid` int(11) unsigned NOT NULL,
  `orderid` char(16) NOT NULL,
  `thirdid` varchar(150) DEFAULT NULL,
  `mchtype` tinyint(4) unsigned NOT NULL,
  `mchname` varchar(150) NOT NULL,
  `mchid` varchar(80) DEFAULT NULL,
  `addtime` datetime NOT NULL,
  `updatetime` datetime NOT NULL,
  `refreshtime` datetime DEFAULT NULL,
  `status` tinyint(1) NOT NULL DEFAULT '0',
  `paid` tinyint(1) NOT NULL DEFAULT '0',
  `info` text DEFAULT NULL,
  `ext` text DEFAULT NULL,
  `reason` varchar(200) DEFAULT NULL,
  `remark` varchar(100) DEFAULT NULL,
  `sub_mchid` varchar(300) DEFAULT NULL,
 PRIMARY KEY (`id`),
 KEY `uid` (`uid`),
 KEY `orderid` (`orderid`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8;

DROP TABLE IF EXISTS `pre_applytrade`;
CREATE TABLE `pre_applytrade` (
  `id` int(11) unsigned NOT NULL auto_increment,
  `mid` int(11) unsigned NOT NULL,
  `type` tinyint(1) NOT NULL DEFAULT '0',
  `orderid` char(18) NOT NULL,
  `thirdid` varchar(150) DEFAULT NULL,
  `from` varchar(150) DEFAULT NULL,
  `account` varchar(150) DEFAULT NULL,
  `money` decimal(10,2) NOT NULL,
  `fee` decimal(10,2) DEFAULT NULL,
  `addtime` datetime NOT NULL,
  `endtime` datetime DEFAULT NULL,
  `status` tinyint(1) NOT NULL DEFAULT '0',
  `statustext` varchar(30) DEFAULT NULL,
  `reason` varchar(300) DEFAULT NULL,
 PRIMARY KEY (`id`),
 KEY `mid` (`mid`),
 KEY `orderid` (`orderid`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8;

DROP TABLE IF EXISTS `pre_applyreport`;
CREATE TABLE `pre_applyreport` (
  `id` int(11) unsigned NOT NULL auto_increment,
  `mid` int(11) unsigned NOT NULL,
  `type` varchar(10) NOT NULL,
  `orderid` char(18) NOT NULL,
  `thirdid` varchar(100) DEFAULT NULL,
  `channel` varchar(100) DEFAULT NULL,
  `submchid` varchar(150) DEFAULT NULL,
  `addtime` datetime NOT NULL,
  `endtime` datetime DEFAULT NULL,
  `status` tinyint(1) NOT NULL DEFAULT '0',
  `reason` varchar(300) DEFAULT NULL,
  `active` tinyint(1) NOT NULL DEFAULT '0',
 PRIMARY KEY (`id`),
 KEY `mid` (`mid`),
 KEY `orderid` (`orderid`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8;

DROP TABLE IF EXISTS `pre_mchrisk`;
CREATE TABLE `pre_mchrisk` (
  `id` int(11) NOT NULL AUTO_INCREMENT,
  `thirdid` varchar(100) NOT NULL,
  `channel` int(11) NOT NULL,
  `uid` int(11) NOT NULL DEFAULT '0',
  `mchid` varchar(64) NOT NULL,
  `mchname` varchar(150) DEFAULT NULL,
  `risk_type` varchar(100) DEFAULT NULL,
  `risk_desc` varchar(255) DEFAULT NULL,
  `punish_type` varchar(255) DEFAULT NULL,
  `punish_desc` varchar(255) DEFAULT NULL,
  `punish_time` datetime DEFAULT NULL,
  `recover_way` varchar(200) DEFAULT NULL,
  `recover_url` varchar(200) DEFAULT NULL,
  `status` tinyint(1) NOT NULL DEFAULT '0',
  PRIMARY KEY (`id`),
  KEY `thirdid` (`thirdid`),
  KEY `uid` (`uid`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8;

DROP TABLE IF EXISTS `pre_attachment`;
CREATE TABLE `pre_attachment` (
  `id` int(11) unsigned NOT NULL auto_increment,
  `type` varchar(10) NOT NULL,
  `name` varchar(255) NOT NULL,
  `path` varchar(255) NOT NULL,
  `addtime` datetime NOT NULL,
  `status` tinyint(1) NOT NULL DEFAULT '0',
  `ext` varchar(10) NOT NULL,
  `size` int(11) NOT NULL,
  `md5` varchar(32) NOT NULL,
 PRIMARY KEY (`id`),
 KEY `name`(`name`,`type`),
 KEY `path`(`path`,`type`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8;

DROP TABLE IF EXISTS `pre_adminuser`;
CREATE TABLE `pre_adminuser` (
  `uid` int(11) unsigned NOT NULL auto_increment,
  `username` varchar(50) NOT NULL,
  `password` varchar(255) NOT NULL,
  `paypwd` varchar(255) NOT NULL,
  `totp` tinyint(1) NOT NULL DEFAULT '0',
  `totp_secret` varchar(255) DEFAULT NULL,
  `addtime` datetime NOT NULL,
  `lasttime` datetime NOT NULL,
  `status` tinyint(1) NOT NULL DEFAULT '0',
  `super` tinyint(1) NOT NULL DEFAULT '0',
  `permission` text DEFAULT NULL,
 PRIMARY KEY (`uid`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8;

DROP TABLE IF EXISTS `pre_onecode`;
CREATE TABLE `pre_onecode` (
  `id` int(11) unsigned NOT NULL auto_increment,
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
) ENGINE=InnoDB DEFAULT CHARSET=utf8;

INSERT INTO `pre_onecode` (`id`,`name`,`label`,`qr_width`,`qr_height`,`foreground`,`background`,`img_width`,`img_height`,`font`,`font_color`,`rec_name_left`,`rec_name_top`,`qr_left`,`qr_top`,`url`,`sort`,`status`,`word`) VALUES
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