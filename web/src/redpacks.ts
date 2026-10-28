export interface Redpack {
  id: string;
  merchant_id: string;
  out_biz_no: string;
  channel_id: string;
  type: "alipay" | "wxpay";
  amount: number;
  money: string;
  remark: string;
  created_by: string;
  status: "claimable" | "claiming" | "succeeded" | "failed" | "expired";
  expires_at: string;
  claimed_at?: string;
  created_at: string;
  claim_url: string;
}

const labels: Record<Redpack["status"], string> = {
  claimable: "待领取",
  claiming: "领取处理中",
  succeeded: "已领取",
  failed: "领取失败",
  expired: "已过期",
};

export const redpackStatusLabel = (s: string) =>
  labels[s as Redpack["status"]] ?? "未知";

// Original default 交易号: date("YmdHis") . rand(11111,99999), 19 digits.
export function defaultRedpackBizNo(now = new Date(), rand = Math.random()) {
  const p = (n: number) => String(n).padStart(2, "0");
  return (
    now.getFullYear() +
    p(now.getMonth() + 1) +
    p(now.getDate()) +
    p(now.getHours()) +
    p(now.getMinutes()) +
    p(now.getSeconds()) +
    String(11111 + Math.floor(rand * 88889))
  );
}

export function validRedpackForm(
  f: {
    merchant_id: string;
    out_biz_no: string;
    amount: string;
    remark: string;
    type: string;
    channel_id: string;
  },
  admin: boolean,
): string {
  if (admin && !/^[1-9]\d*$/.test(f.merchant_id)) return "请输入付款商户ID";
  if (!/^\d{19}$/.test(f.out_biz_no)) return "交易号必须为19位纯数字";
  if (!/^(0|[1-9]\d*)(\.\d{1,2})?$/.test(f.amount) || Number(f.amount) <= 0)
    return "红包金额无效";
  if ([...f.remark].length > 32) return "红包备注不能超过32个字";
  if (f.type !== "alipay" && f.type !== "wxpay") return "请选择通道";
  if (f.channel_id && !/^[1-9]\d*$/.test(f.channel_id)) return "转账通道ID无效";
  return "";
}

export function readRedpacks(raw: unknown): {
  items: Redpack[];
  open: boolean;
} {
  const v = raw as { items?: unknown; open?: unknown };
  if (
    !v ||
    typeof v !== "object" ||
    !Array.isArray(v.items) ||
    typeof v.open !== "boolean"
  )
    throw Error("红包数据无法核对");
  for (const r of v.items as Redpack[]) {
    if (
      !r ||
      typeof r.id !== "string" ||
      !/^\d{19}$/.test(r.out_biz_no) ||
      !(r.status in labels) ||
      typeof r.claim_url !== "string"
    )
      throw Error("红包数据无法核对");
  }
  return { items: v.items as Redpack[], open: v.open };
}
