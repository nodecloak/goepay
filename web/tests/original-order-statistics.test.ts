import { describe, it, expect } from "vitest";
import {
  originalStatisticsQuery,
  statisticsMoney,
  statisticsTrend,
} from "../src/original-order-statistics";
describe("original order statistics", () => {
  it("keeps original merchant daily filters within the current session", () => {
    const filters = { type: "wxpay", channel: "", uid: "", from: "", to: "" };
    expect(
      originalStatisticsQuery("daily", filters, "merchant").query?.toString(),
    ).toBe("view=original&type=wxpay");
    expect(
      originalStatisticsQuery("daily", { ...filters, uid: "1" }, "merchant")
        .error,
    ).toBeTruthy();
    expect(
      originalStatisticsQuery("daily", { ...filters, channel: "7" }, "merchant")
        .error,
    ).toBeTruthy();
    expect(
      originalStatisticsQuery("channels", filters, "merchant").error,
    ).toBeTruthy();
  });
  it("uses merchant original income as the second trend series without private profit", () => {
    const series = statisticsTrend(
      [
        {
          day: "2026-10-25",
          realmoney: "90071992547409.93",
          getmoney: "81064793292668.94",
          refundmoney: "1.00",
          profitmoney: "123.00",
        },
      ],
      "merchant",
    );
    expect(series.map((s) => s.name)).toEqual([
      "订单金额",
      "分成金额",
      "退款金额",
    ]);
    expect(series[1].points[0].value).toBe("81064793292668.94");
    expect(series.some((s) => s.key === "profitmoney")).toBe(false);
  });
  it("preserves original dimensions and canonical large IDs without implicit date scope", () => {
    const q = originalStatisticsQuery("daily", {
      type: "alipay",
      channel: "9007199254740993",
      uid: "1",
      from: "",
      to: "",
    });
    expect(q.error).toBeUndefined();
    expect(q.query?.get("channel")).toBe("9007199254740993");
    expect(q.query?.get("view")).toBe("original");
    expect(q.query?.has("from")).toBe(false);
    expect(
      originalStatisticsQuery("channels", {
        type: "",
        channel: "",
        uid: "01",
        from: "2026-10-25",
        to: "2026-10-25",
      }).error,
    ).toBeTruthy();
    expect(
      originalStatisticsQuery("channels", {
        type: "",
        channel: "",
        uid: "",
        from: "2026-02-30",
        to: "2026-03-01",
      }).error,
    ).toBeTruthy();
  });
  it("keeps exact money and represents missing costs as unknown", () => {
    expect(statisticsMoney("90071992547409.93")).toBe("90071992547409.93");
    expect(statisticsMoney(null)).toBe("未知");
    expect(statisticsMoney(1)).toBe("未知");
    expect(statisticsMoney("-0.03")).toBe("-0.03");
  });
  it("plots gaps for unknown profit and scales large exact amounts before Number conversion", () => {
    const series = statisticsTrend([
      {
        day: "2026-10-24",
        realmoney: "90071992547409.93",
        refundmoney: "0.00",
        profitmoney: "-0.01",
      },
      {
        day: "2026-10-25",
        realmoney: "1.00",
        refundmoney: "0.00",
        profitmoney: null,
      },
      {
        day: "2026-10-26",
        realmoney: "2.00",
        refundmoney: "0.00",
        profitmoney: "0.01",
      },
    ]);
    expect(series).toHaveLength(3);
    expect(series[0].points[0].value).toBe("90071992547409.93");
    expect(series[2].segments).toHaveLength(2);
    expect(series[2].points).toHaveLength(2);
    expect(
      series
        .flatMap((s) => s.points)
        .every((p) => Number.isFinite(p.x) && Number.isFinite(p.y)),
    ).toBe(true);
  });
});
