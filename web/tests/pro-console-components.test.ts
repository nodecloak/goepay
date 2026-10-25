// @vitest-environment jsdom
import { describe, it, expect, vi } from "vitest";
import { mount, flushPromises } from "@vue/test-utils";
import DataTable from "../src/components/DataTable.vue";
import ResourcePage from "../src/components/ResourcePage.vue";
import ReportPage from "../src/components/ReportPage.vue";
import OrderDetails from "../src/components/OrderDetails.vue";
import { ApiClient } from "../src/api";

describe("Pro console field presentation", () => {
  it("presents the original order instant with an explicit Beijing-time column without changing its row", () => {
    const row = Object.freeze({
      trade_no: "9007199254740993123",
      created_at: "2026-10-23T16:00:00Z",
      status: "pending",
    });
    const w = mount(DataTable, {
      props: {
        items: [row],
        columns: [{ key: "created_at", label: "创建时间" }],
        orders: true,
      },
    });
    expect(w.get("thead").text()).toContain("创建时间（北京时间）");
    expect(w.get("tbody").text()).toContain("2026-10-24 00:00:00");
    expect(row.created_at).toBe("2026-10-23T16:00:00Z");
  });
  it("shows proven effective group, masked recipient and approved domains without falling back to mutable legacy plaintext", async () => {
    const client = new ApiClient("admin");
    const record = {
      id: "1000",
      name: "商户",
      group_id: "999",
      effective_group_id: "3",
      effective_group_name: "专业组",
      membership_expires_at: "2026-11-01T00:00:00Z",
      group_membership_active: true,
      created_at: "2026-10-24T00:00:00Z",
      approved_domains: [
        "shop.example.com",
        "shop.example.com",
        "pay.example.com",
      ],
      settlement_recipient: {
        state: "configured",
        config_id: "9007199254740993123",
        type: "alipay",
        app_id: "",
        account_masked: "***1234",
        name_masked: "***",
      },
      account: "legacy-plaintext-account",
      username: "legacy-plaintext-name",
    };
    const fields = [
      { name: "name", label: "名称", type: "string" },
      { name: "group_id", label: "配置用户组", type: "string" },
      ...[
        "effective_group_id",
        "effective_group_name",
        "membership_expires_at",
        "group_membership_active",
        "approved_domains",
        "settlement_recipient",
        "created_at",
      ].map((name) => ({
        name,
        label:
          name === "settlement_recipient"
            ? "结算账号 / 姓名（脱敏）"
            : name === "created_at"
              ? "注册时间 / 域名"
              : name,
        type:
          name === "group_membership_active"
            ? "boolean"
            : name === "approved_domains" || name === "settlement_recipient"
              ? "json"
              : "string",
        read_only: true,
      })),
    ];
    const request = vi.spyOn(client, "request").mockImplementation(
      async (path) =>
        (path.endsWith("/schema")
          ? {
              title: "商户管理",
              fields,
              permissions: { create: false, update: false, delete: false },
            }
          : path.endsWith("/1000")
            ? record
            : { items: [record], total: 1 }) as never,
    );
    const w = mount(ResourcePage, { props: { client, resource: "merchants" } });
    await flushPromises();
    expect(w.findAll("thead th").map((h) => h.text())).toEqual([
      "商户号 / 用户组",
      "名称",
      "结算账号 / 姓名（脱敏）",
      "注册时间 / 域名（北京时间）",
      "操作",
    ]);
    expect(w.text()).toContain("专业组（3）");
    expect(w.text()).toContain("成员有效期至（北京时间）：2026-11-01 08:00:00");
    expect(w.text()).toContain("支付宝 · ***1234 / ***");
    expect(
      w
        .findAll("tbody td")
        .filter((td) => td.text().includes("shop.example.com")),
    ).toHaveLength(1);
    expect(w.text()).not.toContain("legacy-plaintext");
    expect(w.text()).not.toContain("配置用户组");
    await w
      .findAll("button")
      .find((b) => b.text() === "详情")!
      .trigger("click");
    await flushPromises();
    expect(w.get('[role="dialog"]').text()).not.toContain('"account_masked"');
    expect(w.get('[role="dialog"]').text()).toContain("支付宝 · ***1234 / ***");
    expect(request).toHaveBeenCalledTimes(3);
    w.unmount();
  });
  it("separates merchant available, reserved and income-held projections and never submits read-only balances", async () => {
    const client = new ApiClient("admin");
    const record = {
      id: "9007199254740993123",
      name: "测试商户",
      available_minor: "9007199254740993",
      frozen_minor: "123",
      income_held_minor: "456",
      created_at: "2026-10-24T00:00:00Z",
      effective_group_id: "3",
      effective_group_name: "专业组",
      membership_expires_at: "2026-11-01T00:00:00Z",
      group_membership_active: true,
      approved_domains: ["shop.example.com"],
      settlement_recipient: { state: "missing" },
    };
    const schema = {
      title: "商户管理",
      fields: [
        { name: "name", label: "名称", type: "string" },
        ...[
          "effective_group_id",
          "effective_group_name",
          "membership_expires_at",
          "group_membership_active",
          "approved_domains",
          "settlement_recipient",
        ].map((name) => ({
          name,
          label: name === "settlement_recipient" ? "脱敏收款资料" : name,
          type:
            name === "group_membership_active"
              ? "boolean"
              : name === "approved_domains" || name === "settlement_recipient"
                ? "json"
                : "string",
          read_only: true,
        })),
        {
          name: "available_minor",
          label: "可用余额（分）",
          type: "string",
          read_only: true,
        },
        {
          name: "frozen_minor",
          label: "冻结预留（分）",
          type: "string",
          read_only: true,
        },
        {
          name: "income_held_minor",
          label: "待解冻收入（分）",
          type: "string",
          read_only: true,
        },
        {
          name: "created_at",
          label: "注册时间",
          type: "string",
          read_only: true,
        },
      ],
      permissions: { create: true, update: true, delete: false },
    };
    const requests = vi
      .spyOn(client, "request")
      .mockImplementation(
        async (path, init) =>
          (path.endsWith("/schema")
            ? schema
            : init?.method === "PATCH" || init?.method === "POST"
              ? record
              : path.endsWith(record.id)
                ? record
                : { items: [record], total: 1 }) as never,
      );
    const wrapper = mount(ResourcePage, {
      props: { client, resource: "merchants" },
    });
    await flushPromises();
    expect(wrapper.findAll("thead th").map((h) => h.text())).toEqual([
      "商户号 / 用户组",
      "名称",
      "可用余额（元）",
      "脱敏收款资料",
      "注册时间（北京时间）",
      "操作",
    ]);
    expect(wrapper.text()).toContain("90071992547409.93");
    expect(wrapper.text()).toContain("冻结预留：1.23");
    expect(wrapper.text()).toContain("待解冻收入：4.56");
    await wrapper
      .findAll("button")
      .find((b) => b.text() === "详情")!
      .trigger("click");
    await flushPromises();
    expect(wrapper.get('[role="dialog"]').text()).toContain(
      "冻结预留（元）1.23",
    );
    expect(wrapper.get('[role="dialog"]').text()).toContain(
      "待解冻收入（元）4.56",
    );
    await wrapper.get('[role="dialog"] button').trigger("click");
    await wrapper
      .findAll("button")
      .find((b) => b.text() === "编辑")!
      .trigger("click");
    await flushPromises();
    expect(wrapper.find("#field-available_minor").exists()).toBe(false);
    expect(wrapper.find("#field-created_at").exists()).toBe(false);
    expect(wrapper.find("#field-settlement_recipient").exists()).toBe(false);
    expect(wrapper.find("#field-effective_group_id").exists()).toBe(false);
    await wrapper.get("#field-name").setValue("更新名称");
    await wrapper.get('[role="dialog"] form').trigger("submit");
    await flushPromises();
    const saved = requests.mock.calls.find(
      ([, init]) => init?.method === "PATCH",
    );
    expect(saved?.[1]?.body).toEqual({ name: "更新名称" });
    await wrapper
      .findAll("button")
      .find((b) => b.text() === "新增商户")!
      .trigger("click");
    expect(wrapper.find("#field-available_minor").exists()).toBe(false);
    expect(wrapper.find("#field-income_held_minor").exists()).toBe(false);
    expect(wrapper.find("#field-approved_domains").exists()).toBe(false);
    await wrapper.get("#field-name").setValue("新增名称");
    await wrapper.get('[role="dialog"] form').trigger("submit");
    await flushPromises();
    expect(
      requests.mock.calls.find(([, init]) => init?.method === "POST")?.[1]
        ?.body,
    ).toEqual({ name: "新增名称" });
    wrapper.unmount();
  });
  it("renders integer minor amounts as exact yuan without rounding long identifiers", () => {
    const wrapper = mount(DataTable, {
      props: {
        columns: [
          { key: "trade_no", label: "系统订单号" },
          { key: "amount", label: "交易金额（分）" },
          { key: "status", label: "支付状态" },
          { key: "type", label: "支付方式" },
        ],
        items: [
          {
            trade_no: "9007199254740993123",
            amount: "9007199254740993",
            status: "paid",
            type: "wxpay",
          },
        ],
        orders: true,
      },
    });
    expect(wrapper.text()).toContain("9007199254740993123");
    expect(wrapper.text()).toContain("90071992547409.93");
    expect(wrapper.text()).toContain("交易金额（元）");
    expect(wrapper.text()).toContain("已支付");
    expect(wrapper.text()).toContain("微信支付");
  });

  it("does not divide already formatted report money or expose credential columns", () => {
    const wrapper = mount(DataTable, {
      props: {
        columns: [
          { key: "paid_money", label: "成功金额（元）" },
          { key: "api_key", label: "API密钥" },
        ],
        items: [
          { paid_money: "123456789012345.01", api_key: "must-never-display" },
        ],
      },
    });
    expect(wrapper.text()).toContain("123456789012345.01");
    expect(wrapper.text()).not.toContain("must-never-display");
    expect(wrapper.text()).not.toContain("API密钥");
  });

  it("shows compact merchant columns in approved order without duplicating its string ID", async () => {
    const client = new ApiClient("admin");
    vi.spyOn(client, "request").mockImplementation(async (path) =>
      path.endsWith("/schema")
        ? {
            title: "商户管理",
            permissions: { create: false, update: false, delete: false },
            fields: [
              {
                name: "id",
                label: "商户 ID",
                type: "string",
                create_only: true,
              },
              { name: "name", label: "名称", type: "string" },
              {
                name: "signature_algorithm",
                label: "签名算法",
                type: "string",
              },
              {
                name: "base_group_id",
                label: "基础组账务快照",
                type: "string",
                read_only: true,
              },
              { name: "enabled", label: "启用", type: "boolean" },
              { name: "email", label: "电子邮箱", type: "email" },
            ],
          }
        : ({
            items: [
              {
                id: "9007199254740993123",
                name: "测试商户",
                enabled: true,
                email: "already-masked@example.invalid",
                base_group_id: "3",
              },
            ],
            total: 1,
          } as never),
    );
    const wrapper = mount(ResourcePage, {
      props: { client, resource: "merchants" },
    });
    await flushPromises();
    expect(wrapper.findAll("thead th").map((h) => h.text())).toEqual([
      "商户号",
      "名称",
      "电子邮箱",
      "启用",
      "签名算法",
      "操作",
    ]);
    expect(wrapper.text()).not.toContain("基础组账务快照");
    expect(wrapper.findAll("tbody td")[0].text()).toBe("9007199254740993123");
    wrapper.unmount();
  });

  it("aligns report columns and derives an exact success percentage without dividing yuan", async () => {
    const client = new ApiClient("merchant");
    vi.spyOn(client, "request").mockResolvedValue({
      columns: [
        { key: "dimension", label: "日期" },
        { key: "orders", label: "总笔数" },
        { key: "paid_orders", label: "成功笔数" },
        { key: "paid_money", label: "成功金额（元）" },
        { key: "net_money", label: "商户净额（元）" },
        { key: "fee_money", label: "平台手续费（元）" },
      ],
      items: [
        {
          dimension: "2026-10-24",
          orders: "3",
          paid_orders: "2",
          paid_money: "1.23",
          net_money: "1.00",
          fee_money: "0.23",
        },
      ],
      total: 1,
    });
    const wrapper = mount(ReportPage, {
      props: { client, report: "daily" },
      global: { stubs: { CsvExportButton: true } },
    });
    await flushPromises();
    expect(wrapper.findAll("thead th").map((h) => h.text())).toEqual([
      "日期",
      "成功金额（元）",
      "商户净额（元）",
      "平台手续费（元）",
      "总笔数",
      "成功笔数",
      "支付成功率",
    ]);
    expect(wrapper.text()).toContain("66.67%");
    expect(wrapper.text()).toContain("1.23");
    expect(wrapper.text()).not.toContain("0.0123");
    wrapper.unmount();
  });

  it("shows order details in yuan with separate notification and payment states", async () => {
    const client = new ApiClient("merchant");
    vi.spyOn(client, "request").mockResolvedValue({
      order: {
        trade_no: "9007199254740993123",
        out_trade_no: "merchant-long-id",
        amount: "123",
        merchant_amount: "100",
        platform_fee: "23",
        type: "wxpay",
        status: "paid",
      },
      reporting: {
        refunded_amount: "1",
        notification_state: "dead",
        api_trade_no: "receipt-original",
        channel_id: "7",
      },
    });
    const wrapper = mount(OrderDetails, {
      props: { client, tradeNo: "9007199254740993123" },
      global: {
        stubs: {
          OrderIncomePanel: true,
          NotificationDeliveryPanel: true,
          OrderPrintingPanel: true,
        },
      },
    });
    await flushPromises();
    expect(wrapper.text()).toContain("1.23");
    expect(wrapper.text()).toContain("订单金额（元）");
    expect(wrapper.text()).toContain("接口订单号");
    expect(wrapper.text()).toContain("receipt-original");
    expect(wrapper.text()).toContain("已支付");
    expect(wrapper.text()).toContain("通知失败");
    wrapper.unmount();
  });

  it("groups original receipt and money details and disables refunds for unpaid orders", () => {
    const wrapper = mount(DataTable, {
      props: {
        orders: true,
        columns: [
          { key: "trade_no", label: "系统订单号" },
          { key: "amount", label: "订单金额（元）" },
          { key: "type", label: "支付方式" },
        ],
        items: [
          {
            trade_no: "long-id",
            out_trade_no: "merchant-id",
            api_trade_no: "receipt-id",
            amount: "123",
            merchant_amount: "100",
            platform_fee: "23",
            refunded_amount: "1",
            type: "wxpay",
            channel_id: "9007199254740993123",
            status: "pending",
          },
        ],
      },
    });
    expect(wrapper.text()).toContain("接口订单号：receipt-id");
    expect(wrapper.text()).toContain("商户入账：1.00");
    expect(wrapper.text()).toContain("已退款：0.01");
    expect(wrapper.text()).toContain("通道 ID：9007199254740993123");
    expect(
      wrapper
        .findAll("button")
        .find((b) => b.text() === "申请退款")
        ?.attributes("disabled"),
    ).toBeDefined();
    wrapper.unmount();
  });

  it("queries Beijing calendar shortcuts as exact server dates", async () => {
    vi.useFakeTimers({ toFake: ["Date"] });
    vi.setSystemTime(new Date("2026-10-23T16:30:00Z"));
    try {
      const client = new ApiClient("merchant");
      const request = vi
        .spyOn(client, "request")
        .mockResolvedValue({ columns: [], items: [], total: 0 });
      const wrapper = mount(ReportPage, {
        props: { client, report: "daily" },
        global: { stubs: { CsvExportButton: true } },
      });
      await flushPromises();
      const shortcut = wrapper.find('select[aria-label="统计日期快捷范围"]');
      expect(shortcut.exists()).toBe(true);
      await shortcut.setValue("today");
      await flushPromises();
      const query = new URLSearchParams(
        String(request.mock.calls.at(-1)?.[0]).split("?")[1],
      );
      expect(query.get("from")).toBe("2026-10-24");
      expect(query.get("to")).toBe("2026-10-24");
      wrapper.unmount();
    } finally {
      vi.useRealTimers();
    }
  });
});
