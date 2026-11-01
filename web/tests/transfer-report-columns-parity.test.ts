import { afterEach, expect, it, vi } from "vitest";
import { mount, flushPromises } from "@vue/test-utils";
import { ApiClient } from "../src/api";
import ReportPage from "../src/components/ReportPage.vue";
import { reportColumns, reportRows, type ReportColumn } from "../src/reports";

const accountColumns: ReportColumn[] = [
  { key: "dimension", label: "付款账号" },
  { key: "name", label: "姓名" },
  { key: "orders", label: "付款笔数" },
  { key: "paid_orders", label: "成功笔数" },
  { key: "paid_money", label: "付款金额（元）" },
  { key: "cost_money", label: "花费金额（元）" },
];
const channelColumns: ReportColumn[] = [
  { key: "dimension", label: "通道ID" },
  { key: "orders", label: "总笔数" },
  { key: "paid_orders", label: "成功笔数" },
  { key: "paid_money", label: "成功金额（元）" },
  { key: "net_money", label: "成功出款扣款（元）" },
  { key: "fee_money", label: "平台手续费（元）" },
];
afterEach(() => vi.restoreAllMocks());

it("aligns the account report's first four columns and preserves the additional metrics", () => {
  const columns = reportColumns("transfer-accounts", accountColumns);
  expect(columns.map((c) => c.key)).toEqual([
    "dimension",
    "name",
    "orders",
    "paid_money",
    "paid_orders",
    "cost_money",
    "success_rate",
  ]);
  expect(columns.slice(0, 4).map((c) => c.label)).toEqual([
    "付款账号",
    "姓名",
    "付款笔数",
    "付款金额",
  ]);
  expect(columns.slice(4).map((c) => c.label)).toEqual([
    "成功笔数",
    "花费金额（元）",
    "成功率",
  ]);
  expect(accountColumns[4].label).toBe("付款金额（元）");
});

it("aligns safe channel identities/counts without presenting gross platform fees as net profit", () => {
  const columns = reportColumns("transfer-channels", channelColumns);
  expect(columns.map((c) => c.key)).toEqual([
    "dimension",
    "paid_money",
    "fee_money",
    "orders",
    "paid_orders",
    "net_money",
    "success_rate",
  ]);
  expect(columns.map((c) => c.label)).toEqual([
    "付款通道",
    "付款金额",
    "平台手续费（元）",
    "付款总数",
    "成功数量",
    "成功出款扣款（元）",
    "成功率",
  ]);
  expect(columns.some((c) => /利润/.test(c.label))).toBe(false);
  expect(channelColumns[0].label).toBe("通道ID");
});

it("keeps merchant transfer and non-transfer report presentation unchanged", () => {
  expect(
    reportColumns("transfer-merchants", channelColumns).map((c) => c.key),
  ).toEqual([
    "dimension",
    "paid_money",
    "net_money",
    "fee_money",
    "orders",
    "paid_orders",
    "success_rate",
  ]);
  expect(
    reportColumns("transfer-merchants", channelColumns).find(
      (c) => c.key === "orders",
    )!.label,
  ).toBe("总笔数");
  expect(
    reportColumns("channels", channelColumns).find(
      (c) => c.key === "dimension",
    )!.label,
  ).toBe("通道ID");
  expect(reportColumns("channels", channelColumns).at(-1)!.label).toBe(
    "支付成功率",
  );
});

it("retains exact source metric strings while deriving only the existing success percentage", () => {
  const row = {
    dimension: "9007199254740993123",
    name: "王*",
    orders: "5",
    paid_orders: "2",
    paid_money: "9007199254740993.01",
    fee_money: "0.03",
    net_money: "9007199254740993.04",
  };
  expect(reportRows([row])).toEqual([{ ...row, success_rate: "40.00%" }]);
  expect(reportRows([row])[0]).not.toHaveProperty("profit_money");
});

it.each([
  {
    report: "transfer-accounts",
    columns: accountColumns,
    expected: [
      "付款账号",
      "姓名",
      "付款笔数",
      "付款金额",
      "成功笔数",
      "花费金额（元）",
      "成功率",
    ],
  },
  {
    report: "transfer-channels",
    columns: channelColumns,
    expected: [
      "付款通道",
      "付款金额",
      "平台手续费（元）",
      "付款总数",
      "成功数量",
      "成功出款扣款（元）",
      "成功率",
    ],
  },
])(
  "renders $report using the aligned columns without changing server metric data",
  async ({ report, columns, expected }) => {
    const client = new ApiClient("admin");
    const request = vi.spyOn(client, "request").mockResolvedValue({
      columns,
      items: [
        {
          dimension: "9007199254740993",
          name: "王*",
          orders: "5",
          paid_orders: "2",
          paid_money: "1.00",
          fee_money: "0.03",
          net_money: "1.03",
          cost_money: "1.03",
        },
      ],
      total: 1,
    } as never);
    const w = mount(ReportPage, { props: { client, report } });
    try {
      await flushPromises();
      expect(request).toHaveBeenCalledWith(
        expect.stringContaining("/reports/" + report),
        expect.anything(),
      );
      expect(w.findAll("thead th").map((th) => th.text())).toEqual(
        expected.map((label) => label.replace("（元）", "")),
      );
      expect(w.text()).toContain("9007199254740993");
      expect(w.text()).toContain("40.00%");
      expect(w.text()).not.toContain("手续费利润");
    } finally {
      w.unmount();
    }
  },
);
