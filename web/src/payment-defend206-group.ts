export interface PaymentDefendGroup {
  group_id: string;
  group_name: string;
  configured: boolean;
  revision: string;
  pay_verify: string;
  effective_pay_verify: string;
  fingerprint: string;
  can_update: boolean;
}

const keys = [
  "group_id",
  "group_name",
  "configured",
  "revision",
  "pay_verify",
  "effective_pay_verify",
  "fingerprint",
  "can_update",
]
  .sort()
  .join();

// Original gedit.php config[pay_verify]: '' 缺省, 0 关闭, 1 按规则, 3 强制.
export const groupPayVerifyOptions: [string, string][] = [
  ["", "缺省（与系统设置一致）"],
  ["0", "关闭"],
  ["1", "按规则开启"],
  ["3", "强制开启"],
];

export const payVerifyLabels: Record<string, string> = {
  "0": "关闭",
  "1": "按规则开启",
  "2": "指定商户开启",
  "3": "全部商户开启",
};

export function readPaymentDefendGroup(
  raw: unknown,
  id: string,
): PaymentDefendGroup {
  const v = raw as PaymentDefendGroup;
  if (
    !v ||
    typeof v !== "object" ||
    Array.isArray(v) ||
    Object.keys(v).sort().join() !== keys ||
    v.group_id !== id ||
    typeof v.group_name !== "string" ||
    !v.group_name ||
    typeof v.configured !== "boolean" ||
    typeof v.can_update !== "boolean" ||
    typeof v.revision !== "string" ||
    !/^(0|[1-9]\d{0,18})$/.test(v.revision) ||
    BigInt(v.revision) > 9223372036854775807n ||
    v.configured !== (v.revision !== "0") ||
    typeof v.fingerprint !== "string" ||
    !/^[0-9a-f]{64}$/.test(v.fingerprint) ||
    !["", "0", "1", "3"].includes(v.pay_verify) ||
    !["0", "1", "2", "3"].includes(v.effective_pay_verify) ||
    (v.pay_verify !== "" && v.effective_pay_verify !== v.pay_verify)
  )
    throw Error("用户组人机验证数据无法核对");
  return { ...v };
}
