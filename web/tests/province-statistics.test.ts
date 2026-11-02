import { describe, it, expect } from "vitest";
import {
  provinceRange,
  provinceStatisticsQuery,
  provinceStatisticsData,
  provincePaths,
} from "../src/province-statistics";
describe("original province statistics", () => {
  it("restores six Beijing business-day ranges", () => {
    const now = new Date("2026-10-25T16:01:00Z");
    expect(provinceRange("today", now)).toEqual({
      from: "2026-10-26",
      to: "2026-10-26",
    });
    expect(provinceRange("yesterday", now)).toEqual({
      from: "2026-10-25",
      to: "2026-10-25",
    });
    expect(provinceRange("3days", now)).toEqual({
      from: "2026-10-24",
      to: "2026-10-26",
    });
    expect(provinceRange("15days", now)).toEqual({
      from: "2026-10-12",
      to: "2026-10-26",
    });
    expect(provinceRange("other", now)).toBeNull();
  });
  it("rejects inherited object names as date presets", () => {
    expect(provinceRange("__proto__")).toBeNull();
    expect(provinceRange("constructor")).toBeNull();
  });
  it("keeps administrator IDs exact and rejects merchant filters", () => {
    expect(
      provinceStatisticsQuery(
        "admin",
        "7days",
        "9007199254740993",
        "7",
      ).query?.get("uid"),
    ).toBe("9007199254740993");
    expect(
      provinceStatisticsQuery("merchant", "today", "1", "").error,
    ).toBeTruthy();
    expect(
      provinceStatisticsQuery("merchant", "today", "", "").query?.has("uid"),
    ).toBe(false);
    expect(
      provinceStatisticsQuery("admin", "today", "01", "").error,
    ).toBeTruthy();
  });
  it("combines only explicit region aliases, preserving unknown amounts and BigInt ranking", () => {
    const data = provinceStatisticsData([
      { name: "广东", money: "90071992547409.93", count: "9007199254740993" },
      { name: "广东省", money: "0.01", count: "1" },
      { name: "北京市", money: "99.00", count: "1" },
      { name: "未知", money: "4.00", count: "2" },
      { name: "广东省某地区", money: "5.00", count: "1" },
    ]);
    expect(data.rank[0].name).toBe("广东省");
    expect(data.rank[0].money).toBe("90071992547409.94");
    expect(data.rank[0].count).toBe("9007199254740994");
    expect(data.unmappedMoney).toBe("9.00");
    expect(data.unmappedCount).toBe("3");
    expect(data).toMatchObject({
      unmapped: [
        { name: "未知", money: "4.00", count: "2" },
        { name: "广东省某地区", money: "5.00", count: "1" },
      ],
    });
    expect(data.regions.find((r) => r.name === "广西壮族自治区")?.money).toBe(
      "0.00",
    );
    expect(() =>
      provinceStatisticsData([{ name: "广东省", money: 1, count: "1" }]),
    ).toThrow();
  });
  it("uses the supplied original local geometry without fetching a map", () => {
    const paths = provincePaths();
    expect(paths.length).toBe(35);
    expect(paths.filter((p) => p.label)).toHaveLength(34);
    expect(
      paths
        .filter((p) => p.label)
        .every(
          (p) =>
            p.label!.x >= 0 &&
            p.label!.x <= 720 &&
            p.label!.y >= 0 &&
            p.label!.y <= 540,
        ),
    ).toBe(true);
    expect(paths.find((p) => p.name === "广东省")?.path).toMatch(/^M/);
    expect(
      paths.every(
        (p) => !p.path.includes("NaN") && !p.path.includes("Infinity"),
      ),
    ).toBe(true);
  });
  it("keeps unnamed map outlines out of selectable regions without losing unknown transactions", () => {
    const data = provinceStatisticsData([
      { name: "", money: "1.23", count: "2" },
      { name: "广东", money: "2.00", count: "1" },
    ]);
    expect(data.regions).toHaveLength(34);
    expect(data.regions.every((region) => region.name.trim())).toBe(true);
    expect(data.unmappedMoney).toBe("1.23");
    expect(data.unmappedCount).toBe("2");
    expect(data.rank.map((region) => region.name)).toEqual(["广东省"]);
    // Geographic outlines remain available for drawing, including decoration.
    expect(provincePaths()).toHaveLength(35);
  });
});
