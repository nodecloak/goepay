import { it, expect, vi } from "vitest";
import { mount, flushPromises } from "@vue/test-utils";
import { createRouter, createMemoryHistory } from "vue-router";
import TransferPage from "../src/components/TransferPage.vue";
import { ApiClient } from "../src/api";
import {
  readTransferStatistics,
  transferStatisticsMoney,
} from "../src/transfer-statistics";

it("transfer statistics read the full applied scope and preserve integer cents", async () => {
  const router = createRouter({
    history: createMemoryHistory(),
    routes: [{ path: "/:pathMatch(.*)*", component: { template: "<div />" } }],
  });
  await router.push("/admin/transfer/list");
  const client = new ApiClient("admin");
  client.csrfToken = "local-statistics";
  const request = vi
    .spyOn(client, "request")
    .mockImplementation(async (path) =>
      path.startsWith("/transfers/statistics")
        ? {
            count: "9007199254740993",
            amount: "9007199254740993",
            fee_amount: "7",
            cost_amount: "9007199254741000",
            succeeded_count: "1",
            failed_count: "0",
            pending_count: "0",
          }
        : { items: [], total: 0 },
    );
  const view = mount(TransferPage, {
    props: { client, area: "admin", initialQuery: { merchant_id: "751" } },
    global: { plugins: [router] },
  });
  await flushPromises();
  await view.get('input[aria-label="商户号"]').setValue("752");
  const button = view.findAll("button").find((b) => b.text() === "统计");
  expect(button).toBeDefined();
  await button!.trigger("click");
  await flushPromises();
  const path = String(request.mock.calls.at(-1)?.[0]);
  expect(path).toContain("/transfers/statistics?");
  expect(new URL(path, "http://local").searchParams.get("merchant_id")).toBe(
    "751",
  );
  expect(new URL(path, "http://local").searchParams.has("page")).toBe(false);
  const stats = view.get('[aria-label="付款统计"]');
  expect(stats.text()).toContain("90071992547409.93");
  expect(stats.text()).toContain("9007199254740993");
  view.unmount();
});

it("transfer statistics refuse malformed totals and retain aggregate precision above int64", () => {
  const v = {
    count: "3",
    amount: "92233720368547758070",
    fee_amount: "10",
    cost_amount: "92233720368547758080",
    succeeded_count: "1",
    failed_count: "1",
    pending_count: "1",
  };
  expect(transferStatisticsMoney(readTransferStatistics(v).amount)).toBe(
    "922337203685477580.70",
  );
  for (const bad of [
    { ...v, count: 3 },
    { ...v, cost_amount: "0" },
    { ...v, pending_count: "2" },
    { ...v, amount: "01" },
    { ...v, amount: "1".repeat(41) },
  ])
    expect(() => readTransferStatistics(bad)).toThrow();
});

it("a statistics response from an old CSRF session cannot remain visible or loading", async () => {
  const router = createRouter({
    history: createMemoryHistory(),
    routes: [{ path: "/:pathMatch(.*)*", component: { template: "<div />" } }],
  });
  await router.push("/admin/transfer/list");
  const client = new ApiClient("admin");
  client.csrfToken = "first";
  let release: (v: unknown) => void = () => {};
  vi.spyOn(client, "request").mockImplementation(async (path) =>
    path.startsWith("/transfers/statistics")
      ? new Promise((resolve) => {
          release = resolve;
        })
      : { items: [], total: 0 },
  );
  const view = mount(TransferPage, {
    props: { client, area: "admin" },
    global: { plugins: [router] },
  });
  await flushPromises();
  await view
    .findAll("button")
    .find((b) => b.text() === "统计")!
    .trigger("click");
  await flushPromises();
  client.csrfToken = "second";
  release({
    count: "1",
    amount: "100",
    fee_amount: "0",
    cost_amount: "100",
    succeeded_count: "1",
    failed_count: "0",
    pending_count: "0",
  });
  await flushPromises();
  expect(view.find('[aria-label="付款统计"]').exists()).toBe(false);
  expect(
    view
      .findAll("button")
      .find((b) => b.text() === "统计")!
      .attributes("disabled"),
  ).toBeUndefined();
  view.unmount();
});
