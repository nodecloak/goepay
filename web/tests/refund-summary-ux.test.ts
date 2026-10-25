// @vitest-environment jsdom
import { it, expect, vi } from "vitest";
import { mount, flushPromises } from "@vue/test-utils";
import { ApiClient } from "../src/api";
import RefundSummaryDialog from "../src/components/RefundSummaryDialog.vue";
const summary = {
  totalCount: "9007199254740993",
  totalMoney: "90071992547409.93",
  successCount: "1",
  successMoney: "0.01",
  pendingCount: "2",
  pendingMoney: "0.80",
};
it("refund statistics retain exact strings and clear a previous result after a refused or malformed refresh", async () => {
  const client = new ApiClient("admin"),
    calls = vi.spyOn(client, "request").mockResolvedValue(summary as never),
    w = mount(RefundSummaryDialog, {
      props: { client, query: "type=alipay&q=original" },
    });
  await flushPromises();
  expect(w.text()).toContain("9007199254740993");
  expect(w.text()).toContain("¥90071992547409.93");
  expect(calls.mock.calls[0]?.[0]).toBe(
    "/refunds/summary?type=alipay&q=original",
  );
  calls.mockResolvedValueOnce({ ...summary, totalMoney: "1e3" } as never);
  await w.setProps({ query: "status=unknown" });
  await flushPromises();
  expect(w.text()).not.toContain("9007199254740993");
  expect(w.get('[role="alert"]').text()).toContain("格式无效");
  expect(calls.mock.calls.every(([, o]) => !o?.method)).toBe(true);
  w.unmount();
});
it("a late previous-account statistics response cannot replace the current filtered totals", async () => {
  const old = new ApiClient("admin"),
    current = new ApiClient("merchant");
  let finish!: (value: unknown) => void;
  const calls = vi.spyOn(old, "request").mockImplementation(
    () =>
      new Promise((resolve) => {
        finish = resolve;
      }) as never,
  );
  vi.spyOn(current, "request").mockResolvedValue({
    ...summary,
    totalCount: "3",
    totalMoney: "0.81",
  } as never);
  const w = mount(RefundSummaryDialog, { props: { client: old, query: "" } });
  await flushPromises();
  await w.setProps({ client: current, query: "q=mine" });
  await flushPromises();
  finish(summary);
  await flushPromises();
  expect(calls.mock.calls[0]?.[1]?.signal?.aborted).toBe(true);
  expect(w.text()).toContain("¥0.81");
  expect(w.text()).not.toContain("9007199254740993");
  w.unmount();
});
