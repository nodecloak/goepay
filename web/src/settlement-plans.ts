import type { SettlementRecipient } from "./settlement-recipient";
export interface PlanSchedule {
  frequency: "second" | "minute" | "hour" | "day" | "once";
  interval: string;
  anchor_at: string;
  execute_hour: number;
  execute_minute: number;
  execute_second: number;
}
export interface SettlementPlan {
  id: string;
  revision: string;
  name: string;
  mode: "generate_only" | "balance_threshold";
  schedule: PlanSchedule;
  merchant_ids: string[];
  enabled: boolean;
  last_authorization_id: string;
}
export interface PlanAuthorization {
  id: string;
  plan_id: string;
  plan_revision: string;
  expected_authorization_id: string;
  valid_from: string;
  expires_at: string;
  max_item_gross: string;
  max_run_gross: string;
  max_day_gross: string;
  max_day_items: string;
  revoked: boolean;
  created_at: string;
}
export interface PlanRun {
  id: string;
  plan_id: string;
  plan_revision: string;
  authorization_id: string;
  slot_no: string;
  business_date: string;
  scheduled_at: string;
  state: string;
  created_count: string;
  skipped_count: string;
  blocked_count: string;
  error_count: string;
  created_at: string;
  items?: Record<string, unknown>[];
}
export interface SettlementSchedulePrefs {
  merchant_id: string;
  revision: string;
  enabled: boolean;
  recipient_config_id: string;
  recipient: SettlementRecipient;
}
export interface PlanDraft {
  mode?: string;
  name: string;
  frequency: string;
  interval: string;
  anchor_at: string;
  execute_hour: string;
  execute_minute: string;
  execute_second: string;
  merchant_ids: string;
}
export interface AuthorizationDraft {
  valid_from: string;
  expires_at: string;
  max_item_gross: string;
  max_run_gross: string;
  max_day_gross: string;
  max_day_items: string;
}
const integer = (value: string, zero = false) => {
  if (
    !(zero ? /^(0|[1-9]\d*)$/ : /^[1-9]\d*$/).test(value) ||
    BigInt(value) > 9223372036854775807n
  )
    throw new Error("请输入规范整数，ID和金额不接受小数或前导零");
  return value;
};
function instant(value: string): string {
  if (
    !/^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(?:Z|[+-]\d{2}:\d{2})$/.test(value) ||
    !Number.isFinite(Date.parse(value))
  )
    throw new Error("时间须为带时区的RFC3339，例如2026-10-24T09:00:00+08:00");
  return value;
}
export function planBody(
  d: PlanDraft,
  expected?: string,
): Record<string, unknown> {
  const mode = d.mode === undefined ? "generate_only" : d.mode;
  if (
    !d.name.trim() ||
    d.name.trim().length > 100 ||
    !["generate_only", "balance_threshold"].includes(mode) ||
    !(
      mode === "balance_threshold"
        ? ["second", "minute", "hour", "day", "once"]
        : ["hour", "day", "once"]
    ).includes(d.frequency)
  )
    throw new Error("请填写计划名称与支持的频率");
  const ids = d.merchant_ids
    .trim()
    .split(/[\s,，]+/)
    .filter(Boolean);
  if (ids.length < 1 || ids.length > 100 || new Set(ids).size !== ids.length)
    throw new Error("请填写1–100个不重复的商户ID");
  ids.forEach((x) => integer(x));
  const interval = Number(integer(d.interval, true));
  const hour = Number(integer(d.execute_hour, true)),
    minute = Number(integer(d.execute_minute, true)),
    second = Number(integer(d.execute_second, true));
  if (
    !Number.isSafeInteger(interval) ||
    (d.frequency === "once" ? interval !== 0 : interval < 1) ||
    (d.frequency === "hour" && interval > 168) ||
    (d.frequency === "day" && interval > 366) ||
    (d.frequency === "minute" && interval > 1440) ||
    (d.frequency === "second" && (interval < 10 || interval > 3600)) ||
    hour > 23 ||
    minute > 59 ||
    second > 59 ||
    (d.frequency !== "day" && (hour !== 0 || minute !== 0 || second !== 0))
  )
    throw new Error(
      "请核对间隔和执行时分秒；秒10–3600、分钟1–1440、小时1–168、天1–366、单次0；非按天时分秒必须为0",
    );
  return {
    name: d.name.trim(),
    mode,
    schedule: {
      frequency: d.frequency,
      interval: d.interval,
      anchor_at: instant(d.anchor_at),
      execute_hour: hour,
      execute_minute: minute,
      execute_second: second,
    },
    merchant_ids: ids,
    ...(expected === undefined
      ? {}
      : { expected_revision: integer(expected, true) }),
  };
}
export function authorizationBody(
  revision: string,
  previous: string,
  d: AuthorizationDraft,
): Record<string, string> {
  const from = instant(d.valid_from),
    expiry = instant(d.expires_at);
  if (Date.parse(expiry) <= Date.parse(from))
    throw new Error("授权截止须晚于开始时间");
  return {
    expected_revision: integer(revision),
    expected_authorization_id: integer(previous, true),
    valid_from: from,
    expires_at: expiry,
    max_item_gross: integer(d.max_item_gross),
    max_run_gross: integer(d.max_run_gross),
    max_day_gross: integer(d.max_day_gross),
    max_day_items: integer(d.max_day_items),
  };
}
