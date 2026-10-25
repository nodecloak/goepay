import { it, expect } from "vitest";
import { merchantPaymentStatisticsFixture as fixture } from "./fixtures/merchant-payment-statistics";
import * as api from "../src/merchant-payment-statistics";
const read = (value: unknown, id: string) => {
  expect(typeof api.readMerchantPaymentStatistics).toBe("function");
  return api.readMerchantPaymentStatistics(value, id);
};
it("merchant statistics preserve all seven days and exact money and weighted rates", () => {
  const data = fixture(),
    result = read(data, data.merchant_id);
  expect(result.trend).toHaveLength(7);
  expect(result.days.today.summary.money).toBe("90071992547409.94");
  expect(result.days.today.summary.rate).toBe("66.67");
  expect(result.days.yesterday.summary.rate).toBe("50.00");
});
it("merchant scope never accepts another identity or group statistics context", () => {
  const data = fixture();
  for (const id of ["1", "0", "01", "9223372036854775808", ""])
    expect(() => read(data, id)).toThrow();
  for (const change of [
    (d: any) => (d.merchant_id = 9007199254740993),
    (d: any) => (d.cohort = "current_effective_group"),
    (d: any) => (d.timezone = "UTC"),
  ]) {
    const bad = structuredClone(data);
    change(bad);
    expect(() => read(bad, data.merchant_id)).toThrow();
  }
});
it("merchant report rejects date gaps, inconsistent day copies, unsafe money and inconsistent totals", () => {
  const data = fixture();
  for (const change of [
    (d: any) => d.trend.pop(),
    (d: any) => (d.trend[0].date = "2026-10-21"),
    (d: any) => (d.trend[0].date = "2026-02-30"),
    (d: any) => (d.days.yesterday = d.trend[4]),
    (d: any) => (d.days.today.items[0].money = 90071992547409.93),
    (d: any) => (d.days.today.items[1].type = "alipay"),
    (d: any) => (d.days.today.summary.money = "90071992547409.93"),
    (d: any) => (d.days.today.items[1].rate = "100.00"),
  ]) {
    const bad = structuredClone(data);
    change(bad);
    expect(() => read(bad, data.merchant_id)).toThrow();
  }
});
it("trend coordinates use exact integer scaling without rounding the displayed financial data", () => {
  const data = fixture(),
    result = read(data, data.merchant_id);
  expect(typeof api.paymentTrendSeries).toBe("function");
  const series = api.paymentTrendSeries(result.trend);
  expect(series).toHaveLength(3);
  expect(series[0]!.points.at(-1)).toEqual({
    x: 650,
    y: 30,
    date: "2026-10-26",
    money: "90071992547409.93",
  });
  expect(series[1]!.points.at(-1)!.money).toBe("0.01");
  expect(series[1]!.points.at(-1)!.y).toBe(250);
  expect(
    series
      .flatMap((s: any) => s.points)
      .every((p: any) => p.y >= 30 && p.y <= 250),
  ).toBe(true);
});
