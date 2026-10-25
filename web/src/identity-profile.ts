export interface IdentityProfile {
  id: string;
  revision: string;
  channel_id: string;
  provider: "wxpay" | "alipay";
  kind: "web" | "mini";
  app_id: string;
  path: string;
  url_link_available?: boolean;
}
export interface ProfileRequest {
  request_id: string;
  merchant_id: string;
  channel_id: string;
  profile_id: string;
  profile_revision: string;
  provider: "wxpay" | "alipay";
  kind: "web" | "mini";
  app_id: string;
  path: string;
  status: "pending" | "processing" | "verified" | "failed" | "revealed";
  expires_at: string;
}
export interface ProfileSDKLaunch {
  kind: "sdk";
  provider: string;
  app_id: string;
  path: string;
  method: "POST";
  completion_url: string;
  body: { state: string };
  expires_at: string;
}
export const profilePositive = (v: unknown): v is string =>
  typeof v === "string" &&
  /^[1-9][0-9]{0,18}$/.test(v) &&
  BigInt(v) <= 9223372036854775807n;
const text = (v: unknown, max = 128): v is string =>
  typeof v === "string" &&
  v.length > 0 &&
  v.length <= max &&
  !/[\u0000-\u001f\u007f]/.test(v);
function object(v: unknown): Record<string, unknown> {
  if (!v || typeof v !== "object" || Array.isArray(v))
    throw Error("独立授权响应格式错误");
  return v as Record<string, unknown>;
}
export function parseIdentityProfile(raw: unknown): IdentityProfile {
  const v = object(raw);
  if (
    !profilePositive(v.id) ||
    !profilePositive(v.revision) ||
    !profilePositive(v.channel_id) ||
    !["wxpay", "alipay"].includes(String(v.provider)) ||
    !["web", "mini"].includes(String(v.kind)) ||
    !text(v.app_id) ||
    typeof v.path !== "string" ||
    (v.kind === "web" && v.path !== "") ||
    (v.kind === "mini" &&
      (v.path.length > 256 ||
        !/^[-A-Za-z0-9_.]+(?:\/[-A-Za-z0-9_.]+)*$/.test(v.path) ||
        v.path.split("/").some((p) => p === "." || p === ".."))) ||
    "fingerprint" in v ||
    "secret" in v ||
    (v.url_link_available !== undefined &&
      typeof v.url_link_available !== "boolean") ||
    (v.url_link_available === true &&
      (v.provider !== "wxpay" || v.kind !== "mini"))
  )
    throw Error("应用档案格式或命名空间无效");
  return {
    id: v.id,
    revision: v.revision,
    channel_id: v.channel_id,
    provider: v.provider as IdentityProfile["provider"],
    kind: v.kind as IdentityProfile["kind"],
    app_id: v.app_id,
    path: v.path,
    url_link_available: v.url_link_available === true,
  };
}

export interface ProfileIdentityLink {
  purpose: "wechat_mini_identity_url_link";
  request_id: string;
  provider: "wxpay";
  app_id: string;
  status: "creating" | "ready" | "failed" | "unknown";
  created_at: string;
  expires_at: string;
  requested_expires_at: string;
  url_link?: string;
}

function profileLinkTime(value: unknown): number {
  if (
    typeof value !== "string" ||
    value.length > 64 ||
    !/^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(?:\.\d{1,9})?(?:Z|[+-]\d{2}:\d{2})$/.test(
      value,
    )
  )
    throw Error("URL Link时间格式错误");
  const parsed = Date.parse(value);
  if (!Number.isFinite(parsed)) throw Error("URL Link时间格式错误");
  return parsed;
}

// Check raw authority/path before URL normalization can erase empty user-info,
// default ports or dot segments. Return only the actual validated provider URL.
function profileVendorLink(value: unknown): string {
  if (
    typeof value !== "string" ||
    value.length > 2048 ||
    /[\p{White_Space}\p{Cc}\\]/u.test(value)
  )
    throw Error("URL Link厂商地址无效");
  const match =
    /^https:\/\/(wxaurl\.cn|wxmpurl\.cn)(\/[^?#]*)(?:\?([^#]*))?$/.exec(value);
  if (!match || match[3] === "") throw Error("URL Link厂商地址无效");
  let path: string;
  try {
    path = decodeURIComponent(match[2]!);
    encodeURI(path);
  } catch {
    throw Error("URL Link厂商地址无效");
  }
  if (
    !path.replaceAll("/", "") ||
    new TextEncoder().encode(path).length > 1024 ||
    /[\p{White_Space}\p{Cc}\\]/u.test(path) ||
    path.split("/").some((part) => part === "." || part === "..")
  )
    throw Error("URL Link厂商地址无效");
  if (match[3] !== undefined) {
    const rawQuery = match[3];
    if (/%(?![0-9a-f]{2})/i.test(rawQuery) || rawQuery.includes(";"))
      throw Error("URL Link厂商参数无效");
    const values = [...new URLSearchParams(rawQuery)];
    if (
      values.length !== 1 ||
      values[0]![0] !== "cq" ||
      !/^[A-Za-z0-9!#$&'()*+,/:;=?@._~%`-]{1,256}$/.test(values[0]![1])
    )
      throw Error("URL Link厂商参数无效");
  }
  return value;
}

export function parseProfileIdentityLink(
  raw: unknown,
  q: ProfileRequest,
  previous?: ProfileIdentityLink,
): ProfileIdentityLink {
  const v = object(raw);
  const allowed = [
    "purpose",
    "request_id",
    "provider",
    "app_id",
    "status",
    "created_at",
    "expires_at",
    "requested_expires_at",
    "url_link",
  ];
  const created = profileLinkTime(v.created_at),
    expires = profileLinkTime(v.expires_at),
    requested = profileLinkTime(v.requested_expires_at),
    originalExpiry = profileLinkTime(q.expires_at);
  if (
    Object.keys(v).some((k) => !allowed.includes(k)) ||
    q.provider !== "wxpay" ||
    q.kind !== "mini" ||
    v.purpose !== "wechat_mini_identity_url_link" ||
    v.request_id !== q.request_id ||
    v.provider !== q.provider ||
    v.app_id !== q.app_id ||
    !["creating", "ready", "failed", "unknown"].includes(String(v.status)) ||
    expires !== originalExpiry ||
    requested !== Math.floor(originalExpiry / 1000) * 1000 ||
    created < originalExpiry - 300000 ||
    created > Date.now() + 10000 ||
    created >= requested ||
    originalExpiry <= Date.now() ||
    (previous &&
      (v.created_at !== previous.created_at ||
        v.expires_at !== previous.expires_at ||
        v.requested_expires_at !== previous.requested_expires_at))
  )
    throw Error("URL Link并非当前原始请求或期限不一致");
  const link = v.status === "ready" ? profileVendorLink(v.url_link) : undefined;
  if (v.status !== "ready" && "url_link" in v)
    throw Error("URL Link未就绪却返回了地址");
  return {
    purpose: "wechat_mini_identity_url_link",
    request_id: q.request_id,
    provider: "wxpay",
    app_id: q.app_id,
    status: v.status as ProfileIdentityLink["status"],
    created_at: v.created_at as string,
    expires_at: v.expires_at as string,
    requested_expires_at: v.requested_expires_at as string,
    ...(link ? { url_link: link } : {}),
  };
}
export function parseProfileRequest(
  raw: unknown,
  p: IdentityProfile,
  mid: string,
): ProfileRequest {
  const v = object(raw);
  if (
    !/^[a-f0-9]{64}$/.test(String(v.request_id)) ||
    v.merchant_id !== mid ||
    v.channel_id !== p.channel_id ||
    v.profile_id !== p.id ||
    v.profile_revision !== p.revision ||
    v.provider !== p.provider ||
    v.kind !== p.kind ||
    v.app_id !== p.app_id ||
    v.path !== p.path ||
    !["pending", "processing", "verified", "failed", "revealed"].includes(
      String(v.status),
    ) ||
    typeof v.expires_at !== "string" ||
    !Number.isFinite(Date.parse(v.expires_at)) ||
    Date.parse(v.expires_at) > Date.now() + 310000 ||
    ["subject_id", "state", "fingerprint", "authorization_token"].some(
      (k) => k in v,
    )
  )
    throw Error("原请求应用、版本或商户不一致");
  return {
    request_id: String(v.request_id),
    merchant_id: mid,
    channel_id: p.channel_id,
    profile_id: p.id,
    profile_revision: p.revision,
    provider: p.provider,
    kind: p.kind,
    app_id: p.app_id,
    path: p.path,
    status: v.status as ProfileRequest["status"],
    expires_at: v.expires_at,
  };
}
export function parseProfileLaunch(
  raw: unknown,
  q: ProfileRequest,
): ProfileSDKLaunch {
  const v = object(raw),
    body = object(v.body);
  const u = new URL(String(v.completion_url));
  if (
    q.kind !== "mini" ||
    v.kind !== "sdk" ||
    v.provider !== q.provider ||
    v.app_id !== q.app_id ||
    v.path !== q.path ||
    v.method !== "POST" ||
    v.expires_at !== q.expires_at ||
    Object.keys(body).length !== 1 ||
    !text(body.state, 64) ||
    !/^[a-f0-9]{64}$/.test(body.state) ||
    u.protocol !== "https:" ||
    u.username ||
    u.password ||
    u.search ||
    u.hash ||
    u.pathname !==
      `/identity/profile/mini/${q.provider}/${q.profile_id}/${q.request_id}/`
  )
    throw Error("SDK资料并非当前独立查询用途");
  return {
    kind: "sdk",
    provider: q.provider,
    app_id: q.app_id,
    path: q.path,
    method: "POST",
    completion_url: u.href,
    body: { state: body.state },
    expires_at: q.expires_at,
  };
}
