import { describe, expect, it } from "vitest";
import { consoleColumnLabel, consoleValue } from "../src/pro-console-fields";

describe("console timestamp presentation", () => {
  it("shows explicit instants in Beijing time across day and year boundaries", () => {
    expect(consoleValue("created_at", "2026-10-23T16:00:00Z")).toBe(
      "2026-10-24 00:00:00",
    );
    expect(consoleValue("paid_at", "2026-12-31T20:01:02.123456789+02:00")).toBe(
      "2027-01-01 02:01:02",
    );
    expect(
      consoleValue("membership_expires_at", "2026-10-24T02:03:04-07:00"),
    ).toBe("2026-10-24 17:03:04");
    expect(consoleValue("observed_at", "2024-02-29T23:59:59+08:00")).toBe(
      "2024-02-29 23:59:59",
    );
    expect(consoleColumnLabel("created_at", "创建时间")).toBe(
      "创建时间（北京时间）",
    );
    expect(consoleColumnLabel("paid_at", "付款时间（北京时间）")).toBe(
      "付款时间（北京时间）",
    );
  });

  it("does not guess missing offsets, invalid dates, report dimensions or identifiers", () => {
    for (const raw of [
      "2026-02-29T00:00:00Z",
      "1900-02-29T00:00:00Z",
      "2026-04-31T00:00:00Z",
      "2026-10-24T24:00:00Z",
      "2026-10-24T12:00:60Z",
      "2026-10-24T12:00:00+24:00",
      "2026-10-24T12:00:00-00:00",
      "2026-10-24T12:00:00",
      "2026-10-24 12:00:00",
      "2026-10-24",
      "invalid",
    ]) {
      expect(consoleValue("created_at", raw)).toBe(raw);
    }
    for (const key of [
      "id",
      "trade_no",
      "dimension",
      "name",
      "created_at_metadata",
    ]) {
      expect(consoleValue(key, "2026-10-23T16:00:00Z")).toBe(
        "2026-10-23T16:00:00Z",
      );
      expect(consoleColumnLabel(key, "原标签")).toBe("原标签");
    }
    expect(consoleValue("created_at", null)).toBe("—");
    expect(consoleValue("created_at", undefined)).toBe("—");
    expect(consoleValue("created_at", "")).toBe("—");
  });

  it("handles Gregorian leap centuries and explicit UTC without host timezone inference", () => {
    expect(consoleValue("created_at", "2000-02-29T00:00:00+00:00")).toBe(
      "2000-02-29 08:00:00",
    );
    expect(consoleValue("created_at", "2026-03-08T01:30:00-08:00")).toBe(
      "2026-03-08 17:30:00",
    );
    expect(consoleValue("created_at", "2026-11-01T01:30:00-07:00")).toBe(
      "2026-11-01 16:30:00",
    );
  });

  it("formats without changing original API timestamps or export data", () => {
    const row = Object.freeze({
      created_at: "2026-10-23T20:03:04.567Z",
      trade_no: "9007199254740993123",
    });
    const original = JSON.stringify(row);
    expect(consoleValue("created_at", row.created_at)).toBe(
      "2026-10-24 04:03:04",
    );
    expect(consoleValue("trade_no", row.trade_no)).toBe(row.trade_no);
    expect(JSON.stringify(row)).toBe(original);
  });
});
