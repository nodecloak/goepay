// @vitest-environment jsdom
import { it, expect, vi } from "vitest";
import { mount, flushPromises } from "@vue/test-utils";
import ReportPage from "../src/components/ReportPage.vue";
import { ApiClient } from "../src/api";

it("renders the complaint overview with complaint counts and exact amounts instead of payment indicators", async () => {
  const client = new ApiClient("admin");
  const paths: string[] = [];
  vi.spyOn(client, "request").mockImplementation(async (path) => {
    paths.push(path);
    return {
      columns: [
        { key: "dimension", label: "商户ID" },
        { key: "today_count", label: "今日投诉总条数" },
        { key: "today_money", label: "今日投诉金额（元）" },
        { key: "complaint_rate", label: "近7日投诉/支付订单（%）" },
      ],
      items: [
        {
          dimension: "1000",
          today_count: "2",
          today_money: "123.50",
          complaint_rate: null,
        },
      ],
      total: 1,
      source: "local_registered_complaints",
      summary: {
        today_count: "2",
        today_money: "123.50",
        today_alipay: "1",
        today_wxpay: "1",
        yesterday_count: "1",
        yesterday_money: "9.87",
        yesterday_alipay: "1",
        yesterday_wxpay: "0",
      },
    } as never;
  });
  const w = mount(ReportPage, { props: { client, report: "complaints" } });
  await flushPromises();
  expect(paths[0]).toContain("view=overview");
  expect(
    w
      .findAll(".stat")
      .map((x) => x.text())
      .join(" "),
  ).toContain("今日投诉");
  expect(w.text()).toContain("本地已登记投诉");
  expect(w.findAll("thead th").map((x) => x.text())).toEqual([
    "商户ID",
    "今日投诉总条数",
    "今日投诉金额（元）",
    "近7日投诉/支付订单（%）",
  ]);
  expect(w.text()).not.toContain("平台手续费");
  expect(w.find('input[type="date"]').exists()).toBe(false);
  expect(w.get("tbody").text()).toContain("123.50");
  w.unmount();
});
