import { ApiError } from "./api";
export type WeixinKind = "official_account" | "mini_program";
export type WeixinApplication = {
  id: string;
  revision: string;
  name: string;
  kind: string;
  app_id: string;
  enabled: boolean;
  archived: boolean;
  source_channel_id: string;
  merchant_ids: string[];
  path: string;
  fingerprint: string;
  secret_configured: boolean;
  draft: boolean;
  execution_ready: boolean;
  origin: "managed" | "imported" | "legacy_metadata";
  read_only: boolean;
};
export type WeixinSource = {
  id: string;
  fingerprint: string;
  merchant_ids: string[];
};
export type WeixinConfigurationError = { id: string; message: string };
export type WeixinDraft = {
  kind: WeixinKind;
  name: string;
  app_id: string;
  appsecret: string;
  enabled: boolean;
  source_channel_id: string;
  merchant_ids: string[];
  path: string;
};
export const weixinKinds = [
  { value: "official_account", label: "微信公众号" },
  { value: "mini_program", label: "微信小程序" },
];
export const weixinKindName = (kind: string) =>
  weixinKinds.find((k) => k.value === kind)?.label || "旧类别待核对";
const object = (raw: unknown): Record<string, unknown> => {
  if (!raw || typeof raw !== "object" || Array.isArray(raw))
    throw Error("应用资料格式无法核对");
  return raw as Record<string, unknown>;
};
export function weixinID(raw: unknown, zero = false): string {
  if (
    typeof raw !== "string" ||
    !(zero ? /^(0|[1-9][0-9]{0,18})$/ : /^[1-9][0-9]{0,18}$/).test(raw) ||
    BigInt(raw) > 9223372036854775807n
  )
    throw Error("应用或授权编号无法精确核对");
  return raw;
}
const fingerprint = (raw: unknown, optional = false): string => {
  if (optional && raw === "") return "";
  if (typeof raw !== "string" || !/^[a-f0-9]{64}$/.test(raw))
    throw Error("应用版本无法核对");
  return raw;
};
const text = (raw: unknown, max: number): string => {
  if (
    typeof raw !== "string" ||
    new TextEncoder().encode(raw).length > max ||
    /[\x00\r\n]/.test(raw)
  )
    throw Error("应用资料格式无法核对");
  return raw;
};
function ids(raw: unknown): string[] {
  if (!Array.isArray(raw) || raw.length > 1000)
    throw Error("允许商户范围无法核对");
  const out = raw.map((v) => weixinID(v));
  if (new Set(out).size !== out.length) throw Error("允许商户编号重复");
  return out;
}
export function readWeixinApplication(raw: unknown): WeixinApplication {
  const row = object(raw),
    origin = row.origin;
  if (
    origin !== "managed" &&
    origin !== "imported" &&
    origin !== "legacy_metadata"
  )
    throw Error("应用资料来源无法核对");
  if (
    Object.keys(row).some(
      (key) =>
        /secret|private.?key|access.?token/i.test(key) &&
        key !== "secret_configured",
    )
  )
    throw Error("应用返回中含不应展示的凭证字段");
  for (const key of [
    "enabled",
    "archived",
    "secret_configured",
    "draft",
    "execution_ready",
    "read_only",
  ])
    if (typeof row[key] !== "boolean") throw Error("应用资料状态无法核对");
  const out: WeixinApplication = {
    id: weixinID(row.id),
    revision: weixinID(row.revision, origin !== "managed"),
    name: text(row.name, 1024),
    kind: text(row.kind, 64),
    app_id: text(row.app_id, 256),
    enabled: row.enabled as boolean,
    archived: row.archived as boolean,
    source_channel_id: weixinID(row.source_channel_id, true),
    merchant_ids: ids(row.merchant_ids),
    path: text(row.path, 1024),
    fingerprint: fingerprint(row.fingerprint, origin !== "managed"),
    secret_configured: row.secret_configured as boolean,
    draft: row.draft as boolean,
    execution_ready: row.execution_ready as boolean,
    origin,
    read_only: row.read_only as boolean,
  };
  if (origin !== "managed" && !out.read_only) throw Error("旧资料未标记为只读");
  if (
    origin === "managed" &&
    (!weixinKinds.some((k) => k.value === out.kind) ||
      !out.name ||
      !/^[A-Za-z0-9_-]{1,128}$/.test(out.app_id))
  )
    throw Error("受管应用资料无法核对");
  return out;
}
export function readWeixinApplications(raw: unknown): {
  items: WeixinApplication[];
  can_write: boolean;
  configuration_errors: WeixinConfigurationError[];
} {
  const data = object(raw);
  if (
    typeof data.can_write !== "boolean" ||
    !Array.isArray(data.items) ||
    data.items.length > 10000 ||
    data.total !== data.items.length
  )
    throw Error("应用列表无法核对");
  const items = data.items.map(readWeixinApplication),
    keys = items.map((v) => v.origin + ":" + v.id);
  if (new Set(keys).size !== keys.length) throw Error("同来源应用编号重复");
  const errors =
    data.configuration_errors === undefined ? [] : data.configuration_errors;
  if (!Array.isArray(errors) || errors.length > 1000)
    throw Error("应用异常资料无法核对");
  const configuration_errors = errors.map((raw) => {
    const entry = object(raw);
    return { id: weixinID(entry.id), message: text(entry.message, 256) };
  });
  return { items, can_write: data.can_write, configuration_errors };
}
export function readWeixinCapabilities(raw: unknown): {
  sources: WeixinSource[];
  can_write: boolean;
} {
  const data = object(raw);
  if (
    typeof data.can_write !== "boolean" ||
    !Array.isArray(data.sources) ||
    data.sources.length > 1000
  )
    throw Error("应用关联能力无法核对");
  const sources = data.sources.map((raw) => {
    const row = object(raw);
    return {
      id: weixinID(row.id),
      fingerprint: fingerprint(row.fingerprint),
      merchant_ids: ids(row.merchant_ids),
    };
  });
  if (new Set(sources.map((s) => s.id)).size !== sources.length)
    throw Error("来源通道编号重复");
  return { sources, can_write: data.can_write };
}
export function newWeixinRequestID(): string {
  const bytes = crypto.getRandomValues(new Uint8Array(32));
  return Array.from(bytes, (v) => v.toString(16).padStart(2, "0")).join("");
}
export function emptyWeixinDraft(): WeixinDraft {
  return {
    kind: "official_account",
    name: "",
    app_id: "",
    appsecret: "",
    enabled: false,
    source_channel_id: "0",
    merchant_ids: [],
    path: "",
  };
}
export function weixinDraftError(
  draft: WeixinDraft,
  sources: WeixinSource[],
  previous?: WeixinApplication,
): string {
  if (
    !draft.name.trim() ||
    new TextEncoder().encode(draft.name.trim()).length > 100 ||
    /[\x00\r\n]/.test(draft.name)
  )
    return "名称须为1至100字节";
  if (
    !weixinKinds.some((k) => k.value === draft.kind) ||
    !/^[A-Za-z0-9_-]{1,128}$/.test(draft.app_id)
  )
    return "请核对类别和APPID";
  const secretRequired =
    !previous ||
    previous.kind !== draft.kind ||
    previous.app_id !== draft.app_id;
  if (secretRequired && !draft.appsecret)
    return previous
      ? "更改类别或APPID时必须填写新的APPSECRET"
      : "请填写APPSECRET";
  if (
    draft.appsecret &&
    (new TextEncoder().encode(draft.appsecret).length > 512 ||
      /^@/.test(draft.appsecret) ||
      /\s|\x00/.test(draft.appsecret))
  )
    return "APPSECRET格式无效";
  const source = sources.find((s) => s.id === draft.source_channel_id);
  if (draft.source_channel_id !== "0" && !source)
    return "当前关联来源不可用，请重新选择来源";
  if (
    draft.merchant_ids.some((id) => !source?.merchant_ids.includes(id)) ||
    new Set(draft.merchant_ids).size !== draft.merchant_ids.length
  )
    return "允许商户须来自当前来源的授权范围";
  if (draft.kind === "official_account" && draft.path)
    return "公众号不使用小程序路径";
  if (
    new TextEncoder().encode(draft.path).length > 256 ||
    (draft.path &&
      !draft.path
        .split("/")
        .every((p) => p !== "." && p !== ".." && /^[A-Za-z0-9_.-]+$/.test(p)))
  )
    return "小程序路径须使用有效相对路径";
  return "";
}
export function knownWeixinRejection(error: unknown): error is ApiError {
  return (
    error instanceof ApiError &&
    error.responseDecoded &&
    error.status >= 400 &&
    error.status < 500
  );
}
