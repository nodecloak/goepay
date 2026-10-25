import { afterEach, expect, it, vi } from "vitest";
import { mount, flushPromises } from "@vue/test-utils";
import { ApiClient } from "../src/api";
import PrintingSettingsPage from "../src/components/PrintingSettingsPage.vue";
import OrderPrintingPanel from "../src/components/OrderPrintingPanel.vue";
const receipt = {
  trade_no: "123",
  merchant_name: "Local",
  name: "<cut/>商品",
  amount: "9007199254740993",
  type: "alipay",
  paid_at: "2026-10-23T00:00:00Z",
  remark: "safe",
};
const job = {
  id: "1",
  trade_no: "123",
  external_no: "",
  request_no: "local-request",
  state: "queued",
  provider: "local_fake",
  copies: 1,
  device_masked: "***1234",
  receipt,
};
afterEach(() => vi.restoreAllMocks());

it("distinguishes an installed external connector from an enabled worker", async () => {
  const c = new ApiClient("admin");
  const read = vi
    .spyOn(c, "request")
    .mockResolvedValue({
      revision: "1",
      enabled: false,
      provider: "excelsecu",
      app_id_masked: "***test",
      secret_present: true,
      external_execution_available: true,
    });
  const w = mount(PrintingSettingsPage, {
    props: { client: c, ownSession: true, adminClass: "super" },
  });
  await flushPromises();
  expect(w.text()).toContain("固定网关连接器可用");
  expect(w.text()).toContain("外部执行默认关闭");
  expect(read).toHaveBeenCalledTimes(1);
  expect(w.text()).not.toContain("原打印网关配置（不执行）");
  w.unmount();
});

it("never reads printing credentials for delegated or operator sessions", async () => {
  const c = new ApiClient("admin"),
    read = vi.spyOn(c, "request");
  for (const props of [
    { ownSession: false, adminClass: "super" },
    { ownSession: true, adminClass: "operator" },
  ]) {
    const w = mount(PrintingSettingsPage, { props: { client: c, ...props } });
    await flushPromises();
    expect(read).not.toHaveBeenCalled();
    expect(w.find("form").exists()).toBe(false);
    w.unmount();
  }
});

it("clears private fields on rejected CAS save and blocks a blind second save", async () => {
  const c = new ApiClient("admin"),
    read = vi.spyOn(c, "request").mockImplementation(async (_path, options) => {
      if (options?.method === "PUT") throw new Error("configuration changed");
      return {
        revision: "3",
        enabled: false,
        provider: "local_fake",
        app_id_masked: "***test",
        secret_present: true,
        external_execution_available: false,
      } as never;
    });
  const w = mount(PrintingSettingsPage, {
    props: { client: c, ownSession: true, adminClass: "super" },
  });
  await flushPromises();
  await w.findAll("select")[1]!.setValue("replace");
  await w.find('input[type="password"]').setValue("synthetic-ui-secret");
  await w.find("form").trigger("submit");
  await w
    .findAll("button")
    .find((b) => b.text() === "确认保存")!
    .trigger("click");
  await flushPromises();
  expect(w.find('input[type="password"]').exists()).toBe(false);
  expect(w.find("form").exists()).toBe(false);
  expect(w.text()).not.toContain("synthetic-ui-secret");
  expect(read.mock.calls.filter(([, o]) => o?.method === "PUT")).toHaveLength(
    1,
  );
  w.unmount();
});

it("unknown original print blocks new sends and receipt markup remains text", async () => {
  const c = new ApiClient("merchant"),
    read = vi.spyOn(c, "request").mockImplementation(async (path) =>
      path.endsWith("/receipt")
        ? (receipt as never)
        : ({
            items: [{ ...job, state: "unknown" }],
            next_cursor: "",
          } as never),
    ),
    send = vi.spyOn(c, "requestResult");
  const w = mount(OrderPrintingPanel, {
    props: { client: c, ownSession: true, tradeNo: "123" },
  });
  await w.find("button").trigger("click");
  await flushPromises();
  expect(read.mock.calls[1]![0]).toBe(
    "/orders/123/print-jobs?after_id=0&limit=20",
  );
  expect(w.text()).toContain("90071992547409.93");
  expect(w.find("cut").exists()).toBe(false);
  const create = w
    .findAll("button")
    .find((b) => b.text() === "创建手工打印请求")!;
  expect(create.attributes("disabled")).toBeDefined();
  expect(send).not.toHaveBeenCalled();
  w.unmount();
});

it("a lost create response recovers the identical original number and never reports paper completion", async () => {
  vi.spyOn(crypto, "randomUUID").mockReturnValue(
    "11111111-1111-4111-8111-111111111111",
  );
  const c = new ApiClient("merchant");
  vi.spyOn(c, "request").mockImplementation(async (path) =>
    path.endsWith("/receipt")
      ? (receipt as never)
      : ({ items: [], next_cursor: "" } as never),
  );
  const send = vi
    .spyOn(c, "requestResult")
    .mockRejectedValueOnce(new Error("lost response"))
    .mockImplementation(
      async (_p, o) =>
        ({
          status: 202,
          data: {
            ...job,
            external_no: (o!.body as { external_no: string }).external_no,
          },
        }) as never,
    );
  const w = mount(OrderPrintingPanel, {
    props: { client: c, ownSession: true, tradeNo: "123" },
  });
  await w.find("button").trigger("click");
  await flushPromises();
  await w
    .findAll("button")
    .find((b) => b.text() === "创建手工打印请求")!
    .trigger("click");
  await w
    .findAll("button")
    .find((b) => b.text() === "确认创建打印请求")!
    .trigger("click");
  await flushPromises();
  await w
    .findAll("button")
    .find((b) => b.text() === "按原请求号核对")!
    .trigger("click");
  await flushPromises();
  expect(send.mock.calls).toHaveLength(2);
  expect(send.mock.calls[0]![1]!.body).toEqual(send.mock.calls[1]![1]!.body);
  expect(w.text()).toContain("尚未发送");
  expect(w.text()).not.toContain("打印成功");
  w.unmount();
});

it("unmounting receipt lookup cannot start a follow-up history request", async () => {
  let finish!: (v: never) => void;
  const c = new ApiClient("merchant"),
    read = vi.spyOn(c, "request").mockImplementationOnce(
      () =>
        new Promise((resolve) => {
          finish = resolve;
        }),
    );
  const w = mount(OrderPrintingPanel, {
    props: { client: c, ownSession: true, tradeNo: "123" },
  });
  await w.find("button").trigger("click");
  w.unmount();
  finish(receipt as never);
  await flushPromises();
  expect(read).toHaveBeenCalledTimes(1);
});

it("a lost task recovery response keeps the same expected task and original print intent", async () => {
  const c = new ApiClient("merchant");
  vi.spyOn(c, "request").mockImplementation(async (path) =>
    path.endsWith("/receipt")
      ? (receipt as never)
      : ({
          items: [
            {
              ...job,
              state: "blocked",
              task_id: "44",
              task_state: "dead",
              can_recover: true,
            },
          ],
          next_cursor: "",
        } as never),
  );
  const send = vi
    .spyOn(c, "requestResult")
    .mockRejectedValueOnce(new Error("lost recovery response"))
    .mockResolvedValueOnce({
      status: 200,
      data: {
        ...job,
        task_id: "45",
        task_state: "pending",
        can_recover: false,
      },
    } as never);
  const w = mount(OrderPrintingPanel, {
    props: { client: c, ownSession: true, tradeNo: "123" },
  });
  await w.find("button").trigger("click");
  await flushPromises();
  await w
    .findAll("button")
    .find((b) => b.text() === "核对并恢复原未发送任务")!
    .trigger("click");
  expect(send).not.toHaveBeenCalled();
  await w
    .findAll("button")
    .find((b) => b.text() === "确认恢复原未发送任务")!
    .trigger("click");
  await flushPromises();
  await w
    .findAll("button")
    .find((b) => b.text() === "按原任务号核对恢复")!
    .trigger("click");
  await flushPromises();
  expect(send).toHaveBeenCalledTimes(2);
  expect(send.mock.calls.map(([p, o]) => ({ path: p, body: o?.body }))).toEqual(
    [
      { path: "/print-jobs/1/recover", body: { expected_task_id: "44" } },
      { path: "/print-jobs/1/recover", body: { expected_task_id: "44" } },
    ],
  );
  expect(w.text()).toContain("尚未发送");
  w.unmount();
});
