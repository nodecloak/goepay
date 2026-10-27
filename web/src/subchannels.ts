import { readCoreRiskStatus, type CoreRiskStatus } from "./core-risk-status";
import type { Area } from "./menu";
const max = 9223372036854775807n;
const obj = (v: unknown): Record<string, unknown> => {
  if (!v || typeof v !== "object" || Array.isArray(v))
    throw Error("子通道资料无法核对");
  return v as Record<string, unknown>;
};
export const subchannelID = (v: unknown): string => {
  if (typeof v !== "string" || !/^[1-9]\d{0,18}$/.test(v) || BigInt(v) > max)
    throw Error("通道或商户编号无法精确核对");
  return v;
};
export function subchannelSessionID(v: unknown): string {
  return subchannelID(
    typeof v === "number" && Number.isSafeInteger(v) ? String(v) : v,
  );
}
const amount = (v: unknown): string => {
  if (
    typeof v !== "string" ||
    !/^(0|[1-9]\d{0,18})$/.test(v) ||
    BigInt(v) > max
  )
    throw Error("金额无法精确核对");
  return v;
};
const text = (v: unknown, maxBytes = 120): string => {
  if (
    typeof v !== "string" ||
    new TextEncoder().encode(v).length > maxBytes ||
    /[\x00\r\n]/.test(v)
  )
    throw Error("子通道文字资料无效");
  return v;
};
const bool = (v: unknown): boolean => {
  if (typeof v !== "boolean") throw Error("子通道状态无法核对");
  return v;
};
const integer = (v: unknown, min: number, max: number): number => {
  if (typeof v !== "number" || !Number.isSafeInteger(v) || v < min || v > max)
    throw Error("子通道数值无法核对");
  return v;
};
const fingerprint = (v: unknown): string => {
  if (typeof v !== "string" || !/[a-f0-9]{64}/.test(v) || v.length !== 64)
    throw Error("资料已变化，请重新读取");
  return v;
};
const payType = (v: unknown): "alipay" | "wxpay" => {
  if (v !== "alipay" && v !== "wxpay") throw Error("支付方式无法核对");
  return v;
};
export type SubchannelField = {
  name: string;
  label: string;
  secret: boolean;
  required: boolean;
};
export type SubchannelSchema = {
  parentId: string;
  revision: string;
  payType: "alipay" | "wxpay";
  fingerprint: string;
  fields: SubchannelField[];
};
export type SubchannelPolicy = {
  merchantId: string;
  rollType: number;
  revision: string;
  fingerprint: string;
};
export const subchannelRollNames = [
  "随机轮询",
  "顺序轮询",
  "权重轮询",
  "首个可用",
] as const;
export type SubchannelRow = {
  coreRisk?: CoreRiskStatus;
  parentCoreRisk?: CoreRiskStatus;
  id: string;
  merchantId: string;
  parentId: string;
  payType: "alipay" | "wxpay";
  name: string;
  enabled: boolean;
  weight: number;
  minAmount: string;
  maxAmount: string;
  revision: string;
  accountVersion: string;
  fingerprint: string;
  schemaFingerprint: string;
  rollType: number;
  fields: { name: string; secret: boolean; present: boolean }[];
  methods: string[];
  accountAvailable: boolean;
  parentAvailable: boolean;
  browserTestAvailable: boolean;
  unavailableReason: string;
  feeRateBps: number | null;
  feeSource: string;
  feePolicy: string;
  statistics: null | {
    attempts: string;
    confirmed: string;
    today: string;
    yesterday: string;
    successBps: number | null;
    timezone: string;
    basis: string;
  };
};
const methods = new Set([
  "page",
  "wap",
  "native",
  "h5",
  "app",
  "jsapi",
  "mini",
  "applet",
  "scan",
  "preauth",
]);
const reasons = [
  "",
  "account_unconfirmed",
  "merchant_disabled",
  "group_unavailable",
  "parent_unavailable",
  "risk_limited",
  "fee_unconfirmed",
  "writes_disabled",
  "disabled",
];
const fieldName = (v: unknown): string => {
  if (typeof v !== "string" || !/^[a-z][a-z0-9_]{0,47}$/.test(v))
    throw Error("账户参数字段无法核对");
  return v;
};
export function readSubchannelSchemas(raw: unknown): SubchannelSchema[] {
  const data = obj(raw);
  if (
    !Array.isArray(data.items) ||
    data.items.length > 1000 ||
    data.total !== data.items.length
  )
    throw Error("父通道目录无法核对");
  const seen = new Set<string>();
  return data.items.map((item) => {
    const r = obj(item),
      parentId = subchannelID(r.parent_id);
    if (
      seen.has(parentId) ||
      !Array.isArray(r.fields) ||
      !r.fields.length ||
      r.fields.length > 24
    )
      throw Error("父通道参数目录无效");
    seen.add(parentId);
    const names = new Set<string>();
    return {
      parentId,
      revision: subchannelID(r.revision),
      payType: payType(r.pay_type),
      fingerprint: fingerprint(r.fingerprint),
      fields: r.fields.map((item) => {
        const f = obj(item),
          name = fieldName(f.name);
        if (names.has(name)) throw Error("账户参数字段重复");
        names.add(name);
        return {
          name,
          label: f.label === undefined ? name : text(f.label),
          secret: bool(f.secret),
          required: bool(f.required),
        };
      }),
    };
  });
}
export function readSubchannelPolicy(
  raw: unknown,
  merchantId: string,
): SubchannelPolicy {
  const r = obj(raw),
    id = subchannelID(r.merchant_id);
  if (id !== merchantId) throw Error("轮询设置不属于当前商户");
  return {
    merchantId: id,
    rollType: integer(r.roll_type, 0, 3),
    revision: amount(r.revision),
    fingerprint: fingerprint(r.fingerprint),
  };
}
export function readSubchannelRow(
  raw: unknown,
  area: Area,
  selfId: string,
): SubchannelRow {
  const r = obj(raw);
  for (const key of [
    "values",
    "snapshot",
    "ciphertext",
    "nonce",
    "proof_hash",
    "account_scope",
    "private_key",
    "app_secret",
  ])
    if (Object.hasOwn(r, key)) throw Error("子通道资料包含不应显示的字段");
  const merchantId = subchannelID(r.merchant_id);
  if (r.archived !== undefined && bool(r.archived))
    throw Error("子通道已删除，请刷新列表后操作");
  if (area === "merchant" && merchantId !== selfId)
    throw Error("子通道不属于当前商户");
  if (
    !Array.isArray(r.fields) ||
    r.fields.length > 24 ||
    !Array.isArray(r.methods) ||
    r.methods.length > 32 ||
    r.methods.some((m) => typeof m !== "string" || !methods.has(m)) ||
    new Set(r.methods).size !== r.methods.length
  )
    throw Error("子通道能力无法核对");
  const names = new Set<string>();
  const fields = r.fields.map((v) => {
    const f = obj(v),
      name = fieldName(f.name);
    if (names.has(name) || Object.hasOwn(f, "value"))
      throw Error("账户参数状态无效");
    names.add(name);
    return { name, secret: bool(f.secret), present: bool(f.present) };
  });
  if (
    typeof r.unavailable_reason !== "string" ||
    !reasons.includes(r.unavailable_reason) ||
    !["none", "parent", "group", "legacy", "unknown"].includes(
      String(r.fee_source),
    ) ||
    !["none", "merchant_deduct", "unknown"].includes(String(r.fee_policy))
  )
    throw Error("子通道当前政策无法核对");
  let statistics: SubchannelRow["statistics"] = null;
  if (r.statistics !== undefined && r.statistics !== null) {
    const s = obj(r.statistics);
    if (
      s.timezone !== "UTC+8" ||
      s.basis !== "verified_confirmation_gross_including_later_refunds"
    )
      throw Error("子通道统计口径无法核对");
    statistics = {
      attempts: amount(s.attempts),
      confirmed: amount(s.confirmed),
      today: amount(s.today_amount),
      yesterday: amount(s.yesterday_amount),
      successBps:
        s.success_bps === null ? null : integer(s.success_bps, 0, 10000),
      timezone: s.timezone,
      basis: s.basis,
    };
    if (
      BigInt(statistics.confirmed) > BigInt(statistics.attempts) ||
      (statistics.attempts === "0") !== (statistics.successBps === null)
    )
      throw Error("子通道统计无法核对");
    if (
      statistics.successBps !== null &&
      BigInt(statistics.successBps) !==
        (BigInt(statistics.confirmed) * 10000n) / BigInt(statistics.attempts)
    )
      throw Error("子通道确认比例不一致");
  }
  if (
    r.browser_test_available === true &&
    (!(
      r.account_available === true &&
      r.parent_available === true &&
      r.enabled === true
    ) ||
      r.unavailable_reason !== "" ||
      r.fee_rate_bps === null ||
      r.fee_rate_bps === undefined ||
      r.fee_source === "unknown" ||
      r.fee_policy === "unknown" ||
      !r.methods.some((m) =>
        ["page", "wap", "native", "h5", "app"].includes(String(m)),
      ))
  )
    throw Error("浏览器测试能力与账户状态不一致");
  if (
    r.browser_test_available === true &&
    ((r.core_risk !== undefined && readCoreRiskStatus(r.core_risk).blocked) ||
      (r.parent_core_risk !== undefined &&
        readCoreRiskStatus(r.parent_core_risk).blocked))
  )
    throw Error("风控有效状态与付款测试能力不一致");
  const minAmount = amount(r.min_amount),
    maxAmount = amount(r.max_amount);
  if (maxAmount !== "0" && BigInt(minAmount) > BigInt(maxAmount))
    throw Error("单笔限额无效");
  return {
    id: subchannelID(r.id),
    merchantId,
    parentId: subchannelID(r.parent_id),
    payType: payType(r.pay_type),
    name: text(r.name),
    coreRisk:
      r.core_risk === undefined ? undefined : readCoreRiskStatus(r.core_risk),
    parentCoreRisk:
      r.parent_core_risk === undefined
        ? undefined
        : readCoreRiskStatus(r.parent_core_risk),
    enabled: bool(r.enabled),
    weight: integer(r.weight, 1, 99),
    minAmount,
    maxAmount,
    revision: subchannelID(r.revision),
    accountVersion: subchannelID(r.account_version),
    fingerprint: fingerprint(r.fingerprint),
    schemaFingerprint: fingerprint(r.schema_fingerprint),
    rollType: integer(r.roll_type, 0, 3),
    fields,
    methods: [...r.methods] as string[],
    accountAvailable: bool(r.account_available),
    parentAvailable: bool(r.parent_available),
    browserTestAvailable: bool(r.browser_test_available),
    unavailableReason: r.unavailable_reason,
    feeRateBps:
      r.fee_rate_bps === null ? null : integer(r.fee_rate_bps, 0, 10000),
    feeSource: String(r.fee_source),
    feePolicy: String(r.fee_policy),
    statistics,
  };
}
export function readSubchannelPage(raw: unknown, area: Area, selfId: string) {
  const r = obj(raw);
  if (!Array.isArray(r.items) || r.items.length > 100)
    throw Error("子通道列表无法核对");
  const ids = new Set<string>(),
    items = r.items.map((v) => {
      const row = readSubchannelRow(v, area, selfId);
      if (ids.has(row.id)) throw Error("子通道编号重复");
      ids.add(row.id);
      return row;
    });
  return {
    items,
    total: integer(r.total, 0, Number.MAX_SAFE_INTEGER),
    page: integer(r.page, 1, 1000000),
    size: integer(r.size, 1, 100),
  };
}
export function subchannelMoney(cents: string): string {
  const value = BigInt(amount(cents));
  return (
    (value / 100n).toString() + "." + (value % 100n).toString().padStart(2, "0")
  );
}
export function subchannelMoneyInput(
  value: string,
  allowUnlimited = true,
): string {
  if (value === "" && allowUnlimited) return "0";
  if (!/^(0|[1-9]\d*)(\.\d{1,2})?$/.test(value))
    throw Error("金额请输入元，最多两位小数");
  const [whole, fraction = ""] = value.split("."),
    cents = BigInt(whole!) * 100n + BigInt(fraction.padEnd(2, "0"));
  if (cents > max || (!allowUnlimited && cents === 0n))
    throw Error("金额超出可用范围");
  return cents.toString();
}
export const subchannelReason = (reason: string): string =>
  ({
    account_unconfirmed: "账户参数需要管理员核对",
    merchant_disabled: "商户当前未开放支付",
    group_unavailable: "当前用户组未开放此方式的子通道",
    parent_unavailable: "父通道当前不可用",
    risk_limited: "子通道连续未付，当前限制新付款",
    fee_unconfirmed: "当前费率尚未核对",
    writes_disabled: "当前未开放支付写入",
    disabled: "已关闭",
  })[reason] || "";
export const subchannelFeeSource = (source: string): string =>
  ({
    none: "计费未开启",
    parent: "跟随父通道",
    group: "当前用户组",
    legacy: "现有计费规则",
    unknown: "未核对",
  })[source] || "未核对";
export type SubchannelFilters = {
  id: string;
  name: string;
  payType: string;
  merchantId: string;
  parentId: string;
  page: number;
  size: number;
};
export const freshSubchannelFilters = (): SubchannelFilters => ({
  id: "",
  name: "",
  payType: "",
  merchantId: "",
  parentId: "",
  page: 1,
  size: 10,
});
export function subchannelListQuery(
  f: SubchannelFilters,
  area: Area,
  selfId: string,
): string {
  const q = new URLSearchParams({
    page: String(integer(f.page, 1, 1000000)),
    size: String(integer(f.size, 1, 100)),
  });
  if (f.id) q.set("id", subchannelID(f.id));
  if (f.name) q.set("name", text(f.name));
  if (f.payType) q.set("type", payType(f.payType));
  if (f.parentId) q.set("parent_id", subchannelID(f.parentId));
  if (f.merchantId) {
    const id = subchannelID(f.merchantId);
    if (area === "merchant" && id !== selfId)
      throw Error("只能查看本人的子通道");
    if (area === "admin") q.set("merchant_id", id);
  }
  return q.toString();
}
export function readSubchannelRoute(
  raw: Record<string, unknown>,
  area: Area,
  selfId: string,
): SubchannelFilters {
  const f = freshSubchannelFilters(),
    v: Record<string, string> = {};
  for (const [k, value] of Object.entries(raw)) {
    if (
      ![
        "uid",
        "merchant_id",
        "id",
        "parent_id",
        "name",
        "type",
        "page",
        "size",
      ].includes(k) ||
      typeof value !== "string" ||
      /[\x00\r\n]/.test(value)
    )
      throw Error("关联子通道筛选无效，请清空后查询");
    v[k] = value;
  }
  if (
    v.uid !== undefined &&
    v.merchant_id !== undefined &&
    v.uid !== v.merchant_id
  )
    throw Error("关联商户编号不一致");
  f.merchantId = v.uid ?? v.merchant_id ?? "";
  f.id = v.id ?? "";
  f.parentId = v.parent_id ?? "";
  f.name = v.name ?? "";
  f.payType = v.type ?? "";
  for (const k of ["page", "size"] as const)
    if (v[k] !== undefined) {
      if (!/^[1-9]\d*$/.test(v[k]!)) throw Error("分页无效");
      f[k] = Number(v[k]);
    }
  subchannelListQuery(f, area, selfId);
  return f;
}
export const subchannelOrdersHref = (area: Area, id: string) =>
  `/${area}/trade/order?subchannel=${encodeURIComponent(subchannelID(id))}`;
export type SubchannelMutationExpected = {
  before: SubchannelRow | null;
  merchantId: string;
  parentId: string;
  name: string;
  enabled: boolean;
  weight: number;
  minAmount: string;
  maxAmount: string;
  accountChanged?: boolean;
  archived?: boolean;
  schema?: SubchannelSchema;
};
export function readSubchannelMutation(
  raw: unknown,
  area: Area,
  selfId: string,
  expected: SubchannelMutationExpected,
): SubchannelRow {
  const value = obj(raw);
  if (expected.archived && (value.archived !== true || value.enabled !== false))
    throw Error("删除结果无法核对，请刷新列表");
  const row = readSubchannelRow(
    expected.archived ? { ...value, archived: false } : value,
    area,
    selfId,
  );
  const before = expected.before;
  if (
    row.merchantId !== expected.merchantId ||
    row.parentId !== expected.parentId ||
    row.name !== expected.name ||
    row.enabled !== expected.enabled ||
    row.weight !== expected.weight ||
    row.minAmount !== expected.minAmount ||
    row.maxAmount !== expected.maxAmount ||
    row.revision !== (before ? String(BigInt(before.revision) + 1n) : "1") ||
    row.accountVersion !==
      (before
        ? String(
            BigInt(before.accountVersion) + (expected.accountChanged ? 1n : 0n),
          )
        : "1") ||
    (before &&
      (row.id !== before.id ||
        row.payType !== before.payType ||
        row.fingerprint === before.fingerprint))
  )
    throw Error("保存结果与本次目标或版本不一致，请刷新列表");
  if (expected.schema) {
    const schema = expected.schema;
    if (
      row.payType !== schema.payType ||
      row.schemaFingerprint !== schema.fingerprint ||
      row.fields.length !== schema.fields.length ||
      schema.fields.some(
        (f) =>
          !row.fields.some(
            (v) =>
              v.name === f.name &&
              v.secret === f.secret &&
              (!f.required || v.present),
          ),
      )
    )
      throw Error("保存后的账户字段无法核对，请刷新列表");
  }
  return row;
}
