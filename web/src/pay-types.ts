// Original platform pay-type catalog (pre_type) for the web console. Shownames
// match install.sql; ecny (数字人民币) is served by some ported plugins. Keep in
// sync with the Go adapter.PluginPayTypes and the backoffice pay-types select.
export const PAY_TYPE_LABELS: Record<string, string> = {
  alipay: "支付宝",
  wxpay: "微信支付",
  qqpay: "QQ钱包",
  bank: "银联支付",
  jdpay: "京东支付",
  paypal: "PayPal",
  douyinpay: "抖音支付",
  ecny: "数字人民币",
  usdt: "USDT",
  sandbox: "本地模拟",
};

export const CATALOG_PAY_TYPES = [
  "alipay",
  "wxpay",
  "qqpay",
  "bank",
  "jdpay",
  "paypal",
  "douyinpay",
  "ecny",
  "usdt",
] as const;

// Existing CNY report/refund fields use integer cents. USDT has its own asset
// order view and must not be offered to these currency-specific projections.
export const FILTER_PAY_TYPES = [
  ...CATALOG_PAY_TYPES.filter((type) => type !== "usdt"),
  "sandbox",
] as const;

export function payTypeLabel(value: unknown): string {
  return PAY_TYPE_LABELS[String(value)] ?? String(value ?? "—");
}
