export interface PaymentPasswordStatus {
  configured: boolean;
  enforced: boolean;
  credential_epoch: number;
  totp_required: boolean;
}
export function needsFundsAuthorization(
  target: string,
  enforced: boolean,
): boolean {
  return (
    enforced ||
    /^\/console\/v1\/admin\/channel-stops\/[1-9]\d*\/resume$/.test(target) ||
    /^\/console\/v1\/admin\/orders\/[A-Za-z0-9_-]+\/(?:adopt-income-hold|release-remaining-authorization)$/.test(
      target,
    ) ||
    /^\/console\/v1\/admin\/settlement-plans\/[1-9]\d*\/(?:authorizations|auto-authorizations)$/.test(
      target,
    )
  );
}
export function isAdminFundsAction(path: string, method: string): boolean {
  if (method !== "POST") return false;
  const [pathname, query] = path.split("?");
  if (query !== undefined || !pathname) return false;
  return (
    /^\/channel-stops\/[1-9]\d*\/resume$/.test(pathname) ||
    /^\/settlement-plans\/[1-9]\d*\/(?:authorizations|auto-authorizations)$/.test(
      pathname,
    ) ||
    /^\/referral-policies\/(?:global\/0|group\/[1-9]\d*)$/.test(pathname) ||
    /^\/membership-referral-policies\/(?:global\/0|group\/[1-9]\d*)$/.test(
      pathname,
    ) ||
    /^\/channel-sharing\/(?:orders|returns|finishes)$/.test(pathname) ||
    /^\/channel-sharing\/rules\/[1-9]\d*(?:\/disable)?$/.test(pathname) ||
    /^\/channel-sharing\/receivers\/[1-9]\d*\/(?:bind|unbind)$/.test(
      pathname,
    ) ||
    /^\/(?:transfers|transfer-batches)$/.test(pathname) ||
    /^\/transfer-batches\/[1-9]\d*\/(?:execute|cancel-unsent)$/.test(
      pathname,
    ) ||
    /^\/settlement-payouts\/[1-9]\d*\/(?:execute|cancel-unsent)$/.test(
      pathname,
    ) ||
    /^\/orders\/[A-Za-z0-9_-]+\/(?:refund|close|capture-authorization|release-authorization|release-remaining-authorization|freeze-income|unfreeze-income|adopt-income-hold)$/.test(
      pathname,
    ) ||
    /^\/workflows\/(?:settlements|internal-transfers|profitsharing|memberships|deposits)$/.test(
      pathname,
    ) ||
    /^\/workflows\/(?:settlements|internal-transfers|profitsharing|deposits)\/[1-9]\d*\/(?:approve|reject)$/.test(
      pathname,
    ) ||
    /^\/workflows\/profitsharing-rules\/[1-9]\d*\/execute$/.test(pathname)
  );
}
export function readPaymentPasswordStatus(raw: unknown): PaymentPasswordStatus {
  if (!raw || typeof raw !== "object")
    throw new Error("管理员资金安全状态读取失败，请重新查询");
  const value = raw as Record<string, unknown>;
  if (
    typeof value.configured !== "boolean" ||
    typeof value.enforced !== "boolean" ||
    typeof value.totp_required !== "boolean" ||
    !Number.isSafeInteger(value.credential_epoch) ||
    Number(value.credential_epoch) < 0
  )
    throw new Error("管理员资金安全状态响应不完整");
  return value as unknown as PaymentPasswordStatus;
}
export function validateAuthorizationTarget(target: string): string {
  if (
    !/^\/console\/v1\/admin\/(?:[A-Za-z0-9_-]+\/)*[A-Za-z0-9_-]+$/.test(target)
  )
    throw new Error("资金操作目标无效，请重新核对原业务");
  return target.slice("/console/v1/admin".length);
}
export function readAuthorizationDeadline(raw: unknown): number {
  const text = (raw as Record<string, unknown> | null)?.expires_at;
  const deadline = typeof text === "string" ? Date.parse(text) : NaN;
  if (!Number.isFinite(deadline) || deadline <= Date.now())
    throw new Error("资金授权已过期或截止时间无效，请重新预览");
  return deadline;
}
export function snapshotFundsBody(body: unknown): Record<string, unknown> {
  if (!body || typeof body !== "object" || Array.isArray(body))
    throw new Error("原业务参数必须为 JSON 对象");
  return JSON.parse(JSON.stringify(body));
}
