import { displayValue } from "./api";

// Explicit API timestamp fields only. Calendar dimensions and arbitrary text
// must retain their original meaning. This is presentation, never serialization.
const timestampFields = new Set([
  "created_at",
  "updated_at",
  "paid_at",
  "membership_expires_at",
  "observed_at",
]);

export function isConsoleTimestamp(key: string): boolean {
  return timestampFields.has(key);
}

export function beijingTimestamp(value: unknown): string {
  if (value === undefined || value === null || value === "") return "—";
  if (typeof value !== "string") return displayValue(value);
  const match =
    /^(\d{4})-(\d{2})-(\d{2})T(\d{2}):(\d{2}):(\d{2})(?:\.\d{1,9})?(Z|[+-]\d{2}:\d{2})$/.exec(
      value,
    );
  if (!match || match[7] === "-00:00") return value;
  const [, year, month, day, hour, minute, second, offset] = match;
  const y = Number(year),
    m = Number(month),
    d = Number(day);
  const days = [
    31,
    y % 4 === 0 && (y % 100 !== 0 || y % 400 === 0) ? 29 : 28,
    31,
    30,
    31,
    30,
    31,
    31,
    30,
    31,
    30,
    31,
  ];
  if (
    y === 0 ||
    m < 1 ||
    m > 12 ||
    d < 1 ||
    d > days[m - 1]! ||
    Number(hour) > 23 ||
    Number(minute) > 59 ||
    Number(second) > 59 ||
    (offset !== "Z" &&
      (Number(offset!.slice(1, 3)) > 23 || Number(offset!.slice(4)) > 59))
  )
    return value;
  const instant = Date.parse(value);
  if (!Number.isFinite(instant)) return value;
  // The console's business calendar is fixed UTC+8, independent of the browser
  // host timezone. Raw RFC3339 values remain untouched in API/CSV payloads.
  const date = new Date(instant + 8 * 60 * 60 * 1000);
  if (date.getUTCFullYear() < 1 || date.getUTCFullYear() > 9999) return value;
  const two = (n: number) => String(n).padStart(2, "0");
  return `${String(date.getUTCFullYear()).padStart(4, "0")}-${two(date.getUTCMonth() + 1)}-${two(date.getUTCDate())} ${two(date.getUTCHours())}:${two(date.getUTCMinutes())}:${two(date.getUTCSeconds())}`;
}

export function consoleTimeLabel(key: string, label: string): string {
  return isConsoleTimestamp(key) && !label.includes("北京时间")
    ? `${label}（北京时间）`
    : label;
}
