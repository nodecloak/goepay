export interface CoreRiskStatus {
  blocked: boolean;
  reason: string;
  until: string | null;
  action_id: string;
  revision: string;
}
export function readCoreRiskStatus(v: unknown): CoreRiskStatus {
  if (!v || typeof v !== "object" || Array.isArray(v))
    throw Error("风控有效状态无法核实");
  const o = v as Record<string, unknown>,
    id = (v: unknown) =>
      typeof v === "string" &&
      /^(0|[1-9]\d{0,18})$/.test(v) &&
      BigInt(v) <= 9223372036854775807n;
  if (
    Object.keys(o).sort().join() !==
      ["blocked", "reason", "until", "action_id", "revision"].sort().join() ||
    typeof o.blocked !== "boolean" ||
    typeof o.reason !== "string" ||
    o.reason.length > 128 ||
    /[\x00\r\n]/.test(o.reason) ||
    !id(o.action_id) ||
    !id(o.revision) ||
    (o.until !== null &&
      (typeof o.until !== "string" ||
        !/^\d{4}-\d\d-\d\dT\d\d:\d\d:\d\d(?:\.\d+)?(?:Z|[+-]\d\d:\d\d)$/.test(
          o.until,
        ) ||
        !Number.isFinite(Date.parse(o.until)))) ||
    (o.blocked &&
      (o.action_id === "0" || o.revision === "0" || o.reason === "")) ||
    (!o.blocked && (o.reason !== "" || o.until !== null))
  )
    throw Error("风控有效状态无法核实");
  return o as unknown as CoreRiskStatus;
}
export function effectiveRiskDescription(
  raw: unknown,
  configuredEnabled: boolean,
  pay?: unknown,
) {
  const reasons: string[] = [];
  if (!configuredEnabled) reasons.push("配置已关闭");
  if (pay !== undefined && String(pay) !== "1")
    reasons.push(String(pay) === "2" ? "支付权限待审核" : "支付权限已关闭");
  try {
    const status = readCoreRiskStatus(raw);
    if (status.blocked)
      reasons.push(
        status.reason +
          (status.until ? `（至 ${status.until}）` : "（待管理员解除）"),
      );
    return reasons.length
      ? "新付款受限：" + reasons.join("；")
      : "核心风控未阻断；其它权限、黑名单及限额仍需通过";
  } catch {
    return (
      (reasons.length ? "新付款受限：" + reasons.join("；") + "；" : "") +
      "风控有效状态待核对"
    );
  }
}
