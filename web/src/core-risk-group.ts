export interface CoreRiskGroup {
  group_id: string;
  group_name: string;
  configured: boolean;
  revision: string;
  fingerprint: string;
  config: Record<"pay_block_oversea" | "check_pay_regoin", string>;
  effective_oversea: string;
  effective_scan_region: string;
  geo_configured: boolean;
}
export function readCoreRiskGroup(raw: unknown, id: string): CoreRiskGroup {
  const v = raw as CoreRiskGroup;
  if (
    !v ||
    typeof v !== "object" ||
    Array.isArray(v) ||
    Object.keys(v).sort().join() !==
      [
        "group_id",
        "group_name",
        "configured",
        "revision",
        "fingerprint",
        "config",
        "effective_oversea",
        "effective_scan_region",
        "geo_configured",
      ]
        .sort()
        .join() ||
    v.group_id !== id ||
    typeof v.group_name !== "string" ||
    !v.group_name ||
    typeof v.configured !== "boolean" ||
    typeof v.geo_configured !== "boolean" ||
    typeof v.revision !== "string" ||
    !/^(0|[1-9]\d{0,18})$/.test(v.revision) ||
    BigInt(v.revision) > 9223372036854775807n ||
    v.configured !== (v.revision !== "0") ||
    typeof v.fingerprint !== "string" ||
    !/^[0-9a-f]{64}$/.test(v.fingerprint) ||
    !v.config ||
    Object.keys(v.config).sort().join() !==
      ["pay_block_oversea", "check_pay_regoin"].sort().join() ||
    !["", "0", "1"].includes(v.config.pay_block_oversea) ||
    !["", "0", "1", "2"].includes(v.config.check_pay_regoin) ||
    !["0", "1"].includes(v.effective_oversea) ||
    !["0", "1", "2"].includes(v.effective_scan_region)
  )
    throw Error("用户组风控数据无法核对");
  return { ...v, config: { ...v.config } };
}
