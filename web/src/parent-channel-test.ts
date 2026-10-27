import { parentChannelPath } from "./parent-channels";
export interface ParentPaymentTest {
  request_id: string;
  channel_id: string;
  merchant_id: string;
  out_trade_no: string;
  trade_no: string;
  name: string;
  money: string;
  state: string;
  checkout_url: string;
  material_type: string;
  can_query: boolean;
}
const fail = () => new Error("原测试支付结果无法核实，请读取原请求");
const id = (v: unknown) =>
  typeof v === "string" &&
  /^[1-9]\d*$/.test(v) &&
  BigInt(v) <= 9223372036854775807n;
export function parentTestMoney(raw: string): string {
  const m = /^(0|[1-9]\d*)(?:\.(\d{1,2}))?$/.exec(raw);
  if (!m) throw new Error("请输入有效支付金额");
  const fraction = (m[2] || "").padEnd(2, "0"),
    cents = BigInt(m[1]) * 100n + BigInt(fraction);
  if (cents <= 0n || cents > 9223372036854775807n)
    throw new Error("请输入有效支付金额");
  return m[1] + "." + fraction;
}
export function parentPaymentTestPath(channel: string, key?: string): string {
  const base = parentChannelPath(channel) + "/tests";
  if (key === undefined) return base;
  if (!/^[a-f0-9]{32}$/.test(key)) throw fail();
  return base + "/" + key;
}
export function readParentPaymentTest(
  raw: unknown,
  channel: string,
  key: string,
): ParentPaymentTest {
  if (raw === null || typeof raw !== "object" || Array.isArray(raw))
    throw fail();
  const v = raw as Record<string, unknown>;
  const keys = [
    "request_id",
    "channel_id",
    "merchant_id",
    "out_trade_no",
    "trade_no",
    "name",
    "money",
    "state",
    "checkout_url",
    "material_type",
    "can_query",
  ];
  if (
    Object.keys(v).length !== keys.length ||
    Object.keys(v).some((k) => !keys.includes(k))
  )
    throw fail();
  if (
    !id(channel) ||
    v.channel_id !== channel ||
    v.request_id !== key ||
    !/^[a-f0-9]{32}$/.test(key) ||
    !id(v.merchant_id) ||
    typeof v.out_trade_no !== "string" ||
    !/^PT[a-f0-9]{32}$/.test(v.out_trade_no) ||
    typeof v.trade_no !== "string" ||
    (v.trade_no !== "" && !/^[A-Za-z0-9_*-]{1,64}$/.test(v.trade_no)) ||
    typeof v.name !== "string" ||
    !v.name.trim() ||
    v.name.length > 256 ||
    typeof v.money !== "string" ||
    parentTestMoney(v.money) !== v.money ||
    typeof v.state !== "string" ||
    ![
      "blocked",
      "prepared",
      "processing",
      "unknown",
      "pending",
      "succeeded",
      "failed",
      "paid",
      "closed",
    ].includes(v.state) ||
    typeof v.checkout_url !== "string" ||
    typeof v.material_type !== "string" ||
    !["", "qrcode", "html", "app", "jsapi", "preauth", "jump"].includes(
      v.material_type,
    ) ||
    typeof v.can_query !== "boolean"
  )
    throw fail();
  if (
    (v.can_query && v.trade_no === "") ||
    (["blocked", "prepared"].includes(v.state) && v.can_query)
  )
    throw fail();
  if (v.checkout_url !== "") {
    if (
      !v.trade_no ||
      !v.material_type ||
      !v.checkout_url.startsWith("/checkout/") ||
      v.checkout_url.startsWith("//")
    )
      throw fail();
    const u = new URL(v.checkout_url, "https://internal.invalid");
    if (
      u.origin !== "https://internal.invalid" ||
      u.pathname !== "/checkout/" + encodeURIComponent(v.trade_no) ||
      u.hash ||
      [...u.searchParams.keys()].length !== 2 ||
      u.searchParams.getAll("expires").length !== 1 ||
      u.searchParams.getAll("token").length !== 1 ||
      !/^\d{10,12}$/.test(u.searchParams.get("expires") || "") ||
      !/^[A-Za-z0-9_-]{43}$/.test(u.searchParams.get("token") || "")
    )
      throw fail();
  }
  return v as unknown as ParentPaymentTest;
}
