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
  platform?: PlatformNoticeSwitches;
  sms_active?: boolean;
}
export interface PlatformNoticeSwitches {
  mail_robot: Record<string, boolean>;
  telegram: boolean;
  qqbot: boolean;
  voice: boolean;
  group_sms: boolean;
  channels?: Partial<
    Record<"wechat_template" | "smtp" | "sms" | "robot", boolean>
  >;
  wechat_templates?: Record<string, boolean>;
}
// Channel columns in the original epay_pro 消息推送 order. Each value is the
// stored Mode an event row selects when that channel is picked (0 = 关闭提醒).
export const noticeChannels = [
  [1, "微信", "wechat"],
  [2, "邮件", "smtp"],
  [3, "短信", "sms"],
  [4, "群机器人", "robot"],
  [5, "Telegram", "telegram"],
  [6, "QQ Bot", "qqbot"],
] as const;
export type NoticeChannelMode = (typeof noticeChannels)[number][0];
// Scenes whose selected WeChat template exists (SelectMerchant: Wechat branch).
const wechatScenes = new Set<NoticeScene>([
  "order",
  "settle",
  "login",
  "complain",
  "balance",
]);
// Scenes that can deliver via a *selected* SMS mode (SelectMerchant: SMS branch).
const smsSelectableScenes = new Set<NoticeScene>(["balance", "complain"]);
export interface ChannelCell {
  mode: NoticeChannelMode;
  label: string;
  ready: boolean;
  reason: string;
}
// channelCell mirrors merchantnotice.SelectMerchant exactly for a *selected*
// mode: it decides whether choosing this channel for this event can deliver,
// and gives the one-line reason it cannot. It never invents a transport the
// backend does not already deliver for some event.
export function channelCell(
  scene: NoticeScene,
  mode: NoticeChannelMode,
  label: string,
  platform: PlatformNoticeSwitches | undefined,
  smsActive: boolean,
  recipientConfigured: boolean,
): ChannelCell {
  const p = platform;
  const cell = (ready: boolean, reason: string): ChannelCell => ({
    mode,
    label,
    ready,
    reason,
  });
  const transport = {
    1: "wechat_template",
    2: "smtp",
    3: "sms",
    4: "robot",
  } as const;
  if (mode <= 4 && p?.channels?.[transport[mode as 1 | 2 | 3 | 4]] === false)
    return cell(false, "平台已关闭该消息渠道");
  switch (mode) {
    case 1: // WeChat template
      if (!wechatScenes.has(scene))
        return cell(false, "该事件没有微信模板消息");
      if (p?.wechat_templates && !p.wechat_templates[scene])
        return cell(false, "平台尚未配置完整的微信通知模板");
      break;
    case 2: // Email
    case 4: // Group robot
      if (scene !== "daily_report" && p && !p.mail_robot[scene])
        return cell(false, "平台未启用该事件的邮件/群机器人提醒");
      break;
    case 3: // SMS
      if (!smsSelectableScenes.has(scene))
        return cell(false, "该事件不支持短信提醒");
      if (!smsActive) return cell(false, "通知短信尚未接入");
      break;
    case 5: // Telegram
      if (p && !p.telegram) return cell(false, "平台未启用 Telegram 提醒");
      break;
    case 6: // QQ Bot
      if (p && !p.qqbot) return cell(false, "平台未启用 QQ Bot 提醒");
      break;
  }
  if (!recipientConfigured) return cell(false, "尚未配置接收方，请在下方填写");
  return cell(true, "");
}
// recipientKey maps a channel mode to the target field that must be configured.
export function recipientConfigured(
  mode: NoticeChannelMode,
  configured: Record<string, boolean>,
): boolean {
  switch (mode) {
    case 1:
      return !!configured.wx_uid && !!configured.wx_appid;
    case 2:
      return !!configured.email;
    case 3:
      return true; // SMS recipient is the merchant's bound mobile, resolved at send.
    case 4:
      return !!configured.msgrobot_url;
    case 5:
      return !!configured.telegram_uid;
    case 6:
      return (
        !!configured.qqbot_appid &&
        !!configured.qqbot_appsecret &&
        !!configured.qqbot_openid
      );
    default:
      return false;
  }
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
