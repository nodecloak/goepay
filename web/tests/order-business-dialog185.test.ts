import { mount, flushPromises } from "@vue/test-utils";
import { it, expect, vi } from "vitest";
import Dialog from "../src/components/OrderBusinessBatchDialog.vue";
import { ApiClient } from "../src/api";
import actual from "../e2e/orderbatch185-http-fixture.json";
it("shows original selected trades and requires acknowledgement before preparing a business batch", async () => {
  const c = new ApiClient("admin");
  c.csrfToken = "synthetic";
  const request = vi.spyOn(c, "request").mockRejectedValue(Error("unexpected"));
  const w = mount(Dialog, {
    props: {
      client: c,
      action: "query",
      rows: [{ trade_no: "T185", status: "pending" }],
      ownSession: true,
      adminClass: "super",
    },
    global: { stubs: { Teleport: true } },
  });
  await flushPromises();
  expect(w.get('[role="dialog"]').attributes("aria-label")).toBe(
    "批量订单业务确认",
  );
  expect(w.text()).toContain("手动补单");
  expect(w.text()).toContain("T185");
  const buttons = w.findAll("button");
  const confirm = buttons.find((b) => b.text() === "确认执行")!;
  expect(confirm.attributes("disabled")).toBeDefined();
  await w.get("textarea").setValue("local verification");
  await w.get('input[type="checkbox"]').setValue(true);
  expect(w.get("button.primary").attributes("disabled")).toBeUndefined();
  expect(request).not.toHaveBeenCalled();
  w.unmount();
});
it("forgets revoked original context and ignores its late preparation result", async () => {
  const c = new ApiClient("admin");
  c.csrfToken = "context185";
  let resolve!: (v: unknown) => void;
  const calls = vi
    .spyOn(c, "request")
    .mockImplementation(() => new Promise((r) => (resolve = r)) as never);
  const props = {
    client: c,
    action: "query" as const,
    rows: [{ trade_no: actual.prepared.items[0].trade_no, status: "pending" }],
    ownSession: true,
    adminClass: "super",
  };
  const w = mount(Dialog, { props, global: { stubs: { Teleport: true } } });
  await w.get("textarea").setValue("local reason");
  await w.get("input").setValue(true);
  await w.get("button.primary").trigger("click");
  await flushPromises();
  expect(calls).toHaveBeenCalledTimes(1);
  await w.setProps({ ownSession: false });
  resolve(actual.prepared);
  await flushPromises();
  expect(calls).toHaveBeenCalledTimes(1);
  w.unmount();
  const again = mount(Dialog, { props, global: { stubs: { Teleport: true } } });
  await flushPromises();
  expect(again.text()).not.toContain("请先核对上次原批次结果");
  again.unmount();
});
