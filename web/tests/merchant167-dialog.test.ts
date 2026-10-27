import { it, expect, vi } from "vitest";
import { mount, flushPromises } from "@vue/test-utils";
import { ApiClient } from "../src/api";
import Dialog from "../src/components/MerchantOrderActionDialog.vue";
const trade = "202610260000000000167";
function setup(
  action: "query" | "refund" | "notify" | "print",
  data: Record<string, unknown>,
) {
  const client = new ApiClient("merchant");
  client.csrfToken = "synthetic167";
  vi.spyOn(client, "request").mockResolvedValue(data);
  const send = vi.spyOn(client, "requestResult").mockResolvedValue({
    status: 200,
    data: { trade_no: trade, status: "paid" },
  } as any);
  const view = mount(Dialog, {
    props: { client, tradeNo: trade, action, ownSession: true },
  });
  return { client, send, view };
}
it("merchant167 requires explicit confirmation and allows only one query POST of the original order", async () => {
  const { view, send } = setup("query", {
    order: { trade_no: trade, status: "pending" },
    merchant_actions: { query: true },
  });
  await flushPromises();
  expect(send).not.toHaveBeenCalled();
  await view.get("button.primary").trigger("click");
  await flushPromises();
  expect(send).toHaveBeenCalledTimes(1);
  expect(send.mock.calls[0]![0]).toBe(`/orders/${trade}/query`);
  expect(view.text()).toContain("原通道已核实支付成功");
  expect(view.find("button.primary").exists()).toBe(false);
  view.unmount();
});
it("merchant167 fresh server denial and mismatched original order cannot reach a refund or mutation", async () => {
  for (const data of [
    {
      order: { trade_no: trade, status: "paid" },
      merchant_actions: { refund: false },
    },
    {
      order: { trade_no: "other-order", status: "paid" },
      merchant_actions: { refund: true },
    },
  ]) {
    const { view, send } = setup("refund", data);
    await flushPromises();
    expect(view.find('[role="alert"]').exists()).toBe(true);
    expect(view.emitted("refund")).toBeUndefined();
    expect(send).not.toHaveBeenCalled();
    view.unmount();
  }
});
it("merchant167 an uncertain query cannot offer a second submission or create another payment", async () => {
  const { view, send } = setup("query", {
    order: { trade_no: trade, status: "pending" },
    merchant_actions: { query: true },
  });
  send.mockRejectedValueOnce(new Error("synthetic lost response"));
  await flushPromises();
  await view.get("button.primary").trigger("click");
  await flushPromises();
  expect(send).toHaveBeenCalledTimes(1);
  expect(view.text()).toContain("原订单查询结果尚未核实");
  expect(view.find("button.primary").exists()).toBe(false);
  expect(send.mock.calls[0]![0]).toBe(`/orders/${trade}/query`);
  view.unmount();
});
it("merchant167 stale detail responses cannot open a refund after cancel or a changed session", async () => {
  let resolve!: (value: unknown) => void;
  const client = new ApiClient("merchant");
  client.csrfToken = "original167";
  vi.spyOn(client, "request").mockImplementation(
    () => new Promise((r) => (resolve = r)) as any,
  );
  const view = mount(Dialog, {
    props: { client, tradeNo: trade, action: "refund", ownSession: true },
  });
  view.unmount();
  resolve({
    order: { trade_no: trade, status: "paid" },
    merchant_actions: { refund: true },
  });
  await flushPromises();
  expect(view.emitted("refund")).toBeUndefined();
  expect(view.emitted("updated")).toBeUndefined();
});
it("merchant167 printing opens the existing protected panel without sending a print job", async () => {
  const { view, send } = setup("print", {
    order: { trade_no: trade, status: "paid" },
    merchant_actions: { print: true },
  });
  await flushPromises();
  expect(view.text()).toContain("订单打印收据");
  expect(view.text()).toContain("查看或刷新打印收据");
  expect(send).not.toHaveBeenCalled();
  view.unmount();
});
it("merchant167 keeps the print dialog and original request while a print response is pending or uncertain", async () => {
  const client = new ApiClient("merchant");
  client.csrfToken = "synthetic167";
  vi.spyOn(client, "request").mockImplementation(async (path) => {
    if (path.endsWith("/receipt"))
      return {
        trade_no: trade,
        merchant_name: "Synthetic",
        name: "Synthetic",
        amount: "10000",
        type: "wxpay",
        paid_at: "2026-10-26T01:00:00Z",
        remark: "",
      } as never;
    if (path.includes("/print-jobs"))
      return { items: [], next_cursor: "" } as never;
    return {
      order: { trade_no: trade, status: "paid" },
      merchant_actions: { print: true },
    } as never;
  });
  let reject!: (e: Error) => void;
  vi.spyOn(client, "requestResult").mockImplementation(
    () =>
      new Promise((_resolve, r) => {
        reject = r;
      }) as never,
  );
  const view = mount(Dialog, {
    props: { client, tradeNo: trade, action: "print", ownSession: true },
    attachTo: document.body,
  });
  const button = (text: string) =>
    view.findAll("button").find((b) => b.text() === text)!;
  await flushPromises();
  await button("查看或刷新打印收据").trigger("click");
  await flushPromises();
  await button("创建手工打印请求").trigger("click");
  await button("确认创建打印请求").trigger("click");
  expect(button("关闭").attributes("disabled")).toBeDefined();
  document.dispatchEvent(
    new KeyboardEvent("keydown", { key: "Escape", bubbles: true }),
  );
  expect(view.emitted("close")).toBeUndefined();
  reject(new Error("synthetic lost print response"));
  await flushPromises();
  expect(button("按原请求号核对").exists()).toBe(true);
  expect(button("关闭").attributes("disabled")).toBeDefined();
  document.dispatchEvent(
    new KeyboardEvent("keydown", { key: "Escape", bubbles: true }),
  );
  expect(view.emitted("close")).toBeUndefined();
  view.unmount();
});
