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
            days: 30,
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
    "",
    "通道ID",
    "通道名称",
    "状态",
    "上次同步时间",
    "同步状态",
    "下次同步时间",
    "获取条件",
    "获取频率",
    "操作",
  ]);
  const body = w.get("tbody").text();
  expect(body).toContain("微信直连");
  expect(body).toContain("6 分钟"); // frequency in minutes, not seconds
  expect(body).toContain("30 天内有订单"); // 获取条件
  // Per-row 编辑 / 删除 actions present.
  expect(w.get('[data-action="edit-task"]').text()).toBe("编辑");
  expect(w.get('[data-action="delete-task"]').text()).toBe("删除");
  w.unmount();
});

it("edits a sync task through PUT /complaint-sync-tasks (days/frequency/num)", async () => {
  const calls: Array<{ path: string; method: string; body: any }> = [];
  const task = {
    channel_id: "7",
    name: "微信直连",
    provider: "wxpay",
    enabled: true,
    days: 30,
    frequency: 6,
    num: 20,
    consecutive_failures: 0,
    last_run_at: "2026-10-28T09:00:00Z",
    last_status: "ok",
    last_reason: "",
    next_run_at: "2026-10-28T09:06:00Z",
  };
  const request = vi.fn(async (path: string, options?: any) => {
    if (options?.method === "PUT") {
      calls.push({ path, method: "PUT", body: options.body });
      return { ...task, ...options.body };
    }
    if (path.startsWith("/complaint-sync-tasks"))
      return { items: [task], total: 1 };
    return { items: [], total: 0 };
  });
  const w = render(request, "admin", "/complain/task");
  await flushPromises();
  await w.get('[data-action="edit-task"]').trigger("click");
  await flushPromises();
  await w.get('[aria-label="获取条件天数"]').setValue("14");
  await w.get('[aria-label="获取频率"]').setValue("10");
  await w.get('[aria-label="每次同步获取数量"]').setValue("50");
  await w.get("form.pa-modal-body").trigger("submit");
  await flushPromises();
  const put = calls.find((c) => /\/complaint-sync-tasks\/7$/.test(c.path));
  expect(put).toBeTruthy();
  expect(put!.body).toMatchObject({ days: 14, frequency: 10, num: 50 });
  w.unmount();
});

it("toggles a sync task's status (PATCH) and deletes it (DELETE)", async () => {
  const calls: Array<{ path: string; method: string }> = [];
  const task = {
    channel_id: "7",
    name: "微信直连",
    provider: "wxpay",
    enabled: true,
    days: 0,
    frequency: 6,
    num: 20,
    consecutive_failures: 0,
    last_run_at: "",
    last_status: "",
    last_reason: "",
    next_run_at: "",
  };
  const request = vi.fn(async (path: string, options?: any) => {
    if (options?.method === "PATCH" || options?.method === "DELETE") {
      calls.push({ path, method: options.method });
      return { ...task, enabled: options.body ? options.body.enabled : false };
    }
    if (path.startsWith("/complaint-sync-tasks"))
      return { items: [task], total: 1 };
    return { items: [], total: 0 };
  });
  vi.spyOn(window, "confirm").mockReturnValue(true);
  const w = render(request, "admin", "/complain/task");
  await flushPromises();
  await w.get('[data-action="toggle-status"]').trigger("click");
  await flushPromises();
  expect(
    calls.some(
      (c) => c.method === "PATCH" && /\/complaint-sync-tasks\/7$/.test(c.path),
    ),
  ).toBe(true);
  await w.get('[data-action="delete-task"]').trigger("click");
  await flushPromises();
  expect(
    calls.some(
      (c) => c.method === "DELETE" && /\/complaint-sync-tasks\/7$/.test(c.path),
    ),
  ).toBe(true);
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

it("sends 日期范围 and 通道ID filters in the list query (admin)", async () => {
  const seen: string[] = [];
  const request = vi.fn(async (path: string) => {
    if (path.startsWith("/complaints133")) seen.push(path);
    if (path.startsWith("/complaints133"))
      return { items: [complaint], total: 1 };
    if (path.startsWith("/complaints-summary")) return { summary: null };
    return { items: [], total: 0 };
  });
  const w = render(request, "admin", "/complain/list");
  await flushPromises();
  // 通道ID + 日期范围 live in the collapsible 展开 section.
  await w.get(".pa-filter-toggle").trigger("click");
  await flushPromises();
  expect(w.find('[aria-label="通道ID"]').exists()).toBe(true);
  expect(w.find('[aria-label="开始日期"]').exists()).toBe(true);
  await w.get('[aria-label="通道ID"]').setValue("7");
  await w.get('[aria-label="开始日期"]').setValue("2026-10-01");
  await w.get('[aria-label="结束日期"]').setValue("2026-10-28");
  await w.get("form").trigger("submit");
  await flushPromises();
  const last = seen[seen.length - 1]!;
  expect(last).toContain("channel_id=7");
  expect(last).toContain("starttime=2026-10-01");
  expect(last).toContain("endtime=2026-10-28");
  w.unmount();
});

it("exports the filtered list via downloadCSV with the same filters", async () => {
  const dl = vi
    .spyOn(ApiClient.prototype, "downloadCSV")
    .mockResolvedValue(undefined);
  const w = render(listRequest(), "admin", "/complain/list");
  await flushPromises();
  (w.vm as any).statusFilter = "open";
  await flushPromises();
  await w.get('[data-action="export"]').trigger("click");
  await flushPromises();
  expect(dl).toHaveBeenCalled();
  expect(String(dl.mock.calls[0]![0])).toContain("/complaints133/export");
  expect(String(dl.mock.calls[0]![0])).toContain("status=open");
  dl.mockRestore();
  w.unmount();
});

it("renders 协商历史 and 投诉图片 in the 处理投诉 detail modal", async () => {
  const request = listRequest(async (path: string) => {
    if (/\/complaints133\/12$/.test(path.split("?")[0]!))
      return {
        ...complaint,
        images: ["https://example.test/i1.jpg"],
        negotiation: [
          {
            operator: "商户",
            role: "merchant",
            content: "已安排退款",
            images: ["https://example.test/h1.jpg"],
            created_at: "2026-10-28T10:00:00Z",
          },
        ],
      };
    return undefined;
  });
  const w = render(request, "admin", "/complain/list");
  await flushPromises();
  await w.get('[data-action="detail"]').trigger("click");
  await flushPromises();
  expect(w.text()).toContain("投诉图片");
  expect(w.text()).toContain("协商历史");
  expect(w.text()).toContain("已安排退款");
  expect(w.findAll(".complain-image-link").length).toBeGreaterThanOrEqual(2);
  w.unmount();
});

it("renders the extended charts: 近7日投诉率 card and 昨日/金额/率 columns", async () => {
  const request = vi.fn(async (path: string) => {
    if (path.startsWith("/complaints-charts"))
      return {
        items: [
          {
            dimension: "1000",
            today_count: "2",
            today_money: "12.00",
            today_alipay: "1",
            today_wxpay: "1",
            yesterday_count: "1",
            yesterday_money: "3.00",
            yesterday_alipay: "0",
            yesterday_wxpay: "1",
            complaint_rate: "0.42%",
            total_count: "9",
          },
        ],
        summary: {
          today_count: "2",
          today_alipay: "1",
          today_wxpay: "1",
          yesterday_count: "1",
          yesterday_alipay: "0",
          yesterday_wxpay: "1",
          days7_rate: "0.42%",
        },
        columns: [
          { key: "dimension", label: "商户ID" },
          { key: "today_count", label: "今日投诉总条数" },
          { key: "today_money", label: "今日投诉金额" },
          { key: "yesterday_count", label: "昨日投诉总条数" },
          { key: "yesterday_money", label: "昨日投诉金额" },
          { key: "complaint_rate", label: "近7日投诉率" },
          { key: "total_count", label: "累计投诉" },
        ],
      };
    return { items: [], total: 0 };
  });
  const w = render(request, "admin", "/complain/charts");
  await flushPromises();
  expect(w.text()).toContain("近7日投诉率");
  const headers = w.findAll("thead th").map((h) => h.text());
  expect(headers).toContain("昨日投诉金额");
  expect(headers).toContain("近7日投诉率");
  const body = w.get("tbody").text();
  expect(body).toContain("¥12.00"); // money column prefixed
  expect(body).toContain("0.42%");
  w.unmount();
});
