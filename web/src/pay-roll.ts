import { ApiError } from "./api";
export type PayRollChannel = { id: string; weight: string };
export type PayRollRecord = {
  id: string;
  name: string;
  type: string;
  strategy: string;
  merchant_id: string;
  enabled: boolean;
  default_route?: boolean;
  channel_ids: string;
  limits: string;
  rules: string;
};
export type PayRollSnapshot = {
  id: string;
  revision: string;
  fields: PayRollRecord;
};
export const payRollStrategies = [
  { value: "round_robin", label: "按顺序依次轮询" },
  { value: "weighted", label: "按权重随机轮询" },
  { value: "first", label: "仅使用第一个已启用的" },
];
export const payRollTypes: Record<string, string> = {
  alipay: "支付宝",
  wxpay: "微信支付",
  qqpay: "QQ钱包",
  bank: "银行卡",
  jdpay: "京东支付",
  paypal: "PayPal",
  douyinpay: "抖音支付",
  ecny: "数字人民币",
};
export function payRollID(v: unknown, zero = false): string {
  if (
    typeof v !== "string" ||
    !(zero ? /^(0|[1-9][0-9]{0,18})$/ : /^[1-9][0-9]{0,18}$/).test(v) ||
    BigInt(v) > 9223372036854775807n
  )
    throw Error("轮询编号无法核对");
  return v;
}
export function readPayRollChannels(raw: unknown): PayRollChannel[] {
  if (typeof raw !== "string" || raw.length > 1000)
    throw Error("轮询通道格式错误");
  if (!raw) return [];
  const seen = new Set<string>();
  const result = raw.split(",").map((entry) => {
    const parts = entry.trim().split(":"),
      id = payRollID(parts[0]),
      weight = parts[1] ?? "0";
    if (
      parts.length > 2 ||
      seen.has(id) ||
      !/^(0|[1-9][0-9]*)$/.test(weight) ||
      BigInt(weight) > 1000000n
    )
      throw Error("轮询通道或权重无法核对");
    seen.add(id);
    return { id, weight };
  });
  if (result.length > 100) throw Error("轮询通道过多");
  return result;
}
function object(raw: unknown): Record<string, unknown> {
  if (!raw || typeof raw !== "object" || Array.isArray(raw))
    throw Error("轮询配置无法核对");
  return raw as Record<string, unknown>;
}
export function readPayRollRecord(
  raw: unknown,
  expectedID?: string,
): PayRollRecord {
  const row = { ...object(raw) },
    id = payRollID(row.id);
  if (expectedID && id !== expectedID) throw Error("轮询上下文不匹配");
  if (
    typeof row.name !== "string" ||
    !row.name.trim() ||
    typeof row.type !== "string" ||
    !Object.hasOwn(payRollTypes, row.type) ||
    typeof row.strategy !== "string" ||
    !["round_robin", "weighted", "first", "priority"].includes(row.strategy) ||
    typeof row.enabled !== "boolean" ||
    (row.default_route !== undefined && typeof row.default_route !== "boolean")
  )
    throw Error("轮询配置无法核对");
  payRollID(row.merchant_id, true);
  const channels = readPayRollChannels(row.channel_ids);
  for (const key of ["limits", "rules"]) {
    // ParsePayRoll has always treated omitted/null/empty optional JSON as {}.
    // This is a read projection only; channel saves never send these fields.
    if (row[key] === undefined || row[key] === null || row[key] === "")
      row[key] = "{}";
    if (typeof row[key] !== "string") throw Error("轮询限制无法核对");
    object(JSON.parse(row[key] as string));
  }
  if (
    !channels.length &&
    (row.enabled ||
      row.default_route !== false ||
      Object.keys(JSON.parse(row.limits as string)).length ||
      Object.keys(JSON.parse(row.rules as string)).length)
  )
    throw Error("空通道轮询不处于安全草稿状态");
  if (
    channels.length &&
    row.strategy === "weighted" &&
    channels.every((c) => c.weight === "0")
  )
    throw Error("轮询权重合计必须大于0");
  return { ...row, id } as PayRollRecord;
}
export function readPayRollSnapshot(
  raw: unknown,
  expectedID: string,
): PayRollSnapshot {
  const snapshot = object(raw),
    id = payRollID(snapshot.id);
  if (
    id !== expectedID ||
    typeof snapshot.revision !== "string" ||
    !/^[a-f0-9]{64}$/.test(snapshot.revision)
  )
    throw Error("轮询配置版本无法核对");
  return {
    id,
    revision: snapshot.revision,
    fields: readPayRollRecord({ ...object(snapshot.fields), id }, id),
  };
}
export function knownPayRollRejection(error: unknown): error is ApiError {
  return (
    error instanceof ApiError &&
    error.responseDecoded &&
    error.status >= 400 &&
    error.status < 500
  );
}
