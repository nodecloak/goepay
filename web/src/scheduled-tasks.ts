export type ScheduledFrequency = "second" | "minute" | "hour" | "day" | "once";
export interface ScheduledSchedule {
  frequency: ScheduledFrequency;
  interval: number;
  execute_at: string;
}
export interface ScheduledParameter {
  name: string;
  label: string;
  type: "number";
  min: number;
  max: number | null;
  default: number;
  required: true;
}
export interface ScheduledTask {
  task: string;
  name: string;
  description: string;
  available: boolean;
  unavailable_reason: string;
  parameter_definitions: ScheduledParameter[];
  configured: boolean;
  control_mode: "scheduler" | "legacy_managed" | "unavailable";
  enabled: boolean;
  schedule: ScheduledSchedule | null;
  params: Record<string, number>;
  revision: string;
  fingerprint: string;
  can_update: boolean;
  can_run: boolean;
  running: boolean;
  last_run_time: string | null;
  next_run_time: string | null;
  last_status: string;
  last_error: string;
}
export interface ScheduledCounts {
  examined: number;
  awarded: number;
  generated: number;
  skipped: number;
  enqueued: number;
  awaiting_authorization: number;
}
export interface ScheduledRun {
  id: string;
  task: string;
  trigger: "manual" | "schedule";
  status: string;
  scheduled_for: string;
  created_at: string;
  started_at: string | null;
  completed_at: string | null;
  counts: ScheduledCounts | null;
  error_code: string;
}
export interface ScheduledHistory {
  items: ScheduledRun[];
  total: number;
  next_cursor: string;
}
export interface ScheduledDraft {
  enabled: boolean;
  frequency: string;
  interval: string;
  executeAt: string;
  params: Record<string, string>;
}
const keys = [
  "order",
  "notify",
  "order_query",
  "transfer",
  "profitsharing",
  "daily_report",
  "balance_notice",
  "check",
  "complain",
  "applyrefresh",
  "settle",
  "referral_daily",
  "settlement_generate",
];
const statuses = [
  "never_run",
  "pending",
  "running",
  "lease_expired",
  "success",
  "failed",
  "cancelled",
];
const errors = [
  "",
  "business_state_changed",
  "execution_failed",
  "config_changed",
];
const maxima: Record<string, number> = {
  second: 3600,
  minute: 1440,
  hour: 168,
  day: 366,
  once: 1,
};
const fail = () => new Error("计划任务资料无法核实，请刷新后重新确认。");
function object(raw: unknown, fields: string[]): Record<string, unknown> {
  if (!raw || typeof raw !== "object" || Array.isArray(raw)) throw fail();
  const v = raw as Record<string, unknown>;
  if (
    Object.keys(v).length !== fields.length ||
    fields.some((k) => !Object.hasOwn(v, k))
  )
    throw fail();
  return v;
}
function integer(
  v: unknown,
  min = 0,
  max = Number.MAX_SAFE_INTEGER,
): v is number {
  return Number.isSafeInteger(v) && Number(v) >= min && Number(v) <= max;
}
function text(v: unknown, max = 5000): v is string {
  return (
    typeof v === "string" &&
    v.length <= max &&
    !/[\u0000-\u0008\u000b\u000c\u000e-\u001f\u007f]/.test(v)
  );
}
function id(v: unknown, zero = false): v is string {
  return (
    typeof v === "string" &&
    (zero ? /^(0|[1-9]\d{0,18})$/ : /^[1-9]\d{0,18}$/).test(v) &&
    BigInt(v) <= 9223372036854775807n
  );
}
function time(v: unknown): v is string {
  return (
    typeof v === "string" &&
    /^\d{4}-\d\d-\d\dT\d\d:\d\d:\d\d(?:\.\d{1,9})?(?:Z|[+-]\d\d:\d\d)$/.test(
      v,
    ) &&
    Number.isFinite(Date.parse(v))
  );
}
function nullableTime(v: unknown): v is string | null {
  return v === null || time(v);
}
export function scheduledTaskPath(key: string) {
  if (!keys.includes(key)) throw fail();
  return "/scheduled-tasks/" + key;
}
function readSchedule(raw: unknown): ScheduledSchedule {
  const v = object(raw, ["frequency", "interval", "execute_at"]);
  if (
    typeof v.frequency !== "string" ||
    !Object.hasOwn(maxima, v.frequency) ||
    !integer(v.interval, 1, maxima[v.frequency]) ||
    !time(v.execute_at) ||
    new Date(v.execute_at).getUTCFullYear() < 1
  )
    throw fail();
  return { ...v } as unknown as ScheduledSchedule;
}
function readParameter(raw: unknown): ScheduledParameter {
  const v = object(raw, [
    "name",
    "label",
    "type",
    "min",
    "max",
    "default",
    "required",
  ]);
  if (
    typeof v.name !== "string" ||
    !/^[a-z][a-z0-9_]{0,63}$/.test(v.name) ||
    !text(v.label, 200) ||
    v.type !== "number" ||
    !integer(v.min) ||
    (v.max !== null && !integer(v.max, Number(v.min))) ||
    !integer(
      v.default,
      Number(v.min),
      v.max === null ? Number.MAX_SAFE_INTEGER : Number(v.max),
    ) ||
    v.required !== true
  )
    throw fail();
  return { ...v } as unknown as ScheduledParameter;
}
export function readScheduledTask(raw: unknown): ScheduledTask {
  const v = object(raw, [
    "task",
    "name",
    "description",
    "available",
    "unavailable_reason",
    "parameter_definitions",
    "configured",
    "control_mode",
    "enabled",
    "schedule",
    "params",
    "revision",
    "fingerprint",
    "can_update",
    "can_run",
    "running",
    "last_run_time",
    "next_run_time",
    "last_status",
    "last_error",
  ]);
  if (
    typeof v.task !== "string" ||
    !keys.includes(v.task) ||
    !text(v.name, 200) ||
    !v.name ||
    !text(v.description) ||
    !text(v.unavailable_reason) ||
    ![
      v.available,
      v.configured,
      v.enabled,
      v.can_update,
      v.can_run,
      v.running,
    ].every((x) => typeof x === "boolean") ||
    !id(v.revision, true) ||
    typeof v.fingerprint !== "string" ||
    !/^[a-f0-9]{64}$/.test(v.fingerprint) ||
    !Array.isArray(v.parameter_definitions) ||
    v.parameter_definitions.length > 10 ||
    !nullableTime(v.last_run_time) ||
    !nullableTime(v.next_run_time) ||
    typeof v.last_status !== "string" ||
    !statuses.includes(v.last_status) ||
    typeof v.last_error !== "string" ||
    !errors.includes(v.last_error) ||
    !["scheduler", "legacy_managed", "unavailable"].includes(
      String(v.control_mode),
    )
  )
    throw fail();
  const definitions = v.parameter_definitions.map(readParameter),
    names = definitions.map((p) => p.name);
  if (new Set(names).size !== names.length) throw fail();
  const params = object(v.params, names);
  for (const p of definitions)
    if (
      !integer(
        params[p.name],
        p.min,
        p.max === null ? Number.MAX_SAFE_INTEGER : p.max,
      )
    )
      throw fail();
  const schedule = v.schedule === null ? null : readSchedule(v.schedule);
  if (
    v.configured
      ? !id(v.revision) || !schedule || v.control_mode === "legacy_managed"
      : v.revision !== "0" || schedule !== null || v.enabled || v.can_run
  )
    throw fail();
  if (
    v.running !==
      (v.last_status === "running" || v.last_status === "lease_expired") ||
    (v.can_update && (!v.available || v.running)) ||
    (v.can_run &&
      (!v.available ||
        !v.configured ||
        !v.enabled ||
        v.running ||
        v.last_status === "pending")) ||
    (!v.available &&
      (v.can_update || v.can_run || v.control_mode !== "unavailable")) ||
    (v.available && v.control_mode === "unavailable")
  )
    throw fail();
  return {
    ...v,
    schedule,
    parameter_definitions: definitions,
    params: { ...params },
  } as unknown as ScheduledTask;
}
export function readScheduledCatalog(raw: unknown): ScheduledTask[] {
  const v = object(raw, ["items", "total"]);
  if (
    !Array.isArray(v.items) ||
    !integer(v.total) ||
    v.total !== v.items.length ||
    v.items.length > 100
  )
    throw fail();
  const items = v.items.map(readScheduledTask);
  if (new Set(items.map((t) => t.task)).size !== items.length) throw fail();
  return items;
}
export function readScheduledRun(raw: unknown, task?: string): ScheduledRun {
  const v = object(raw, [
    "id",
    "task",
    "trigger",
    "status",
    "scheduled_for",
    "created_at",
    "started_at",
    "completed_at",
    "counts",
    "error_code",
  ]);
  if (
    !id(v.id) ||
    typeof v.task !== "string" ||
    !keys.includes(v.task) ||
    (task !== undefined && v.task !== task) ||
    !(v.trigger === "manual" || v.trigger === "schedule") ||
    typeof v.status !== "string" ||
    !statuses.includes(v.status) ||
    v.status === "never_run" ||
    !time(v.scheduled_for) ||
    !time(v.created_at) ||
    !nullableTime(v.started_at) ||
    !nullableTime(v.completed_at) ||
    typeof v.error_code !== "string" ||
    !errors.includes(v.error_code)
  )
    throw fail();
  const terminal = ["success", "failed", "cancelled"].includes(v.status);
  if (
    terminal !== (v.completed_at !== null) ||
    terminal !== (v.counts !== null) ||
    (v.status === "pending" && v.started_at !== null) ||
    (["running", "lease_expired", "success", "failed"].includes(v.status) &&
      v.started_at === null) ||
    (!terminal && v.error_code !== "") ||
    (v.status === "success" && v.error_code !== "")
  )
    throw fail();
  let counts = null;
  if (v.counts !== null) {
    const c = object(v.counts, [
      "examined",
      "awarded",
      "generated",
      "skipped",
      "enqueued",
      "awaiting_authorization",
    ]);
    if (!Object.values(c).every((x) => integer(x))) throw fail();
    counts = { ...c } as unknown as ScheduledCounts;
  }
  return { ...v, counts } as unknown as ScheduledRun;
}
export function readScheduledHistory(
  raw: unknown,
  task: string,
): ScheduledHistory {
  scheduledTaskPath(task);
  const v = object(raw, ["items", "total", "next_cursor"]);
  if (
    !Array.isArray(v.items) ||
    v.items.length > 100 ||
    !integer(v.total) ||
    v.total < v.items.length ||
    !id(v.next_cursor, true)
  )
    throw fail();
  const items = v.items.map((r) => readScheduledRun(r, task));
  if (
    items.some((r, i) => i > 0 && BigInt(r.id) >= BigInt(items[i - 1].id)) ||
    (v.next_cursor !== "0" &&
      (!items.length || v.next_cursor !== items.at(-1)?.id))
  )
    throw fail();
  return { items, total: v.total, next_cursor: v.next_cursor };
}
export function scheduledTaskModeLabel(task: ScheduledTask) {
  if (!task.available) return "暂不可用";
  if (task.control_mode === "legacy_managed")
    return "未接管（由原 worker 管理）";
  if (!task.configured) return "未配置（关闭）";
  return task.enabled ? "已开启" : "已关闭";
}
export function scheduledUTC8Input(raw: string) {
  if (!time(raw)) throw fail();
  return new Date(Date.parse(raw) + 8 * 3600000).toISOString().slice(0, 19);
}
function inputInteger(raw: string, min: number, max: number) {
  if (!/^(0|[1-9]\d*)$/.test(raw)) throw fail();
  const v = Number(raw);
  if (!integer(v, min, max)) throw fail();
  return v;
}
export function scheduledTaskChange(
  task: ScheduledTask,
  draft: ScheduledDraft,
) {
  if (
    !task.available ||
    !task.can_update ||
    typeof draft.enabled !== "boolean" ||
    !Object.hasOwn(maxima, draft.frequency) ||
    !/^\d{4}-\d\d-\d\dT\d\d:\d\d(?::\d\d)?$/.test(draft.executeAt)
  )
    throw fail();
  const local =
      draft.executeAt.length === 16 ? draft.executeAt + ":00" : draft.executeAt,
    parsed = new Date(local + "+08:00");
  if (
    !Number.isFinite(parsed.getTime()) ||
    scheduledUTC8Input(parsed.toISOString()) !== local
  )
    throw fail();
  const params = object(
      draft.params,
      task.parameter_definitions.map((p) => p.name),
    ),
    values: Record<string, number> = {};
  for (const p of task.parameter_definitions) {
    if (typeof params[p.name] !== "string") throw fail();
    values[p.name] = inputInteger(
      params[p.name] as string,
      p.min,
      p.max === null ? Number.MAX_SAFE_INTEGER : p.max,
    );
  }
  return {
    enabled: draft.enabled,
    schedule: {
      frequency: draft.frequency as ScheduledFrequency,
      interval:
        draft.frequency === "once"
          ? 1
          : inputInteger(draft.interval, 1, maxima[draft.frequency]),
      execute_at: parsed.toISOString(),
    },
    params: values,
    expected_fingerprint: task.fingerprint,
  };
}
