import { expect, it, vi } from "vitest";
import { flushPromises, mount } from "@vue/test-utils";
import ChannelSharingPage from "../src/components/ChannelSharingPage.vue";
import { ApiClient } from "../src/api";

const caps = {
  merchant_id: "1000",
  available: true,
  reason: "",
  channels: [
    {
      channel_id: "7",
      provider: "wxpay",
      product: "wxpay_v3_direct",
      methods: ["native"],
      receiver_types: ["MERCHANT_ID", "PERSONAL_OPENID"],
      return_supported: true,
      finish_supported: true,
    },
  ],
};
const receiver = {
  id: "9",
  merchant_id: "1000",
  channel_id: "7",
  provider: "wxpay",
  product: "wxpay_v3_direct",
  type: "MERCHANT_ID",
  account_masked: "***1234",
  name_masked: "测***",
  state: "draft",
};
const render = (
  request: (...args: any[]) => any,
  page: "receiver" | "order" = "receiver",
  area: "admin" | "merchant" = "admin",
) =>
  mount(ChannelSharingPage, {
    props: {
      client: Object.assign(new ApiClient(area), { request }),
      area,
      page,
    },
  });

for (const area of ["admin", "merchant"] as const) {
  it(`${area} V2 single sharing matches original receiver limits and preserves long legal names`, async () => {
    const originalName = "名".repeat(100);
    const request = vi.fn(async (path: string, options?: any) => {
      if (path.includes("channels"))
        return {
          ...caps,
          channels: [
            {
              ...caps.channels[0],
              product: "wxpay_v2_single_direct",
              methods: ["native", "scan"],
            },
          ],
        };
      if (options?.method === "POST")
        return { ...receiver, product: "wxpay_v2_single_direct" };
      return { items: [], next_cursor: "0" };
    });
    const w = render(request, "receiver", area);
    if (area === "admin") await open(w);
    else await flushPromises();
    await w.get('[aria-label="分账通道"]').setValue("7");
    await flushPromises();
    expect(w.text()).toContain("单次分账");
    expect(w.get('[aria-label="接收账号"]').attributes("maxlength")).toBe("64");
    expect(w.get('[aria-label="接收方姓名"]').attributes("maxlength")).toBe(
      "1024",
    );
    await w.get('[aria-label="接收账号"]').setValue("local_receiver79");
    await w.get('[aria-label="接收方姓名"]').setValue(originalName);
    await w.get("form.sharing-form").trigger("submit");
    await flushPromises();
    const writes = () =>
      request.mock.calls.filter(([, options]) => options?.method === "POST");
    expect(writes()).toHaveLength(1);
    expect(writes()[0]?.[1]?.body.name).toBe(originalName);
    expect(writes()[0]?.[1]?.body.relation_type).toBe("SERVICE_PROVIDER");
    expect(w.text()).toContain("付款码支付");
    expect(
      (w.get('[aria-label="接收方姓名"]').element as HTMLInputElement).value,
    ).toBe("");
    await w.get('[aria-label="接收账号"]').setValue("local_receiver79");
    await w.get('[aria-label="接收方姓名"]').setValue("名".repeat(342));
    await w.get("form.sharing-form").trigger("submit");
    await flushPromises();
    expect(writes()).toHaveLength(1);
    expect(w.text()).toContain("接收方姓名过长");
    w.unmount();
  });
}
async function open(w: ReturnType<typeof render>) {
  await w.get('[aria-label="商户 ID"]').setValue("1000");
  await w.get("form.sharing-toolbar").trigger("submit");
  await flushPromises();
}

it("shows honest simulation status and never submits unavailable sharing", async () => {
  const request = vi.fn(async (path: string, _options?: any) =>
    path.includes("channels")
      ? {
          ...caps,
          available: false,
          reason: "模拟通道不支持分账",
          channels: [],
        }
      : { items: [], next_cursor: "0" },
  );
  const w = render(request);
  await open(w);
  expect(w.text()).toContain("模拟通道不支持分账");
  expect(
    w.get('[data-action="create-receiver"]').attributes("disabled"),
  ).toBeDefined();
  expect(request.mock.calls.every((c) => !c[1]?.method)).toBe(true);
  w.unmount();
});
it("clears plaintext inputs immediately and retains only the masked creation response", async () => {
  let resolve: ((v: unknown) => void) | undefined;
  const request = vi.fn(async (path: string, options?: any) => {
    if (path.includes("channels")) return caps;
    if (options?.method === "POST") return new Promise((r) => (resolve = r));
    return { items: [], next_cursor: "0" };
  });
  const w = render(request);
  await open(w);
  await w.get('[aria-label="分账通道"]').setValue("7");
  await w
    .get('[aria-label="接收账号"]')
    .setValue("synthetic-sensitive-account");
  await w.get('[aria-label="接收方姓名"]').setValue("synthetic-private-name");
  await w.get("form.sharing-form").trigger("submit");
  await flushPromises();
  expect(
    (w.get('[aria-label="接收账号"]').element as HTMLInputElement).value,
  ).toBe("");
  resolve?.(receiver);
  await flushPromises();
  expect(w.text()).toContain("***1234");
  expect(w.html()).not.toContain("synthetic-sensitive-account");
  w.unmount();
});
it("ignores late responses from the previous merchant scope", async () => {
  let resolve: ((v: unknown) => void) | undefined;
  const request = vi.fn(async (path: string) =>
    path.includes("channels")
      ? new Promise((r) => (resolve = r))
      : { items: [], next_cursor: "0" },
  );
  const w = render(request);
  await w.get('[aria-label="商户 ID"]').setValue("1000");
  await w.get("form.sharing-toolbar").trigger("submit");
  await flushPromises();
  await w.get('[aria-label="商户 ID"]').setValue("1001");
  resolve?.(caps);
  await flushPromises();
  expect(w.find('option[value="7"]').exists()).toBe(false);
  expect(w.text()).not.toContain("***1234");
  w.unmount();
});
it("queries an unknown original order without generating another submit", async () => {
  const result = {
    id: "10",
    merchant_id: "1000",
    order_id: "11",
    external_no: "original-request",
    out_no: "PS" + "a".repeat(30),
    state: "unknown",
    completed: false,
    funds_released: false,
    released_amount: "0",
    items: [],
  };
  const request = vi.fn(async (path: string, options?: any) => {
    if (path.includes("channels")) return caps;
    if (path.endsWith("/query"))
      return {
        ...result,
        state: "succeeded",
        completed: true,
        funds_released: true,
        released_amount: "80",
      };
    return { items: [result], next_cursor: "0" };
  });
  const w = render(request, "order");
  await open(w);
  await w.get('[data-action="query-order"]').trigger("click");
  await flushPromises();
  expect(request.mock.calls.filter((c) => c[1]?.method === "POST")).toEqual([
    [
      "/channel-sharing/orders/original-request/query",
      expect.objectContaining({ body: { merchant_id: "1000" } }),
    ],
  ]);
  expect(w.text()).toContain("已释放");
  w.unmount();
});

it("recovers a lost submit response by reading the same intent and locks the original number", async () => {
  const row = {
    id: "10",
    merchant_id: "1000",
    order_id: "11",
    external_no: "stable-intent",
    out_no: "PS" + "a".repeat(30),
    state: "unknown",
    completed: false,
    funds_released: false,
    released_amount: "0",
    items: [],
  };
  const request = vi.fn(async (path: string, options?: any) => {
    if (path.includes("channels")) return caps;
    if (path.includes("/payment/"))
      return {
        merchant_id: "1000",
        declared: true,
        gross_amount: "100",
        merchant_net: "100",
        items: [],
      };
    if (options?.method === "POST") throw new Error("local response lost");
    if (path.includes("/orders/stable-intent")) return row;
    return { items: [], next_cursor: "0" };
  });
  const w = render(request, "order");
  await open(w);
  await w.get('[aria-label="平台订单号"]').setValue("LOCAL_TRADE");
  await w.get("form.sharing-form").trigger("submit");
  await flushPromises();
  await w.get('[aria-label="业务请求号"]').setValue("stable-intent");
  await w.get("button.primary").trigger("click");
  await w.get('[role="dialog"] button.primary').trigger("click");
  await flushPromises();
  expect(
    request.mock.calls.filter((c) => c[1]?.method === "POST"),
  ).toHaveLength(1);
  expect(w.text()).toContain("结果未知");
  expect(
    w.get('[aria-label="业务请求号"]').attributes("readonly"),
  ).toBeDefined();
  expect(
    w.get("form.sharing-form button.primary").attributes("disabled"),
  ).toBeDefined();
  w.unmount();
});

it("requests a separate receiver return only from a completed original success and queries its original number", async () => {
  const order = {
    id: "10",
    external_no: "original-ps",
    out_no: "PS" + "a".repeat(30),
    state: "succeeded",
    completed: true,
    funds_released: true,
    released_amount: "80",
    items: [{ receiver_id: "9", amount: "20", state: "succeeded" }],
  };
  const returned = {
    id: "13",
    sharing_id: "10",
    receiver_id: "9",
    external_no: "return-fixed",
    out_no: "PR" + "b".repeat(30),
    amount: "10",
    state: "unknown",
  };
  const request = vi.fn(async (path: string, options?: any) => {
    if (path.includes("channels")) return caps;
    if (path.includes("/receivers/9")) return { ...receiver, state: "bound" };
    if (path.endsWith("/query")) return { ...returned, state: "succeeded" };
    if (options?.method === "POST") return returned;
    if (path.includes("/returns")) return { items: [], next_cursor: "0" };
    return { items: [order], next_cursor: "0" };
  });
  const w = render(request, "order");
  await open(w);
  await w.get('button[data-action="prepare-return"]').trigger("click");
  await flushPromises();
  await w.get('[aria-label="回退金额（元）"]').setValue("0.10");
  await w.get('[aria-label="回退业务请求号"]').setValue("return-fixed");
  await w.get('form[aria-label="申请分账回退"]').trigger("submit");
  await w.get('[role="dialog"] button.primary').trigger("click");
  await flushPromises();
  expect(request).toHaveBeenCalledWith(
    "/channel-sharing/returns",
    expect.objectContaining({
      method: "POST",
      body: {
        merchant_id: "1000",
        sharing_external_no: "original-ps",
        receiver_id: "9",
        external_no: "return-fixed",
        amount: "10",
      },
    }),
  );
  await w.get('button[data-action="query-return"]').trigger("click");
  await flushPromises();
  expect(w.text()).toContain("已回到原商户");
  w.unmount();
});

it("previews original independent finish amounts and never releases funds merely from acceptance", async () => {
  const preview = {
    id: "0",
    order_id: "11",
    merchant_id: "1000",
    trade_no: "LOCAL_PF",
    external_no: "",
    out_no: "",
    state: "eligible",
    source: "finish",
    gross_amount: "100",
    merchant_net: "90",
    shared_amount: "0",
    expected_unsplit: "100",
    release_amount: "90",
    funds_released: false,
  };
  const pending = {
    ...preview,
    id: "16",
    external_no: "stable-finish",
    out_no: "PF" + "c".repeat(30),
    state: "pending",
  };
  const request = vi.fn(async (path: string, options?: any) => {
    if (path.includes("channels")) return caps;
    if (path.includes("finish-preview")) return preview;
    if (path.endsWith("/query"))
      return { ...pending, state: "succeeded", funds_released: true };
    if (options?.method === "POST") return pending;
    return { items: [], next_cursor: "0" };
  });
  const w = render(request, "order");
  await open(w);
  await w.get('[aria-label="完结平台订单号"]').setValue("LOCAL_PF");
  await w.get('form[aria-label="核对渠道独立完结"]').trigger("submit");
  await flushPromises();
  await w.get('[aria-label="完结业务请求号"]').setValue("stable-finish");
  await w.get('[data-action="submit-finish"]').trigger("click");
  await w.get('[role="dialog"] button.primary').trigger("click");
  await flushPromises();
  expect(request).toHaveBeenCalledWith(
    "/channel-sharing/finishes",
    expect.objectContaining({
      method: "POST",
      body: {
        merchant_id: "1000",
        trade_no: "LOCAL_PF",
        external_no: "stable-finish",
      },
    }),
  );
  expect(w.get('[aria-label="独立完结记录"]').text()).toContain("原资金仍保留");
  await w.get('[data-action="query-finish"]').trigger("click");
  await flushPromises();
  expect(w.get('[aria-label="独立完结记录"]').text()).toContain("原余款已释放");
  w.unmount();
});
