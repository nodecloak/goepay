import { ApiClient, ApiError } from "./api";

export const migrationPath234 = "/migration/epay";
export const migrationUploadLimit234 = 64 * 1024 * 1024;
export type MigrationSource234 = "epay_pro" | "epay_release";
export interface MigrationIssue234 {
  path: string;
  code: string;
}
export interface MigrationTable234 {
  name: string;
  rows: number;
  mode: "native" | "archive";
}
export interface MigrationReport234 {
  refund_history_version: number;
  valid: boolean;
  sha256: string;
  merchant_count: number;
  order_count: number;
  refund_count: number;
  transfer_count: number;
  available_total: string;
  reserved_total: string;
  issues: MigrationIssue234[];
  warnings: MigrationIssue234[];
  applied: boolean;
  already_imported: boolean;
}
export interface MigrationView234 {
  id: string;
  status: "blocked" | "prepared" | "applying" | "completed" | "failed";
  sha256: string;
  source: MigrationSource234;
  prefix: string;
  table_counts: MigrationTable234[];
  merchant_count: number;
  order_count: number;
  refund_count: number;
  transfer_count: number;
  available_total: string;
  reserved_total: string;
  issues: MigrationIssue234[];
  warnings: MigrationIssue234[];
  target_ready: boolean;
  progress: number;
  report: MigrationReport234 | null;
  expires_at?: string;
}
export interface MigrationCapabilities234 {
  enabled: true;
  max_upload_bytes: number;
  target_ready: boolean;
  issues: MigrationIssue234[];
  runs: MigrationView234[];
}
const hash = /^[a-f0-9]{64}$/,
  id = /^[a-f0-9]{32}$/;
const money = /^(0|[1-9]\d{0,16})\.\d{2}$/;
const record = (v: unknown): v is Record<string, unknown> =>
  !!v && typeof v === "object" && !Array.isArray(v);
const invalid = () =>
  new Error("迁移结果格式无法核实，请读取当前任务状态，不要重复导入。");
function keys(
  v: Record<string, unknown>,
  required: string[],
  optional: string[] = [],
) {
  if (
    required.some((k) => !Object.hasOwn(v, k)) ||
    Object.keys(v).some((k) => !required.includes(k) && !optional.includes(k))
  )
    throw invalid();
}
function count(v: unknown): v is number {
  return Number.isSafeInteger(v) && Number(v) >= 0;
}
function issues(v: unknown): MigrationIssue234[] {
  if (!Array.isArray(v) || v.length > 4096) throw invalid();
  return v.map((i) => {
    if (!record(i)) throw invalid();
    keys(i, ["path", "code"]);
    if (
      typeof i.path !== "string" ||
      !/^[$A-Za-z0-9_.\[\]:/-]{0,200}$/.test(i.path) ||
      typeof i.code !== "string" ||
      !/^[a-z][a-z0-9_]{0,95}$/.test(i.code)
    )
      throw invalid();
    return { path: i.path, code: i.code };
  });
}
const counts = [
  "merchant_count",
  "order_count",
  "refund_count",
  "transfer_count",
];
function summary(v: Record<string, unknown>) {
  if (
    counts.some((k) => !count(v[k])) ||
    typeof v.available_total !== "string" ||
    !money.test(v.available_total) ||
    typeof v.reserved_total !== "string" ||
    !money.test(v.reserved_total)
  )
    throw invalid();
}
function report(v: unknown): MigrationReport234 | null {
  if (v === null) return null;
  if (!record(v)) throw invalid();
  keys(v, [
    "refund_history_version",
    "valid",
    "sha256",
    ...counts,
    "available_total",
    "reserved_total",
    "issues",
    "warnings",
    "applied",
    "already_imported",
  ]);
  summary(v);
  if (
    !count(v.refund_history_version) ||
    typeof v.valid !== "boolean" ||
    typeof v.applied !== "boolean" ||
    typeof v.already_imported !== "boolean" ||
    typeof v.sha256 !== "string" ||
    !hash.test(v.sha256)
  )
    throw invalid();
  return {
    ...v,
    issues: issues(v.issues),
    warnings: issues(v.warnings),
  } as unknown as MigrationReport234;
}
export function parseMigrationView234(v: unknown): MigrationView234 {
  if (!record(v)) throw invalid();
  keys(
    v,
    [
      "id",
      "status",
      "sha256",
      "source",
      "prefix",
      "table_counts",
      ...counts,
      "available_total",
      "reserved_total",
      "issues",
      "warnings",
      "target_ready",
      "progress",
      "report",
    ],
    ["expires_at"],
  );
  summary(v);
  if (
    typeof v.id !== "string" ||
    !id.test(v.id) ||
    typeof v.sha256 !== "string" ||
    !hash.test(v.sha256) ||
    !["blocked", "prepared", "applying", "completed", "failed"].includes(
      String(v.status),
    ) ||
    !["epay_pro", "epay_release"].includes(String(v.source)) ||
    typeof v.prefix !== "string" ||
    !/^[A-Za-z0-9_]{0,59}$/.test(v.prefix) ||
    typeof v.target_ready !== "boolean" ||
    !count(v.progress) ||
    Number(v.progress) > 100
  )
    throw invalid();
  if (
    v.expires_at !== undefined &&
    (typeof v.expires_at !== "string" ||
      v.expires_at.length > 40 ||
      !Number.isFinite(Date.parse(v.expires_at)))
  )
    throw invalid();
  if (!Array.isArray(v.table_counts) || v.table_counts.length > 4096)
    throw invalid();
  const tables = v.table_counts.map((t) => {
    if (!record(t)) throw invalid();
    keys(t, ["name", "rows", "mode"]);
    if (
      typeof t.name !== "string" ||
      !/^[A-Za-z0-9_]{1,64}$/.test(t.name) ||
      !count(t.rows) ||
      !["native", "archive"].includes(String(t.mode))
    )
      throw invalid();
    return { name: t.name, rows: t.rows, mode: t.mode } as MigrationTable234;
  });
  if (new Set(tables.map((t) => t.name)).size !== tables.length)
    throw invalid();
  if (
    !v.prefix &&
    ["prepared", "applying", "completed"].includes(String(v.status)) &&
    !["user", "order", "type"].every((name) =>
      tables.some((table) => table.name === name),
    )
  )
    throw invalid();
  const r = report(v.report),
    blocked = issues(v.issues);
  if (v.status !== "completed" && r !== null) throw invalid();
  if (v.status === "prepared" && blocked.length) throw invalid();
  if (
    v.status === "completed" &&
    (v.progress !== 100 ||
      !r?.valid ||
      (!r.applied && !r.already_imported) ||
      blocked.length !== 0 ||
      counts.some(
        (key) => v[key] !== (r as unknown as Record<string, unknown>)[key],
      ) ||
      v.available_total !== r.available_total ||
      v.reserved_total !== r.reserved_total)
  )
    throw invalid();
  return {
    ...v,
    table_counts: tables,
    issues: blocked,
    warnings: issues(v.warnings),
    report: r,
  } as unknown as MigrationView234;
}
export function parseMigrationCapabilities234(
  v: unknown,
): MigrationCapabilities234 {
  if (!record(v)) throw invalid();
  keys(v, ["enabled", "max_upload_bytes", "target_ready", "issues", "runs"]);
  if (
    v.enabled !== true ||
    !count(v.max_upload_bytes) ||
    Number(v.max_upload_bytes) < 1 ||
    Number(v.max_upload_bytes) > migrationUploadLimit234 ||
    typeof v.target_ready !== "boolean" ||
    !Array.isArray(v.runs) ||
    v.runs.length > 20
  )
    throw invalid();
  const runs = v.runs.map(parseMigrationView234);
  if (new Set(runs.map((r) => r.id)).size !== runs.length) throw invalid();
  return {
    ...v,
    issues: issues(v.issues),
    runs,
  } as unknown as MigrationCapabilities234;
}
export async function prepareMigration234(
  client: ApiClient,
  source: string,
  prefix: string,
  file: File,
  signal?: AbortSignal,
) {
  if (
    client.area !== "admin" ||
    !client.csrfToken ||
    !["epay_pro", "epay_release"].includes(source) ||
    !/^[A-Za-z0-9_]{0,59}$/.test(prefix) ||
    file.size < 1 ||
    file.size > migrationUploadLimit234 ||
    !/\.sql$/i.test(file.name)
  )
    throw Error("请选择不超过64MiB的SQL备份，并检查源版本、表前缀和本人会话。");
  const csrf = client.csrfToken;
  const form = new FormData();
  form.set("source", source);
  form.set("prefix", prefix);
  form.set("backup", file);
  const response = await fetch(`/console/v1/admin${migrationPath234}/prepare`, {
    method: "POST",
    credentials: "same-origin",
    headers: { Accept: "application/json", "X-CSRF-Token": csrf },
    body: form,
    signal,
  });
  if (signal?.aborted || client.area !== "admin" || client.csrfToken !== csrf)
    throw invalid();
  if (response.status === 401) {
    client.csrfToken = "";
    client.onUnauthorized?.();
  }
  const text = await response.text();
  if (text.length > 2 * 1024 * 1024) throw invalid();
  let payload: unknown;
  try {
    payload = JSON.parse(text);
  } catch {
    throw invalid();
  }
  if (!record(payload) || typeof payload.code !== "number") throw invalid();
  if (!response.ok || payload.code !== 0)
    throw new ApiError(
      "预检查未被接受，请检查会话、文件大小和备份格式。",
      response.status,
      payload.code,
    );
  return parseMigrationView234(payload.data);
}
export function migrationIssueText234(code: string) {
  const text: Record<string, string> = {
    target_not_empty: "目标实例已有业务数据，不能覆盖导入。",
    target_not_ready: "目标实例当前不满足空库条件。",
    archive_only: "此表只加密归档，不会转换成可用业务功能。",
    unsupported_source: "请选择正确的易支付版本。",
    invalid_money: "金额无法按精确分值转换，请核对原始记录。",
    invalid_timestamp: "时间字段无效或超出范围。",
    invalid_registration_timestamp: "商户注册时间无效。",
    duplicate_merchant: "存在重复商户编号。",
    duplicate_order: "存在重复订单编号。",
    unknown_merchant: "订单引用的商户不存在。",
    unsupported_status: "存在无法自动迁移的业务状态。",
    unresolved_settlement: "存在未完成结算，请先在原站核清。",
    unresolved_transfer: "存在未完成转账，请先在原站核清。",
    unsupported_deposit: "存在尚未映射的保证金，请先核清。",
    unsafe_statement: "备份中包含不支持的数据变更语句。",
    ambiguous_prefix: "存在多组源表，请明确表前缀。",
    missing_tables: "备份缺少必要的商户、订单或支付类型表。",
    expired: "预检查已过期，请重新上传一致备份。",
  };
  return text[code] || "此项需要人工核对；系统不会猜测或静默修复。";
}
