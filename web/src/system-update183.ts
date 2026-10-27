type Object183 = Record<string, unknown>;
function object(value: unknown, keys: string[]): Object183 {
  if (!value || typeof value !== "object" || Array.isArray(value))
    throw Error("更新状态格式无法确认");
  const row = value as Object183;
  if (
    Object.keys(row).length !== keys.length ||
    keys.some((k) => !Object.hasOwn(row, k))
  )
    throw Error("更新状态字段无法确认");
  return row;
}
const hash = (v: unknown) => typeof v === "string" && /^[a-f0-9]{64}$/.test(v);
const text = (v: unknown, max = 65536) =>
  typeof v === "string" && v.length <= max;
const integer = (v: unknown, max: number) =>
  Number.isSafeInteger(v) && Number(v) >= 0 && Number(v) <= max;
export interface UpdateTarget183 {
  fingerprint: string;
  version: string;
  source_sha256: string;
  package_sha256: string;
  schema: number;
  package_bytes: number;
  file_count: number;
}
export interface UpdateProgress183 {
  phase: string;
  read_bytes: number;
  total_bytes: number;
  verified_files: number;
  total_files: number;
}
export interface UpdateStatus183 {
  intent: string;
  state:
    | "none"
    | "preparing"
    | "unknown"
    | "prepared"
    | "applied"
    | "failed"
    | "cancelled";
  state_fingerprint: string;
  revision: string;
  target: UpdateTarget183 | null;
  progress: UpdateProgress183;
  applied: boolean;
  can_cancel: boolean;
  can_retry: boolean;
  error_code: string;
}
export interface SystemUpdate183 {
  current_version: string;
  current_source: string;
  current_commit: string;
  latest_version: string;
  latest_version_name: string;
  latest_source: string;
  need_update: boolean;
  update_log: string;
  check_status: string;
  can_prepare: boolean;
  fingerprint: string;
  preparation: UpdateStatus183;
}
export function parseUpdateStatus183(value: unknown): UpdateStatus183 {
  const r = object(value, [
    "intent",
    "state",
    "state_fingerprint",
    "revision",
    "target",
    "progress",
    "applied",
    "can_cancel",
    "can_retry",
    "error_code",
  ]);
  if (
    !hash(r.intent) ||
    !hash(r.state_fingerprint) ||
    typeof r.revision !== "string" ||
    !/^(0|[1-9]\d{0,18})$/.test(r.revision) ||
    BigInt(r.revision) > 9223372036854775807n ||
    ![
      "none",
      "preparing",
      "unknown",
      "prepared",
      "applied",
      "failed",
      "cancelled",
    ].includes(String(r.state)) ||
    typeof r.applied !== "boolean" ||
    typeof r.can_cancel !== "boolean" ||
    typeof r.can_retry !== "boolean" ||
    !text(r.error_code, 128)
  )
    throw Error("原更新意图无法确认");
  const p = object(r.progress, [
    "phase",
    "read_bytes",
    "total_bytes",
    "verified_files",
    "total_files",
  ]);
  if (
    ![
      "idle",
      "queued",
      "accepted",
      "reading",
      "verifying",
      "staged",
      "committing",
      "prepared",
      "applied",
      "failed",
      "cancelled",
    ].includes(String(p.phase)) ||
    !integer(p.read_bytes, 64 * 1024 * 1024) ||
    !integer(p.total_bytes, 64 * 1024 * 1024) ||
    Number(p.read_bytes) > Number(p.total_bytes) ||
    !integer(p.verified_files, 2048) ||
    !integer(p.total_files, 2048) ||
    Number(p.verified_files) > Number(p.total_files)
  )
    throw Error("实际更新进度无法确认");
  if (r.target === null) {
    if (
      r.state !== "none" ||
      r.revision !== "0" ||
      r.can_cancel ||
      r.can_retry ||
      r.applied ||
      p.phase !== "idle" ||
      p.total_bytes !== 0 ||
      p.total_files !== 0
    )
      throw Error("未准备的更新包含执行结果");
  } else {
    const t = object(r.target, [
      "fingerprint",
      "version",
      "source_sha256",
      "package_sha256",
      "schema",
      "package_bytes",
      "file_count",
    ]);
    if (
      t.fingerprint !== r.intent ||
      !hash(t.source_sha256) ||
      !hash(t.package_sha256) ||
      !text(t.version, 100) ||
      !integer(t.schema, 999999) ||
      !integer(t.package_bytes, 64 * 1024 * 1024) ||
      !integer(t.file_count, 2048) ||
      t.package_bytes !== p.total_bytes ||
      t.file_count !== p.total_files ||
      r.state === "none" ||
      r.revision === "0"
    )
      throw Error("更新目标与原意图不一致");
  }
  if (
    r.applied !== (r.state === "applied") ||
    (r.state === "applied" && (r.can_cancel || r.can_retry)) ||
    (["prepared", "applied"].includes(String(r.state)) &&
      (p.read_bytes !== p.total_bytes || p.verified_files !== p.total_files))
  )
    throw Error("准备进度不能证明已部署");
  return r as unknown as UpdateStatus183;
}
export function parseSystemUpdate183(value: unknown): SystemUpdate183 {
  const r = object(value, [
    "current_version",
    "current_source",
    "current_commit",
    "latest_version",
    "latest_version_name",
    "latest_source",
    "need_update",
    "update_log",
    "check_status",
    "can_prepare",
    "fingerprint",
    "preparation",
  ]);
  for (const key of [
    "current_version",
    "current_source",
    "current_commit",
    "latest_version",
    "latest_version_name",
    "latest_source",
    "update_log",
  ])
    if (!text(r[key])) throw Error("版本资料无法确认");
  if (
    !hash(r.fingerprint) ||
    typeof r.need_update !== "boolean" ||
    typeof r.can_prepare !== "boolean" ||
    ![
      "unconfigured",
      "source_unavailable",
      "invalid_manifest",
      "platform_incompatible",
      "schema_incompatible",
      "build_unversioned",
      "current",
      "source_mismatch",
      "package_unavailable",
      "available",
    ].includes(String(r.check_status)) ||
    (r.can_prepare && (!r.need_update || r.check_status !== "available"))
  )
    throw Error("版本检查状态无法确认");
  return {
    ...r,
    preparation: parseUpdateStatus183(r.preparation),
  } as unknown as SystemUpdate183;
}
export const updateStateLabels183: Record<string, string> = {
  none: "尚未准备",
  preparing: "正在准备",
  unknown: "结果待核对",
  prepared: "准备完成，等待部署",
  applied: "更新已部署",
  failed: "准备失败",
  cancelled: "已取消",
};
export const updateCheckLabels183: Record<string, string> = {
  unconfigured: "尚未配置更新源",
  source_unavailable: "更新源暂不可读",
  invalid_manifest: "更新清单校验失败",
  platform_incompatible: "更新包不适用于当前平台",
  schema_incompatible: "数据库版本不兼容",
  build_unversioned: "当前构建未标记发行版本",
  current: "当前已是最新版本",
  source_mismatch: "版本号相同，但构建来源不一致",
  package_unavailable: "本地更新包暂不可用",
  available: "检测到可用更新",
};
