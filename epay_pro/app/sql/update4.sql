CREATE TABLE IF NOT EXISTS `pre_complain` (
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
 UNIQUE KEY `thirdid` (`thirdid`),
 KEY `addtime` (`addtime`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8;

ALTER TABLE `pre_complain`
ADD COLUMN `subchannel` int(11) NOT NULL DEFAULT '0';

ALTER TABLE `pre_complain`
ADD COLUMN `info` text DEFAULT NULL;

ALTER TABLE `pre_complain`
ADD COLUMN `trade_no_list` varchar(500) DEFAULT NULL,
ADD COLUMN `money` decimal(10,2) DEFAULT NULL;

INSERT INTO `pre_config` VALUES ('complain_open', '1');

ALTER TABLE `pre_complain`
ADD COLUMN `times` int(11) NOT NULL DEFAULT '1';

ALTER TABLE `pre_complain`
MODIFY COLUMN `status` tinyint(1) NOT NULL DEFAULT '0';

CREATE TABLE IF NOT EXISTS `pre_applychannel` (
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

CREATE TABLE IF NOT EXISTS `pre_applymerchant` (
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

ALTER TABLE `pre_applymerchant`
ADD COLUMN `sub_mchid` varchar(300) DEFAULT NULL;

INSERT INTO `pre_config` VALUES ('applyments_open', '1');
INSERT INTO `pre_config` VALUES ('mchrisk_open', '1');

CREATE TABLE IF NOT EXISTS `pre_applytrade` (
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

CREATE TABLE IF NOT EXISTS `pre_applyreport` (
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

CREATE TABLE IF NOT EXISTS `pre_mchrisk` (
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

ALTER TABLE `pre_mchrisk`
ADD COLUMN `recover_way` varchar(200) DEFAULT NULL,
ADD COLUMN `recover_url` varchar(200) DEFAULT NULL;


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

ALTER TABLE `pre_config`
ADD COLUMN `cache` tinyint(1) NOT NULL DEFAULT '1';

CREATE TABLE IF NOT EXISTS `pre_attachment` (
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

ALTER TABLE `pre_user`
ADD COLUMN `mode_feerate` varchar(10) DEFAULT NULL;

ALTER TABLE `pre_group`
ADD COLUMN `desc` varchar(500) DEFAULT NULL;

CREATE TABLE IF NOT EXISTS `pre_adminuser` (
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

ALTER TABLE `pre_order`
DROP INDEX `bill_trade_no`,
DROP INDEX `bill_mch_trade_no`,
ADD INDEX `channel` (`channel`),
ADD INDEX `addtime` (`addtime`);

ALTER TABLE `pre_user`
ADD COLUMN `totp` tinyint(1) NOT NULL DEFAULT '0',
ADD COLUMN `totp_secret` varchar(255) DEFAULT NULL;

ALTER TABLE `pre_user`
ADD COLUMN `ordername_rules` text DEFAULT NULL;

ALTER TABLE `pre_complain`
DROP INDEX `thirdid`;
ALTER TABLE `pre_complain`
ADD KEY `thirdid` (`thirdid`,`channel`);

ALTER TABLE `pre_user`
ADD COLUMN `roll_type` tinyint(1) NOT NULL DEFAULT '0';

ALTER TABLE `pre_subchannel`
ADD COLUMN `weight` int(11) NOT NULL DEFAULT '1';

ALTER TABLE `pre_subchannel`
MODIFY COLUMN `usetime` bigint(13) NOT NULL DEFAULT '0';

ALTER TABLE `pre_channel`
ADD COLUMN `usetime` bigint(13) NOT NULL DEFAULT '0';

ALTER TABLE `pre_channel`
ADD COLUMN `rate_limit_seconds` int(10) NOT NULL DEFAULT '0',
ADD COLUMN `rate_limit_count` int(10) NOT NULL DEFAULT '0';