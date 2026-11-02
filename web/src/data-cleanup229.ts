export type CleanupReport229 = {
  cutoff: string;
  preview: boolean;
  expired_sessions: number;
  expired_attempts: number;
  expired_login_throttle_keys: number;
  expired_login_audit_rows: number;
};
export const cleanupCounts229 = [
  { key: "expired_login_audit_rows", label: "登录日志" },
  { key: "expired_sessions", label: "过期会话" },
  { key: "expired_attempts", label: "内存尝试计数" },
  { key: "expired_login_throttle_keys", label: "持久限流记录" },
] as const;
function dateUTC8(value: Date) {
  return new Date(value.valueOf() + 8 * 3600000).toISOString().slice(0, 10);
}
export function cleanupDateBounds229(now = new Date()) {
  const earliest = new Date(now);
  earliest.setUTCFullYear(earliest.getUTCFullYear() - 1);
  // Date-only inputs start at midnight. The earliest complete eligible day
  // starts after the API's rolling one-year timestamp, not before it.
  return {
    min: dateUTC8(new Date(earliest.valueOf() + 86400000)),
    max: dateUTC8(new Date(now.valueOf() - 86400000)),
  };
}
export function cleanupDateDaysAgo229(days: number, now = new Date()) {
  return dateUTC8(new Date(now.valueOf() - days * 86400000));
}
export function cleanupCutoff229(date: string, now = new Date()) {
  const range = cleanupDateBounds229(now);
  const value = new Date(date + "T00:00:00+08:00");
  if (
    !/^\d{4}-\d{2}-\d{2}$/.test(date) ||
    !Number.isFinite(value.valueOf()) ||
    dateUTC8(value) !== date ||
    date < range.min ||
    date > range.max
  )
    throw Error(`请选择${range.min}至${range.max}之间的有效截止日期`);
  return date + "T00:00:00+08:00";
}
export function parseCleanupReport229(
  raw: unknown,
  cutoff: string,
  preview: boolean,
): CleanupReport229 {
  if (!raw || typeof raw !== "object" || Array.isArray(raw))
    throw Error("清理范围返回格式不正确");
  const row = raw as Record<string, unknown>;
  if (
    row.cutoff !== cutoff ||
    row.preview !== preview ||
    cleanupCounts229.some(
      ({ key }) =>
        typeof row[key] !== "number" ||
        !Number.isSafeInteger(row[key]) ||
        Number(row[key]) < 0,
    )
  )
    throw Error("清理范围返回格式不正确或不属于当前截止条件");
  return row as CleanupReport229;
}
