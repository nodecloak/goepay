import { expect, it } from "vitest";
import {
  cleanupCutoff229,
  cleanupDateBounds229,
  parseCleanupReport229,
} from "../src/data-cleanup229";

it("limits complete UTC+8 dates without falling outside the server rolling-year cutoff", () => {
  const now = new Date("2026-11-02T06:00:00Z");
  expect(cleanupDateBounds229(now)).toEqual({
    min: "2025-11-03",
    max: "2026-11-01",
  });
  expect(cleanupCutoff229("2026-11-01", now)).toBe("2026-11-01T00:00:00+08:00");
  for (const date of [
    "2026-11-02",
    "2025-11-02",
    "2026-02-30",
    "",
    "2026-2-01",
  ])
    expect(() => cleanupCutoff229(date, now)).toThrow();
});
it("respects the Beijing calendar across UTC midnight and leap years", () => {
  const now = new Date("2024-02-29T17:00:00Z");
  const bounds = cleanupDateBounds229(now);
  expect(bounds.max).toBe("2024-02-29");
  const cutoff = new Date(cleanupCutoff229(bounds.min, now));
  const oldest = new Date(now);
  oldest.setUTCFullYear(2023);
  expect(cutoff.valueOf()).toBeGreaterThan(oldest.valueOf());
  expect(new Date(cleanupCutoff229(bounds.max, now)).valueOf()).toBeLessThan(
    now.valueOf() - 86400000,
  );
});
it("rejects unsafe deletion counts instead of showing a guessed zero", () => {
  const cutoff = "2026-10-03T00:00:00+08:00";
  const row = {
    cutoff,
    preview: false,
    expired_sessions: 0,
    expired_attempts: 0,
    expired_login_throttle_keys: 0,
    expired_login_audit_rows: 0,
  };
  for (const value of [-1, Number.MAX_SAFE_INTEGER + 1, 0.1, "7", null])
    expect(() =>
      parseCleanupReport229(
        { ...row, expired_login_audit_rows: value },
        cutoff,
        false,
      ),
    ).toThrow();
  expect(parseCleanupReport229(row, cutoff, false)).toEqual(row);
});
