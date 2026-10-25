export const noticeScenes = [
  ["order", "订单支付成功"],
  ["settle", "结算完成"],
  ["login", "账号登录"],
  ["complain", "交易投诉"],
  ["mchrisk", "渠道商户违规处置"],
  ["balance", "余额不足"],
  ["group", "会员到期"],
  ["domain_audit", "授权域名审核"],
  ["daily_report", "每日交易统计"],
] as const;
export type NoticeScene = (typeof noticeScenes)[number][0];
export type NoticeConfig = Record<NoticeScene, number> & {
  order_money: string;
  balance_money: string;
};
export const noticeTargets = [
  ["wx_uid", "微信 OpenID"],
  ["wx_appid", "微信应用 AppID"],
  ["telegram_uid", "Telegram 用户 ID"],
  ["msgrobot_url", "群机器人 Webhook"],
  ["qqbot_appid", "QQ Bot AppID"],
  ["qqbot_appsecret", "QQ Bot AppSecret"],
  ["qqbot_openid", "QQ Bot OpenID"],
  ["voice_devid", "语音设备编号"],
] as const;
export type NoticeTarget = (typeof noticeTargets)[number][0];
export interface MerchantNoticeSettingsView {
  id: string;
  merchant_id: string;
  revision: string;
  msgconfig: NoticeConfig;
  voice_order: boolean;
  email_masked: string;
  configured: Record<string, boolean>;
  target_masks: Record<string, string>;
}
export interface MerchantNoticeEvent {
  id: string;
  scene: string;
  trade_no?: string;
  at: string;
  state: string;
  deliveries: { transport: string; state: string; attempted_at?: string }[];
}
export interface MerchantNoticeEventsPage {
  items: MerchantNoticeEvent[];
  next_cursor: string;
}
