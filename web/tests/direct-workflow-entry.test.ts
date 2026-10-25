// @vitest-environment jsdom
import { it, expect, vi } from "vitest";
import { mount, flushPromises } from "@vue/test-utils";
import WorkflowPage from "../src/components/WorkflowPage.vue";
import TransferPage from "../src/components/TransferPage.vue";
import { ApiClient } from "../src/api";

it("opens the withdrawal application as an inline form with read-only initialization", async () => {
  const client = new ApiClient("merchant");
  const calls: string[] = [];
  vi.spyOn(client, "request").mockImplementation(async (path, options) => {
    calls.push(path);
    expect(options?.method || "GET").toBe("GET");
    return (
      path.includes("funds-availability")
        ? {
            settle_type: 0,
            available_money: "1.00",
            ledger_available_money: "1.00",
            today_blocked_money: "0.00",
          }
        : { state: "not_configured" }
    ) as never;
  });
  const w = mount(WorkflowPage, {
    props: {
      client,
      area: "merchant",
      workflow: "settlements",
      application: true,
    },
  });
  await flushPromises();
  expect(w.find('input[inputmode="decimal"]').exists()).toBe(true);
  expect(w.find('[role="dialog"]').exists()).toBe(false);
  expect(w.find("table").exists()).toBe(false);
  expect(w.get('a[href="/merchant/settle/list"]').text()).toBe("提现记录");
  expect(calls).toContain("/workflows/funds-availability");
  expect(calls.some((p) => /^\/workflows\/settlements\?/.test(p))).toBe(false);
  expect(
    w
      .findAll("button")
      .find((b) => b.text() === "确认提交")
      ?.attributes("disabled"),
  ).toBeDefined();
  w.unmount();
});

it("keeps the ordinary settlement record page closed until explicitly requested", async () => {
  const client = new ApiClient("merchant");
  vi.spyOn(client, "request").mockResolvedValue({ items: [], total: 0 });
  const w = mount(WorkflowPage, {
    props: { client, area: "merchant", workflow: "settlements" },
  });
  await flushPromises();
  expect(w.find('input[inputmode="decimal"]').exists()).toBe(false);
  expect(w.find("table").exists()).toBe(true);
  w.unmount();
});

it.each(["admin", "merchant"] as const)(
  "links %s payment records to the existing durable batch workflow",
  async (area) => {
    const client = new ApiClient(area);
    vi.spyOn(client, "request").mockResolvedValue({ items: [], total: 0 });
    const w = mount(TransferPage, { props: { client, area } });
    await flushPromises();
    expect(w.get(`a[href="/${area}/transfer/batches"]`).text()).toBe(
      "批量转账",
    );
    w.unmount();
  },
);
