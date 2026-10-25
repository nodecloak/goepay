import { it, expect } from "vitest";
import { readGroupStatistics } from "../src/group-statistics";
const fixture = () => {
  const items = ["alipay", "wxpay", "sandbox"].map((type, index) => ({
    type,
    showname: ["支付宝", "微信支付", "本地模拟"][index],
    money: "0.00",
    count: "0",
    success_count: "0",
    rate: "0.00",
  }));
  return {
    group_id: "9007199254740993",
    group_name: "本地组",
    timezone: "Asia/Shanghai",
    cohort: "current_effective_group",
    columns: [
      { key: "showname", label: "支付方式" },
      { key: "money", label: "订单金额" },
      { key: "count", label: "订单数量" },
      { key: "rate", label: "成功率" },
    ],
    days: Object.fromEntries(
      ["today", "yesterday"].map((key) => [
        key,
        {
          date: key === "today" ? "2026-10-26" : "2026-10-25",
          items: structuredClone(items),
          summary: {
            type: "total",
            showname: "总计",
            money: "0.00",
            count: "0",
            success_count: "0",
            rate: "0.00",
          },
        },
      ]),
    ),
  };
};
it("keeps original group context and exact money strings above the JS safe integer range", () => {
  const data = fixture();
  data.days.today.items[0] = {
    type: "alipay",
    showname: "支付宝",
    money: "90071992547409.93",
    count: "3",
    success_count: "2",
    rate: "66.67",
  };
  data.days.today.summary = {
    ...data.days.today.summary,
    money: "90071992547409.93",
    count: "3",
    success_count: "2",
    rate: "66.67",
  };
  expect(
    readGroupStatistics(data, data.group_id).days.today.summary.money,
  ).toBe("90071992547409.93");
});
it("refuses a different group, unsafe numeric amounts, duplicate methods and inconsistent totals", () => {
  const data = fixture();
  expect(() => readGroupStatistics(data, "1")).toThrow();
  for (const change of [
    (d: any) => (d.days.today.items[0].money = 90071992547409.93),
    (d: any) => (d.days.today.items[1].type = "alipay"),
    (d: any) => (d.days.today.summary.count = "1"),
    (d: any) => (d.days.today.items[0].rate = "101.00"),
    (d: any) => (d.days.today.date = "2026-02-30"),
  ]) {
    const bad = structuredClone(data);
    change(bad);
    expect(() => readGroupStatistics(bad, data.group_id)).toThrow();
  }
});
