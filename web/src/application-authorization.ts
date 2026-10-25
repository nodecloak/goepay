export type ApplicationAuthorizationProfile = {
  id: string;
  revision: string;
  issuer_app_id: string;
  expected_auth_app_id: string;
  launch_kind: "basic" | "assigned";
  app_types: string[];
};
export type ApplicationAuthorizationRequest = {
  request_id: string;
  purpose: "alipay_app_authorization";
  merchant_id: string;
  profile_id: string;
  profile_revision: string;
  issuer_app_id: string;
  expected_auth_app_id: string;
  launch_kind: "basic" | "assigned";
  app_types: string[];
  status: "pending" | "processing" | "verified" | "failed" | "revealed";
  expires_at: string;
};
export type ApplicationAuthorizationResult = {
  provider: "alipay";
  issuer_app_id: string;
  auth_app_id: string;
  user_id: string;
  app_auth_token: string;
  token_expires_in_seconds: string;
};
const failure = () => Error("应用授权返回格式不匹配，请清除后重新核对");
export const applicationAuthorizationID = (v: unknown): v is string =>
  typeof v === "string" &&
  /^[1-9][0-9]{0,18}$/.test(v) &&
  BigInt(v) <= 9223372036854775807n;
const appID = (v: unknown): v is string =>
  typeof v === "string" && /^[0-9]{16}$/.test(v);
const hex = (v: unknown): v is string =>
  typeof v === "string" && /^[a-f0-9]{64}$/.test(v);
function object(v: unknown): Record<string, any> {
  if (!v || typeof v !== "object" || Array.isArray(v)) throw failure();
  return v as Record<string, any>;
}
function keys(v: Record<string, any>, allowed: string[]) {
  if (Object.keys(v).some((k) => !allowed.includes(k))) throw failure();
}
function appTypes(
  kind: unknown,
  value: unknown,
  nullMeansNone = false,
): string[] {
  if (kind !== "basic" && kind !== "assigned") throw failure();
  const values =
    value === undefined || (nullMeansNone && value === null) ? [] : value;
  if (
    !Array.isArray(values) ||
    values.length > 5 ||
    (kind === "basic" && values.length !== 0) ||
    (kind === "assigned" && values.length < 1) ||
    new Set(values).size !== values.length ||
    values.some(
      (v) =>
        !["MOBILEAPP", "WEBAPP", "PUBLICAPP", "TINYAPP", "BASEAPP"].includes(v),
    )
  )
    throw failure();
  return [...values];
}
function profile(value: unknown): ApplicationAuthorizationProfile {
  const v = object(value);
  keys(v, [
    "id",
    "revision",
    "issuer_app_id",
    "expected_auth_app_id",
    "launch_kind",
    "app_types",
  ]);
  if (
    !applicationAuthorizationID(v.id) ||
    !applicationAuthorizationID(v.revision) ||
    !appID(v.issuer_app_id) ||
    !appID(v.expected_auth_app_id)
  )
    throw failure();
  return {
    id: v.id,
    revision: v.revision,
    issuer_app_id: v.issuer_app_id,
    expected_auth_app_id: v.expected_auth_app_id,
    launch_kind: v.launch_kind,
    app_types: appTypes(v.launch_kind, v.app_types),
  };
}
function authorizationOrigin(value: unknown): string {
  if (
    typeof value !== "string" ||
    value.length > 2048 ||
    !/^https:\/\/[^/?#@\\\s]+\/?$/.test(value)
  )
    throw failure();
  let u: URL;
  try {
    u = new URL(value);
  } catch {
    throw failure();
  }
  if (
    u.protocol !== "https:" ||
    !u.hostname ||
    u.username ||
    u.password ||
    u.search ||
    u.hash ||
    u.pathname !== "/" ||
    (u.port && Number(u.port) < 1)
  )
    throw failure();
  return u.origin;
}
export function parseApplicationAuthorizationCapabilities(
  value: unknown,
  merchantID: string,
) {
  const v = object(value);
  keys(v, [
    "merchant_id",
    "available",
    "reason",
    "profiles",
    "authorization_origin",
  ]);
  if (
    !applicationAuthorizationID(merchantID) ||
    v.merchant_id !== merchantID ||
    typeof v.available !== "boolean" ||
    typeof v.reason !== "string" ||
    v.reason.length > 1000 ||
    /[\u0000-\u001f\u007f]/.test(v.reason) ||
    !Array.isArray(v.profiles) ||
    v.profiles.length > 256
  )
    throw failure();
  const profiles = v.profiles.map(profile);
  if (
    new Set(profiles.map((p) => p.id)).size !== profiles.length ||
    v.available !== profiles.length > 0
  )
    throw failure();
  const origin = v.available ? authorizationOrigin(v.authorization_origin) : "";
  if (!v.available && v.authorization_origin !== "") throw failure();
  return {
    merchant_id: merchantID,
    available: v.available,
    reason: v.reason as string,
    authorization_origin: origin,
    profiles,
  };
}
export function parseApplicationAuthorizationRequest(
  value: unknown,
  expected: ApplicationAuthorizationProfile,
  merchantID: string,
  previous?: ApplicationAuthorizationRequest,
): ApplicationAuthorizationRequest {
  const v = object(value);
  keys(v, [
    "request_id",
    "purpose",
    "merchant_id",
    "profile_id",
    "profile_revision",
    "issuer_app_id",
    "expected_auth_app_id",
    "launch_kind",
    "app_types",
    "status",
    "expires_at",
  ]);
  const types = appTypes(v.launch_kind, v.app_types, true);
  if (
    !hex(v.request_id) ||
    v.purpose !== "alipay_app_authorization" ||
    v.merchant_id !== merchantID ||
    !applicationAuthorizationID(merchantID) ||
    v.profile_id !== expected.id ||
    v.profile_revision !== expected.revision ||
    v.issuer_app_id !== expected.issuer_app_id ||
    v.expected_auth_app_id !== expected.expected_auth_app_id ||
    v.launch_kind !== expected.launch_kind ||
    types.join(",") !== expected.app_types.join(",") ||
    !["pending", "processing", "verified", "failed", "revealed"].includes(
      v.status,
    ) ||
    typeof v.expires_at !== "string" ||
    !Number.isFinite(Date.parse(v.expires_at)) ||
    Date.parse(v.expires_at) > Date.now() + 310000 ||
    (previous &&
      (v.request_id !== previous.request_id ||
        v.expires_at !== previous.expires_at))
  )
    throw failure();
  return {
    request_id: v.request_id,
    purpose: "alipay_app_authorization",
    merchant_id: merchantID,
    profile_id: expected.id,
    profile_revision: expected.revision,
    issuer_app_id: expected.issuer_app_id,
    expected_auth_app_id: expected.expected_auth_app_id,
    launch_kind: expected.launch_kind,
    app_types: types,
    status: v.status,
    expires_at: v.expires_at,
  };
}
function launch(
  value: unknown,
  kind: "authorize" | "wallet",
  expectedOrigin: string,
) {
  if (
    typeof value !== "string" ||
    value.length > 2048 ||
    // Check the raw authority before URL drops empty user-info (https://@host).
    !/^https:\/\/[^/?#@\\\s]+\//.test(value) ||
    /[\u0000-\u0020\u007f\\?#]/.test(value)
  )
    throw failure();
  let u: URL;
  try {
    u = new URL(value);
  } catch {
    throw failure();
  }
  if (
    u.protocol !== "https:" ||
    !u.hostname ||
    u.username ||
    u.password ||
    u.search ||
    u.hash ||
    (expectedOrigin && u.origin !== expectedOrigin) ||
    !new RegExp(`^/identity/application/${kind}/[a-f0-9]{64}$`).test(u.pathname)
  )
    throw failure();
  return u.href;
}
export function parseApplicationAuthorizationCreated(
  value: unknown,
  expected: ApplicationAuthorizationProfile,
  merchantID: string,
  expectedOrigin: string,
) {
  const v = object(value);
  keys(v, ["request", "authorization_url", "wallet_authorization_url"]);
  const request = parseApplicationAuthorizationRequest(
    v.request,
    expected,
    merchantID,
  );
  if (
    request.status !== "pending" ||
    Date.parse(request.expires_at) <= Date.now()
  )
    throw failure();
  const browserURL = launch(
    v.authorization_url,
    "authorize",
    authorizationOrigin(expectedOrigin),
  );
  let walletURL = "";
  if (expected.launch_kind === "assigned") {
    walletURL = launch(
      v.wallet_authorization_url,
      "wallet",
      new URL(browserURL).origin,
    );
    if (walletURL.split("/").at(-1) !== browserURL.split("/").at(-1))
      throw failure();
  } else if ("wallet_authorization_url" in v) throw failure();
  return { request, browserURL, walletURL };
}
export function parseApplicationAuthorizationResult(
  value: unknown,
  expected: ApplicationAuthorizationRequest,
): ApplicationAuthorizationResult {
  const v = object(value);
  keys(v, [
    "provider",
    "issuer_app_id",
    "auth_app_id",
    "user_id",
    "app_auth_token",
    "token_expires_in_seconds",
  ]);
  if (
    v.provider !== "alipay" ||
    v.issuer_app_id !== expected.issuer_app_id ||
    v.auth_app_id !== expected.expected_auth_app_id ||
    typeof v.user_id !== "string" ||
    !/^2088[0-9]{12}$/.test(v.user_id) ||
    typeof v.app_auth_token !== "string" ||
    v.app_auth_token.length < 1 ||
    v.app_auth_token.length > 4096 ||
    v.app_auth_token.startsWith("@") ||
    /[^\x21-\x7e]/.test(v.app_auth_token) ||
    typeof v.token_expires_in_seconds !== "string" ||
    !/^(0|[1-9][0-9]{0,18})$/.test(v.token_expires_in_seconds) ||
    BigInt(v.token_expires_in_seconds) > 9223372036854775807n
  )
    throw failure();
  return {
    provider: "alipay",
    issuer_app_id: expected.issuer_app_id,
    auth_app_id: expected.expected_auth_app_id,
    user_id: v.user_id,
    app_auth_token: v.app_auth_token,
    token_expires_in_seconds: v.token_expires_in_seconds,
  };
}
