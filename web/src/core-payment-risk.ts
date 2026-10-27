export interface RiskField {
  key: string;
  label: string;
  kind: "integer" | "money" | "ips" | "regions" | "ids" | "flag" | "scan";
  hint?: string;
  parent?: string;
}
export const coreRiskFields: RiskField[] = [
  {
    key: "pay_iplimit",
    label: "IP 每日支付次数",
    kind: "integer",
    hint: "0 或留空关闭。按可信成功日期统计；未决首次发送占用并发名额。",
  },
  {
    key: "pay_ip_interval",
    label: "IP 支付间隔（秒）",
    kind: "integer",
    hint: "按首次发送准入时刻计算，重复查询不会重新计时。",
  },
  {
    key: "pay_iplimit_white",
    label: "IP 频率限制白名单",
    kind: "ips",
    hint: "精确 IPv4/IPv6，以 | 分隔；仅豁免 IP 次数和间隔，不豁免地区、黑名单。",
  },
  {
    key: "pay_userlimit",
    label: "买家每日支付次数",
    kind: "integer",
    hint: "支付宝、微信已知支付账号；按当天确认成功次数。",
  },
  {
    key: "pay_daymoney",
    label: "买家每日累计支付金额（元）",
    kind: "money",
    hint: "已有实付累计达到阈值后拒绝下一笔；本笔可以跨过阈值。",
  },
  {
    key: "pay_region_block",
    label: "禁止支付地区",
    kind: "regions",
    hint: "省、市完整名称，以 | 分隔；仅使用配置的本地地址库。",
  },
  { key: "pay_block_oversea", label: "禁止海外支付", kind: "flag" },
  {
    key: "check_pay_regoin",
    label: "扫码IP与下单IP地区一致性校验",
    kind: "scan",
    hint: "仅支付宝、微信 HTTP(S) 二维码及 WAP 中转；比较原下单省市和当前可信扫码省市。新订单冻结策略；地址库无法核实时显示错误。",
  },
  { key: "auto_check_payip", label: "自动检查 IP 连续未付", kind: "flag" },
  {
    key: "check_payip_second",
    label: "IP 连续未付统计窗口（秒）",
    kind: "integer",
    parent: "auto_check_payip",
  },
  {
    key: "check_payip_count",
    label: "IP 连续未付订单数",
    kind: "integer",
    parent: "auto_check_payip",
    hint: "窗口内订单数达到此值且无成功订单时封禁 5 天。",
  },
  { key: "auto_check_payspeed", label: "自动检查买家支付频率", kind: "flag" },
  {
    key: "check_payspeed_second",
    label: "买家频率统计窗口（秒）",
    kind: "integer",
    parent: "auto_check_payspeed",
  },
  {
    key: "check_payspeed_count",
    label: "买家频率成功订单数",
    kind: "integer",
    parent: "auto_check_payspeed",
  },
  {
    key: "check_payspeed_days",
    label: "买家封禁天数",
    kind: "integer",
    parent: "auto_check_payspeed",
    hint: "0 为永久。关闭自动检查不会解除既有封禁。",
  },
  { key: "auto_check_sucrate", label: "自动检查商户成功率", kind: "flag" },
  {
    key: "check_sucrate_second",
    label: "成功率统计窗口（秒）",
    kind: "integer",
    parent: "auto_check_sucrate",
  },
  {
    key: "check_sucrate_count",
    label: "成功率最少订单数",
    kind: "integer",
    parent: "auto_check_sucrate",
  },
  {
    key: "check_sucrate_value",
    label: "商户成功率阈值（%）",
    kind: "money",
    parent: "auto_check_sucrate",
    hint: "订单数达到门槛且成功率低于此值时停止新付款。",
  },
  {
    key: "check_sucrate_notice",
    label: "成功率异常消息提醒",
    kind: "flag",
    parent: "auto_check_sucrate",
  },
  { key: "auto_check_channel", label: "自动检查通道连续未付", kind: "flag" },
  {
    key: "check_channel_second",
    label: "通道连续未付统计窗口（秒）",
    kind: "integer",
    parent: "auto_check_channel",
  },
  {
    key: "check_channel_failcount",
    label: "通道连续未付订单数",
    kind: "integer",
    parent: "auto_check_channel",
  },
  {
    key: "check_channel_ids",
    label: "检查通道 ID",
    kind: "ids",
    parent: "auto_check_channel",
    hint: "留空检查全部；多个完整 ID 以逗号分隔。",
  },
  {
    key: "check_channel_notice",
    label: "通道异常消息提醒",
    kind: "flag",
    parent: "auto_check_channel",
  },
];
export interface CoreRiskPolicy {
  configured: boolean;
  revision: string;
  fingerprint: string;
  geo_configured: boolean;
  settings: Record<string, string>;
}
export interface CoreRiskCheckResult {
  external_no: string;
  policy_revision: string;
  actions: number;
  scanned: number;
  at: string;
}
export function readCoreRiskCheckResult(
  raw: unknown,
  key: string,
  revision: string,
): CoreRiskCheckResult {
  const o = object(raw);
  if (
    Object.keys(o).sort().join() !==
      ["external_no", "policy_revision", "actions", "scanned", "at"]
        .sort()
        .join() ||
    o.external_no !== key ||
    o.policy_revision !== revision ||
    !Number.isSafeInteger(o.actions) ||
    !Number.isSafeInteger(o.scanned) ||
    (o.actions as number) < 0 ||
    (o.actions as number) > 1000000 ||
    (o.scanned as number) < 0 ||
    (o.scanned as number) > 100000 ||
    typeof o.at !== "string" ||
    !/^\d{4}-\d\d-\d\dT\d\d:\d\d:\d\d(?:\.\d+)?(?:Z|[+-]\d\d:\d\d)$/.test(
      o.at,
    ) ||
    !Number.isFinite(Date.parse(o.at))
  )
    throw Error("原检查结果无法核对");
  return o as unknown as CoreRiskCheckResult;
}
function object(v: unknown): Record<string, unknown> {
  if (!v || typeof v !== "object" || Array.isArray(v))
    throw Error("风控数据格式不正确");
  return v as Record<string, unknown>;
}
export function readCoreRiskPolicy(v: unknown): CoreRiskPolicy {
  const o = object(v);
  if (
    Object.keys(o).sort().join() !==
      ["configured", "revision", "fingerprint", "geo_configured", "settings"]
        .sort()
        .join() ||
    typeof o.configured !== "boolean" ||
    typeof o.geo_configured !== "boolean" ||
    typeof o.revision !== "string" ||
    !/^(0|[1-9]\d{0,18})$/.test(o.revision) ||
    BigInt(o.revision) > 9223372036854775807n ||
    typeof o.fingerprint !== "string" ||
    !/^[0-9a-f]{64}$/.test(o.fingerprint)
  )
    throw Error("风控数据格式不正确");
  const s = object(o.settings);
  if (
    Object.keys(s).sort().join() !==
      coreRiskFields
        .map((f) => f.key)
        .sort()
        .join() ||
    Object.values(s).some((v) => typeof v !== "string" || v.length > 8192)
  )
    throw Error("风控字段格式不正确");
  if (
    o.configured !== (o.revision !== "0") ||
    !validCoreRiskDraft(s as Record<string, string>)
  )
    throw Error("风控字段格式不正确");
  return { ...o, settings: { ...s } } as unknown as CoreRiskPolicy;
}
export function validCoreRiskDraft(settings: Record<string, string>) {
  try {
    canonicalCoreRiskDraft(settings);
    return true;
  } catch {
    return false;
  }
}
export function canonicalCoreRiskDraft(
  settings: Record<string, string>,
): Record<string, string> {
  const out: Record<string, string> = {};
  coreRiskFields.forEach((f) => {
    const v = settings[f.key];
    if (typeof v !== "string" || v.length > 8192 || /[\x00\r\n]/.test(v))
      throw Error("风控字段格式不正确");
    if (f.kind === "scan") {
      if (!["0", "1", "2"].includes(v)) throw Error("扫码校验选项不正确");
      out[f.key] = v;
      return;
    }
    if (f.kind === "flag") {
      if (v !== "0" && v !== "1") throw Error("开关格式不正确");
      out[f.key] = v;
      return;
    }
    if (v === "") {
      out[f.key] = "";
      return;
    }
    if (f.kind === "integer") {
      const max = f.key.endsWith("days")
        ? 3650
        : f.key.endsWith("second") || f.key === "pay_ip_interval"
          ? 31536000
          : 1000000;
      if (!/^(0|[1-9]\d{0,7})$/.test(v) || BigInt(v) > BigInt(max))
        throw Error("数值超出范围");
      out[f.key] = v === "0" ? "" : v;
      return;
    }
    if (f.kind === "money") {
      if (!/^(0|[1-9]\d{0,9})(\.\d{1,2})?$/.test(v))
        throw Error("金额格式不正确");
      const [whole, fraction = ""] = v.split("."),
        cents = BigInt(whole!) * 100n + BigInt(fraction.padEnd(2, "0"));
      if (cents > (f.key === "check_sucrate_value" ? 10000n : 999999999999n))
        throw Error("数值超出范围");
      out[f.key] =
        cents === 0n
          ? ""
          : `${cents / 100n}.${String(cents % 100n).padStart(2, "0")}`;
      return;
    }
    const sep = f.kind === "ids" ? "," : "|",
      parts = v.split(sep).map((p) => p.trim());
    if (parts.length > 128 || parts.some((p) => !p))
      throw Error("列表格式不正确");
    const normalized = parts.map((p) => {
      if (f.kind === "ids") {
        if (!/^[1-9]\d{0,18}$/.test(p) || BigInt(p) > 9223372036854775807n)
          throw Error("通道ID格式不正确");
        return p;
      }
      if (f.kind === "ips") {
        if (!/^[0-9a-fA-F:.]+$/.test(p)) throw Error("IP格式不正确");
        if (!p.includes(":")) {
          if (
            p.split(".").length !== 4 ||
            p
              .split(".")
              .some((x) => !/^(0|[1-9]\d{0,2})$/.test(x) || Number(x) > 255)
          )
            throw Error("IP格式不正确");
          return p;
        }
        const host = new URL(`http://[${p}]/`).hostname.slice(1, -1);
        const mapped = /^::ffff:([0-9a-f]+):([0-9a-f]+)$/.exec(host);
        if (mapped) {
          const x = parseInt(mapped[1]!, 16),
            y = parseInt(mapped[2]!, 16);
          return `${x >> 8}.${x & 255}.${y >> 8}.${y & 255}`;
        }
        return host;
      }
      if ([...p].length > 128) throw Error("地区名称过长");
      return p;
    });
    out[f.key] = [...new Set(normalized)].sort().join(sep);
  });
  for (const [flag, required] of [
    ["auto_check_payip", ["check_payip_second", "check_payip_count"]],
    ["auto_check_payspeed", ["check_payspeed_second", "check_payspeed_count"]],
    [
      "auto_check_sucrate",
      ["check_sucrate_second", "check_sucrate_count", "check_sucrate_value"],
    ],
    ["auto_check_channel", ["check_channel_second", "check_channel_failcount"]],
  ] as const) {
    if (out[flag] === "1" && required.some((k) => !out[k]))
      throw Error("启用检查需配置正数门槛");
  }
  return out;
}
