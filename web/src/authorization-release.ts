export interface AuthorizationRelease extends Record<string, unknown> {
  state: string;
  sent: boolean;
  request_no: string;
}
export function canCaptureAuthorization(
  allowed: unknown,
  status: unknown,
  authorization: unknown,
): boolean {
  if (
    allowed !== true ||
    status !== "pending" ||
    !authorization ||
    typeof authorization !== "object"
  )
    return false;
  const auth = authorization as Record<string, unknown>;
  return auth.capture_state === "none" && auth.release_state === "none";
}
export function canReleaseAuthorization(
  allowed: unknown,
  orderStatus: unknown,
  release: AuthorizationRelease | null,
): boolean {
  if (allowed !== true || orderStatus === "paid") return false;
  if (release?.source_revision !== undefined && release.source_revision !== "0")
    return false;
  return (
    !release ||
    (release.sent === false &&
      ["pending", "reversal_pending"].includes(release.state))
  );
}
export function authorizationReleaseNotice(
  data: unknown,
  httpStatus?: number,
): string {
  const release = data as { state?: string } | null;
  if (httpStatus === 202)
    return "授权解除结果尚未核实。请保留原解除请求号，只查询原请求；不得重新创建解除意图。尚不能确认买家冻结已解除。";
  if (httpStatus === 200 && release?.state === "succeeded")
    return "已核实买家预授权冻结已解除。此操作不是商户退款，也不会增加商户余额。";
  if (httpStatus === 200 && release?.state === "failed")
    return "通道已拒绝授权解除，未确认买家冻结已解除。请查看原请求记录，不要另行创建解除请求。";
  return "授权解除结果尚未核实，请查询原解除请求，不能据此认定买家冻结已解除。";
}
