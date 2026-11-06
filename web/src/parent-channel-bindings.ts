export interface ParentWeixinOption {
  id: string;
  revision: string;
  kind: "web" | "mini";
  appid: string;
}
export interface ParentWeixinCatalog {
  channel_id: string;
  source_channel_id: string;
  fingerprint: string;
  items: ParentWeixinOption[];
  total: number;
}
export interface ParentWeixinDraft {
  appwxmp?: string;
  appwxa?: string;
  expected_bindings_fingerprint: string;
}
export interface ParentWeixinBindingSummary {
  id: string;
  revision: string;
  appid: string;
  current: boolean;
}
export interface ParentWeixinBindingState {
  web?: ParentWeixinBindingSummary;
  mini?: ParentWeixinBindingSummary;
}
const invalid = () => new Error("微信应用关联资料无法核实，请刷新后重试");
function object(v: unknown): Record<string, unknown> {
  if (!v || typeof v !== "object" || Array.isArray(v)) throw invalid();
  return v as Record<string, unknown>;
}
function exact(v: Record<string, unknown>, keys: string[]) {
  if (
    Object.keys(v).length !== keys.length ||
    keys.some((k) => !Object.hasOwn(v, k))
  )
    throw invalid();
}
export function parentBindingID(v: unknown, zero = false): v is string {
  return (
    typeof v === "string" &&
    ((zero && v === "0") ||
      (/^[1-9]\d{0,18}$/.test(v) && BigInt(v) <= 9223372036854775807n))
  );
}
export function readParentWeixinCatalog(
  raw: unknown,
  channel: string,
  source: string,
): ParentWeixinCatalog {
  const v = object(raw);
  exact(v, [
    "channel_id",
    "source_channel_id",
    "fingerprint",
    "items",
    "total",
  ]);
  if (
    !parentBindingID(channel) ||
    !parentBindingID(source) ||
    v.channel_id !== channel ||
    v.source_channel_id !== source ||
    typeof v.fingerprint !== "string" ||
    !/^[a-f0-9]{64}$/.test(v.fingerprint) ||
    !Array.isArray(v.items) ||
    v.items.length > 256 ||
    v.total !== v.items.length
  )
    throw invalid();
  const ids = new Set<string>();
  const items = v.items.map((raw) => {
    const p = object(raw);
    exact(p, ["id", "revision", "kind", "appid"]);
    if (
      !parentBindingID(p.id) ||
      ids.has(p.id) ||
      !parentBindingID(p.revision) ||
      (p.kind !== "web" && p.kind !== "mini") ||
      typeof p.appid !== "string" ||
      !p.appid ||
      p.appid.length > 128 ||
      /[\x00-\x20]/.test(p.appid)
    )
      throw invalid();
    ids.add(p.id);
    return p as unknown as ParentWeixinOption;
  });
  return { ...v, items } as unknown as ParentWeixinCatalog;
}
export function readParentWeixinBindingState(
  raw: unknown,
  web: unknown,
  mini: unknown,
): ParentWeixinBindingState {
  const v = object(raw),
    keys = Object.keys(v);
  if (
    keys.length < 1 ||
    keys.length > 2 ||
    keys.some((k) => k !== "web" && k !== "mini")
  )
    throw invalid();
  for (const [key, id] of [
    ["web", web],
    ["mini", mini],
  ] as const) {
    if (v[key] === undefined) continue;
    const p = object(v[key]);
    exact(p, ["id", "revision", "appid", "current"]);
    if (
      !parentBindingID(p.id) ||
      p.id !== id ||
      !parentBindingID(p.revision) ||
      typeof p.appid !== "string" ||
      !p.appid ||
      p.appid.length > 128 ||
      /[\x00-\x20]/.test(p.appid) ||
      typeof p.current !== "boolean"
    )
      throw invalid();
  }
  return v as unknown as ParentWeixinBindingState;
}
