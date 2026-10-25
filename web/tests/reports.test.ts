import { describe, it, expect } from "vitest";
import { reportFor, csvExport, validDateRange } from "../src/reports";
describe("report views", () => {
  it("routes original statistics pages to their report dimensions", () => {
    expect(reportFor("admin", "/trade/stat/daily")).toBe("daily");
    expect(reportFor("merchant", "/trade/daily-stat")).toBe("daily");
    expect(reportFor("admin", "/transfer/user-stat")).toBe(
      "transfer-merchants",
    );
  });
  it("prevents invalid dates and inverted date intervals", () => {
    expect(validDateRange("2026-02-30", "2026-03-01")).toBe(false);
    expect(validDateRange("2026-10-23", "2026-10-22")).toBe(false);
    expect(validDateRange("", "")).toBe(true);
    expect(validDateRange("2025-01-01", "2027-01-01")).toBe(false);
  });
  it("exports only server fields and prevents CSV formula execution", () => {
    expect(
      csvExport(
        [
          { key: "name", label: "名称" },
          { key: "money", label: "金额" },
        ],
        [{ name: '=HYPERLINK("evil")', money: "0.01" }],
      ),
    ).toContain('"\'=HYPERLINK(""evil"")"');
    expect(
      csvExport(
        [{ key: "money", label: "金额" }],
        [{ money: "123456789012345.01" }],
      ),
    ).toContain("123456789012345.01");
  });
});
