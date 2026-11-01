import { expect, it, vi } from "vitest";
import { flushPromises, mount } from "@vue/test-utils";
import CommercePage from "../src/components/CommercePage.vue";
import { ApiClient } from "../src/api";

for (const area of ["admin", "merchant"] as const)
  it(`retains deposit columns before any application exists for ${area}`, async () => {
    const client = new ApiClient(area);
    const request = vi
      .spyOn(client, "request")
      .mockResolvedValue({ items: [], total: 0 });
    const view = mount(CommercePage, {
      props: { client, kind: "deposits", area },
    });
    try {
      await flushPromises();
      expect(view.findAll("thead th").map((h) => h.text())).toEqual([
        "编号",
        ...(area === "admin" ? ["商户 ID"] : []),
        "操作类型",
        "金额",
        "业务请求号",
        "状态",
        "创建时间",
        ...(area === "admin" ? ["操作"] : []),
      ]);
      expect(
        request.mock.calls.every(
          ([, opts]) => !opts?.method || opts.method === "GET",
        ),
      ).toBe(true);
    } finally {
      view.unmount();
    }
  });

it("shows the deposit API's exact yuan amount without treating it as minor units", async () => {
  const client = new ApiClient("admin");
  vi.spyOn(client, "request").mockResolvedValue({
    items: [
      {
        id: "9007199254740993",
        merchant_id: "101",
        action: "release",
        amount: "90071992547409.93",
        amount_cents: "9007199254740993",
        external_no: "synthetic-deposit",
        status: "pending",
        created_at: "2026-11-01T01:02:03Z",
      },
    ],
    total: 1,
  });
  const view = mount(CommercePage, {
    props: { client, kind: "deposits", area: "admin" },
  });
  try {
    await flushPromises();
    expect(view.find("tbody").text()).toContain("90,071,992,547,409.93");
    expect(view.find("tbody").text()).toContain("申请释放保证金");
  } finally {
    view.unmount();
  }
});

it("retains recharge headers when empty and renders its exact yuan after refresh", async () => {
  const client = new ApiClient("merchant");
  const request = vi
    .spyOn(client, "request")
    .mockResolvedValue({ items: [], total: 0 });
  const view = mount(CommercePage, {
    props: { client, kind: "recharges", area: "merchant" },
  });
  try {
    await flushPromises();
    expect(view.findAll("thead th").map((h) => h.text())).toEqual([
      "编号",
      "充值订单号",
      "金额",
      "状态",
      "创建时间",
    ]);
    request.mockResolvedValue({
      items: [
        {
          id: "7",
          trade_no: "synthetic-recharge",
          amount: "1234.56",
          amount_cents: "123456",
          status: "paid",
          created_at: "2026-11-01T01:02:03Z",
        },
      ],
      total: 1,
    });
    await view
      .findAll("button")
      .find((b) => b.text() === "刷新")!
      .trigger("click");
    await flushPromises();
    expect(view.find("tbody").text()).toContain("1,234.56");
    expect(view.find("tbody").text()).toContain("已支付");
  } finally {
    view.unmount();
  }
});
