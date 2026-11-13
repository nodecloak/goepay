import { readParentChannel, type ParentChannel } from "./parent-channels";

export interface CryptoChannelOption {
  id: string;
  name: string;
  network: "tron" | "ethereum";
  fee_bps: number;
  min_amount: string;
  max_amount: string;
  day_limit: string;
}
export interface CryptoChannel extends CryptoChannelOption {
  asset: "USDT";
  pay_type: "usdt";
  plugin: "usdt_tron" | "usdt_ethereum";
  configured: true;
  revision: string;
  enabled: boolean;
  archived: boolean;
  runtime_enabled: boolean;
  merchant_ids: string[];
  created_at: string;
  updated_at: string;
}
export type PaymentChannel = ParentChannel | CryptoChannel;
export const isCryptoChannel = (row: PaymentChannel): row is CryptoChannel =>
  "asset" in row && row.asset === "USDT";
const invalid = () => new Error("USDT 通道资料无法核实，请重新打开页面");
const id = (v: unknown, zero = false): v is string =>
  typeof v === "string" &&
  ((zero && v === "0") || /^[1-9]\d{0,18}$/.test(v)) &&
  BigInt(v) <= 9223372036854775807n;
const text = (v: unknown): v is string =>
  typeof v === "string" &&
  v.trim().length > 0 &&
  v.length <= 120 &&
  !/[\x00-\x1f]/.test(v);
function object(raw: unknown): Record<string, unknown> {
  if (!raw || typeof raw !== "object" || Array.isArray(raw)) throw invalid();
  return raw as Record<string, unknown>;
}
function exact(raw: Record<string, unknown>, keys: string[]) {
  if (
    Object.keys(raw).length !== keys.length ||
    keys.some((k) => !Object.hasOwn(raw, k))
  )
    throw invalid();
}
export function usdtUnits(raw: unknown): bigint {
  if (
    typeof raw !== "string" ||
    !/^(?:0|[1-9]\d{0,9})(?:\.\d{1,6})?$/.test(raw)
  )
    throw new Error("USDT 金额须为非负数，最多六位小数");
  const [whole, fraction = ""] = raw.split(".");
  const units = BigInt(whole!) * 1000000n + BigInt(fraction.padEnd(6, "0"));
  if (units > 9000000000000000n) throw new Error("USDT 金额超过支持范围");
  return units;
}
const optionKeys = [
  "id",
  "name",
  "network",
  "fee_bps",
  "min_amount",
  "max_amount",
  "day_limit",
];
function checkOption(v: Record<string, unknown>) {
  if (
    !id(v.id) ||
    !text(v.name) ||
    !["tron", "ethereum"].includes(String(v.network)) ||
    !Number.isSafeInteger(v.fee_bps) ||
    Number(v.fee_bps) < 0 ||
    Number(v.fee_bps) > 10000
  )
    throw invalid();
  const min = usdtUnits(v.min_amount),
    max = usdtUnits(v.max_amount);
  usdtUnits(v.day_limit);
  if (max !== 0n && min > max) throw invalid();
}
export function readCryptoChannel(raw: unknown): CryptoChannel {
  const v = object(raw);
  exact(v, [
    ...optionKeys,
    "asset",
    "pay_type",
    "plugin",
    "configured",
    "revision",
    "enabled",
    "archived",
    "runtime_enabled",
    "merchant_ids",
    "created_at",
    "updated_at",
  ]);
  checkOption(v);
  if (
    v.asset !== "USDT" ||
    v.pay_type !== "usdt" ||
    v.plugin !== "usdt_" + v.network ||
    v.configured !== true ||
    !id(v.revision) ||
    [v.enabled, v.archived, v.runtime_enabled].some(
      (b) => typeof b !== "boolean",
    ) ||
    !Array.isArray(v.merchant_ids) ||
    v.merchant_ids.some((m) => !id(m)) ||
    new Set(v.merchant_ids).size !== v.merchant_ids.length ||
    [v.created_at, v.updated_at].some(
      (t) => typeof t !== "string" || !Number.isFinite(Date.parse(t)),
    )
  )
    throw invalid();
  return v as unknown as CryptoChannel;
}
export function readPaymentChannelList(raw: unknown): {
  items: PaymentChannel[];
  total: number;
  page: number;
  size: number;
} {
  const v = object(raw);
  exact(v, ["items", "total", "page", "size"]);
  if (
    !Array.isArray(v.items) ||
    !Number.isSafeInteger(v.total) ||
    Number(v.total) < v.items.length ||
    !Number.isSafeInteger(v.page) ||
    Number(v.page) < 1 ||
    !Number.isSafeInteger(v.size) ||
    Number(v.size) < 1 ||
    Number(v.size) > 100 ||
    v.items.length > Number(v.size)
  )
    throw invalid();
  const items = v.items.map((raw) =>
    Object.hasOwn(object(raw), "asset")
      ? readCryptoChannel(raw)
      : readParentChannel(raw),
  );
  if (new Set(items.map((row) => row.id)).size !== items.length)
    throw invalid();
  return {
    items,
    total: Number(v.total),
    page: Number(v.page),
    size: Number(v.size),
  };
}
export function readCryptoChannelOptions(raw: unknown): CryptoChannelOption[] {
  const v = object(raw);
  exact(v, ["items"]);
  if (!Array.isArray(v.items)) throw invalid();
  const items = v.items.map((raw) => {
    const row = object(raw);
    exact(row, optionKeys);
    checkOption(row);
    return row as unknown as CryptoChannelOption;
  });
  if (new Set(items.map((row) => row.id)).size !== items.length)
    throw invalid();
  return items;
}
export interface CryptoChannelDraft {
  name: string;
  network: string;
  enabled: boolean;
  archived: boolean;
  fee: string;
  min_amount: string;
  max_amount: string;
  day_limit: string;
  merchants: string;
}
export function cryptoChannelInput(
  draft: CryptoChannelDraft,
  revision: string,
) {
  if (
    !text(draft.name.trim()) ||
    !["tron", "ethereum"].includes(draft.network) ||
    !id(revision, true)
  )
    throw new Error("请填写通道名称与网络");
  if (!/^\d{1,3}(?:\.\d{1,2})?$/.test(draft.fee))
    throw new Error("手续费率须为 0 至 100 的百分数，最多两位小数");
  const [whole, fraction = ""] = draft.fee.split(".");
  const fee_bps = Number(
    BigInt(whole!) * 100n + BigInt(fraction.padEnd(2, "0")),
  );
  if (fee_bps > 10000) throw new Error("手续费率不能超过 100%");
  const min = usdtUnits(draft.min_amount),
    max = usdtUnits(draft.max_amount);
  const day = usdtUnits(draft.day_limit);
  if (max && min > max) throw new Error("最低收款金额不能大于最高收款金额");
  const merchant_ids = draft.merchants.trim()
    ? draft.merchants.trim().split(/[\s,，]+/)
    : [];
  if (
    merchant_ids.some((m) => !id(m)) ||
    new Set(merchant_ids).size !== merchant_ids.length
  )
    throw new Error("授权商户 ID 须为不重复的正整数");
  return {
    name: draft.name.trim(),
    network: draft.network,
    enabled: draft.enabled,
    archived: draft.archived,
    fee_bps,
    min_amount: min === 0n ? "0" : draft.min_amount,
    max_amount: max === 0n ? "0" : draft.max_amount,
    day_limit: day === 0n ? "0" : draft.day_limit,
    merchant_ids,
    expected_revision: revision,
  };
}
export const cryptoNetworkLabel = (network: string) =>
  network === "tron"
    ? "TRON（TRC20）"
    : network === "ethereum"
      ? "Ethereum（ERC20）"
      : network;
