import { expect, it, vi } from "vitest";
import { flushPromises, mount } from "@vue/test-utils";
import ComplaintsPage from "../src/components/ComplaintsPage.vue";
import { ApiClient } from "../src/api";

// Pro 交易投诉 parity across the four views (admin 投诉记录/投诉统计/同步任务,
// merchant 投诉记录), built on the design kit over the existing complaints
// resource + report + complaint-tasks reads.

const complaint = {
  id: "12",
  merchant_id: "1000",
  trade_no: "T2026102800001",
  subject: "未收到商品",
  content: "下单后未发货，要求退款",
  status: "open",
  pay_type: "alipay",
  amount: "1200",
  reply: "",
  times: "2",
  third_mch_id: "2088MCH",
  created_at: "2026-10-28T09:00:00Z",
  updated_at: "2026-10-28T09:00:00Z",
};

const render = (
  request: (...args: any[]) => any,
  area: "admin" | "merchant",
  path: string,
) =>
  mount(ComplaintsPage, {
    props: {
      client: Object.assign(new ApiClient(area), { request }),
      area,
      path,
    },
    global: { stubs: { Teleport: true } },
  });

function listRequest(extra?: (...args: any[]) => any) {
  return vi.fn(async (path: string, options?: any) => {
    if (extra) {
      const r = await extra(path, options);
      if (r !== undefined) return r;
    }
    if (/\/complaints133\/12$/.test(path.split("?")[0]!))
      return { ...complaint, images: [], negotiation: [] };
    if (path.startsWith("/complaints133"))
      return { items: [complaint], total: 1 };
    if (path.startsWith("/complaints-summary"))
      return {
        summary: {
          today_count: "3",
          today_alipay: "2",
          today_wxpay: "1",
          yesterday_count: "1",
          yesterday_alipay: "0",
          yesterday_wxpay: "1",
        },
      };
    return { items: [], total: 0 };
  });
}

it("renders the Pro 投诉记录 filters, columns, 投诉次数 and summary banner (admin)", async () => {
  const w = render(listRequest(), "admin", "/complain/list");
  await flushPromises();
  // Pro filter fields.
  expect(w.find('[aria-label="搜索字段"]').exists()).toBe(true);
  expect(w.find('[aria-label="搜索内容"]').exists()).toBe(true);
  expect(w.find('[aria-label="状态"]').exists()).toBe(true);
  // Summary banner (today/yesterday above the list, from /complaints-summary).
  expect(w.find('[data-test="complain-summary"]').exists()).toBe(true);
  expect(w.find('[data-test="complain-summary"]').text()).toContain("今日投诉");
  // Columns include 投诉次数.
  const headers = w.findAll("thead th").map((h) => h.text());
  expect(headers).toEqual([
    "ID",
    "商户号",
    "关联订单号",
    "问题类型",
    "订单金额",
    "投诉详情",
    "创建时间",
    "状态",
    "投诉次数",
    "操作",
  ]);
  const body = w.get("tbody").text();
  expect(body).toContain("12");
  expect(body).toContain("T2026102800001");
  expect(body).toContain("¥12.00");
  expect(body).toContain("待处理");
  expect(w.get('[data-action="detail"]').text()).toBe("处理投诉");
  w.unmount();
});

it("opens the 处理投诉 dialog with Pro fields and reply/supplement/refund actions", async () => {
  const w = render(listRequest(), "admin", "/complain/list");
  await flushPromises();
  await w.get('[data-action="detail"]').trigger("click");
  await flushPromises();
  expect(w.find('[aria-label="回复内容"]').exists()).toBe(true);
  expect(w.find('[aria-label="退款金额"]').exists()).toBe(true);
  expect(w.get('[data-action="reply"]').text()).toBe("回复用户");
  // Alipay complaint offers 补充凭证.
  expect(w.get('[data-action="supplement"]').text()).toBe("补充凭证");
  expect(w.get('[data-action="refund"]').text()).toBe("退款审批");
  // 处理结果 presets fill 回复用户.
  expect(w.findAll('[data-action="preset"]').length).toBeGreaterThan(0);
  w.unmount();
});

it("drives provider reply/complete and refund through the existing refund flow", async () => {
  const calls: Array<{ path: string; body: any }> = [];
  const request = listRequest(async (path: string, options?: any) => {
    if (options?.method === "POST") {
      calls.push({ path, body: options.body });
      return { recorded: true };
    }
    return undefined;
  });
  const w = render(request, "admin", "/complain/list");
  await flushPromises();
  await w.get('[data-action="detail"]').trigger("click");
  await flushPromises();
  // 回复用户 -> provider-reply endpoint.
  await w.get('[aria-label="回复内容"]').setValue("已联系商户");
  await w.get('[data-action="reply"]').trigger("click");
  await flushPromises();
  expect(
    calls.some((c) => /\/complaints\/12\/provider-reply$/.test(c.path)),
  ).toBe(true);
  // 退款审批 -> existing order refund flow (/orders/:trade/refund).
  await w.get('[data-action="detail"]').trigger("click");
  await flushPromises();
  await w.get('[aria-label="退款金额"]').setValue("12.00");
  await w.get('[data-action="refund"]').trigger("click");
  await flushPromises();
  const refund = calls.find((c) =>
    /\/orders\/T2026102800001\/refund$/.test(c.path),
  );
  expect(refund).toBeTruthy();
  expect(refund!.body.amount).toBe("12.00");
  // 渠道处理并完成 (modal submit) -> provider-complete + local review.
  await w.get('[data-action="detail"]').trigger("click");
  await flushPromises();
  await w.get("form.pa-modal-body").trigger("submit");
  await flushPromises();
  expect(
    calls.some((c) => /\/complaints\/12\/provider-complete$/.test(c.path)),
  ).toBe(true);
  w.unmount();
});

it("renders the Pro 投诉统计 summary cards and ranking columns (admin)", async () => {
  const request = vi.fn(async (path: string) => {
    if (path.startsWith("/complaints-charts"))
      return {
        items: [{ dimension: "1000", today_count: "2" }],
        total: 1,
        summary: {
          today_count: "2",
          today_alipay: "1",
          today_wxpay: "1",
          yesterday_count: "0",
          yesterday_alipay: "0",
          yesterday_wxpay: "0",
        },
        columns: [
          { key: "dimension", label: "商户ID" },
          { key: "today_count", label: "今日投诉总条数" },
        ],
      };
    return { items: [], total: 0 };
  });
  const w = render(request, "admin", "/complain/charts");
  await flushPromises();
  expect(w.text()).toContain("今日投诉");
  expect(w.text()).toContain("昨日投诉");
  const headers = w.findAll("thead th").map((h) => h.text());
  expect(headers).toContain("商户ID");
  expect(headers).toContain("今日投诉总条数");
  w.unmount();
});

it("renders the Pro 同步任务 columns from /complaint-sync-tasks (admin)", async () => {
  const request = vi.fn(async (path: string) => {
    if (path.startsWith("/complaint-sync-tasks"))
      return {
        items: [
          {
            channel_id: "7",
            name: "微信直连",
            provider: "wxpay",
            enabled: true,
            frequency: 6,
            num: 20,
            consecutive_failures: 0,
            last_run_at: "2026-10-28T09:00:00Z",
            last_status: "ok",
            last_reason: "",
            next_run_at: "2026-10-28T09:06:00Z",
          },
        ],
        total: 1,
      };
    return { items: [], total: 0 };
  });
  const w = render(request, "admin", "/complain/task");
  await flushPromises();
  const headers = w.findAll("thead th").map((h) => h.text());
  expect(headers).toEqual([
    "通道ID",
    "通道名称",
    "状态",
    "上次同步时间",
    "同步状态",
    "下次同步时间",
    "获取频率",
  ]);
  const body = w.get("tbody").text();
  expect(body).toContain("微信直连");
  expect(body).toContain("6 分钟"); // frequency in minutes, not seconds
  w.unmount();
});

it("toggles 自动获取 for the selected channel through PUT /complaint-sync-tasks (admin)", async () => {
  const calls: Array<{ path: string; method: string; body: any }> = [];
  let enabled = false;
  const request = vi.fn(async (path: string, options?: any) => {
    if (path.startsWith("/complaint-channels"))
      return {
        items: [
          { channel_id: "7", name: "微信直连", provider: "wxpay", enabled },
        ],
      };
    if (options?.method === "PUT") {
      calls.push({ path, method: "PUT", body: options.body });
      enabled = options.body.enabled;
      return { channel_id: "7", enabled, frequency: 6, num: 20 };
    }
    return { items: [], total: 0 };
  });
  const w = render(request, "admin", "/complain/list");
  await flushPromises();
  // 自动获取 control present for admin.
  expect(w.find('[aria-label="自动获取通道"]').exists()).toBe(true);
  // Select the channel and open auto-fetch.
  (w.vm as any).autoChannel = "7";
  await flushPromises();
  await w.get('[data-action="auto-fetch"]').trigger("click");
  await flushPromises();
  const put = calls.find((c) => /\/complaint-sync-tasks\/7$/.test(c.path));
  expect(put).toBeTruthy();
  expect(put!.body.enabled).toBe(true);
  w.unmount();
});

it("merchant 投诉记录 loads on mount with the same list view", async () => {
  const w = render(listRequest(), "merchant", "/complain");
  await flushPromises();
  const headers = w.findAll("thead th").map((h) => h.text());
  expect(headers[0]).toBe("ID");
  expect(w.get("tbody").text()).toContain("T2026102800001");
  w.unmount();
});
