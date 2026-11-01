// @vitest-environment jsdom
import { expect, it, vi } from "vitest";
import { flushPromises, mount } from "@vue/test-utils";
import { ApiClient } from "../src/api";
import { flattenMenu, navigationFor, pageAuthorizedFor } from "../src/menu";
import TransferBatchPage from "../src/components/TransferBatchPage.vue";
import TransferBatchDetails from "../src/components/TransferBatchDetails.vue";
import type { TransferBatch } from "../src/transfer-batches";

const batch: TransferBatch = {
  id: "122",
  merchant_id: "1",
  external_no: "synthetic-feature-batch122",
  item_count: 1,
  amount: "100",
  cost_amount: "100",
  fee_amount: "0",
  all_succeeded: false,
  summary: { unsent: 1 },
  items: [],
};
const caps = {
  writes_enabled: true,
  settle_type: 0,
  available_money: "10.00",
  ledger_available_money: "10.00",
  today_blocked_money: "0.00",
  channels: [{ id: "7", types: ["alipay"] }],
};
const button = (wrapper: ReturnType<typeof mount>, label: string) =>
  wrapper.findAll("button").find((b) => b.text() === label)!;
function clientFixture() {
  const client = new ApiClient("merchant");
  vi.spyOn(client, "request").mockImplementation(async (path) => {
    if (path.startsWith("/transfers/capabilities")) return caps as never;
    if (path === "/transfer-batches/122") return batch as never;
    return { items: [batch], total: 1, next_before_id: "0" } as never;
  });
  return client;
}

it("hides the closed batch menu while preserving the original recovery route", () => {
  const features = { user_transfer_batch: false };
  expect(
    flattenMenu(
      navigationFor("merchant", "merchant", undefined, undefined, features),
    ).some((page) => page.id === "MerchantTransferBatches"),
  ).toBe(false);
  for (const flags of [undefined, {}, features])
    expect(
      pageAuthorizedFor(
        "merchant",
        "/transfer/batches",
        "merchant",
        undefined,
        undefined,
        flags,
      ),
    ).toBe(true);
  expect(
    pageAuthorizedFor(
      "merchant",
      "/transfer/batches",
      "admin",
      undefined,
      undefined,
      features,
    ),
  ).toBe(false);
});

it("keeps the old batch list and details accessible without allowing a new draft", async () => {
  const client = clientFixture();
  const w = mount(TransferBatchPage, {
    props: { client, newUseAllowed: false },
  });
  await flushPromises();
  expect(button(w, "新建批量付款").attributes("disabled")).toBeDefined();
  expect(w.text()).toContain("仍可查询原批次");
  await button(w, "查看批次 122").trigger("click");
  await flushPromises();
  expect(w.findComponent(TransferBatchDetails).props("newUseAllowed")).toBe(
    false,
  );
  expect(w.findAll("button").some((b) => b.text() === "执行尚未发送项目")).toBe(
    false,
  );
  expect(button(w, "查询已发送项目").attributes("disabled")).toBeUndefined();
  w.unmount();
});
it("retains original batch recovery with the explicit disabled-group readonly capability DTO", async () => {
  const client = clientFixture();
  vi.mocked(client.request).mockImplementation(async (path) => {
    if (path.startsWith("/transfers/capabilities"))
      return {
        writes_enabled: false,
        platform_enabled: true,
        effective_enabled: false,
        permission_source: "group_disabled",
        fee_policy_available: false,
        funds_policy_available: false,
        ledger_available_money: "123.45",
        reserved_money: "6.78",
        channels: caps.channels,
      } as never;
    if (path === "/transfer-batches/122") return batch as never;
    return { items: [batch], total: 1, next_before_id: "0" } as never;
  });
  const sends = vi.spyOn(client, "requestResult");
  const w = mount(TransferBatchPage, { props: { client } });
  await flushPromises();
  expect(button(w, "新建批量付款").attributes("disabled")).toBeDefined();
  await button(w, "查看批次 122").trigger("click");
  await flushPromises();
  expect(button(w, "查询已发送项目").attributes("disabled")).toBeUndefined();
  expect(sends).not.toHaveBeenCalled();
  w.unmount();
});

it("keeps query/cancel available after closure while refusing execute even with open channel capability", async () => {
  const client = clientFixture();
  const sends = vi
    .spyOn(client, "requestResult")
    .mockResolvedValue({ status: 200, data: batch });
  const w = mount(TransferBatchDetails, {
    props: { client, batchId: "122", newUseAllowed: false },
  });
  await flushPromises();
  expect(w.findAll("button").some((b) => b.text() === "执行尚未发送项目")).toBe(
    false,
  );
  await button(w, "查询已发送项目").trigger("click");
  await flushPromises();
  expect(sends).toHaveBeenCalledTimes(1);
  expect(sends.mock.calls[0]?.[0]).toBe("/transfer-batches/122/query");
  await button(w, "取消尚未发送项目").trigger("click");
  await flushPromises();
  await button(w, "确认取消未发送项目").trigger("click");
  await flushPromises();
  expect(sends.mock.calls.map(([path]) => path)).toEqual([
    "/transfer-batches/122/query",
    "/transfer-batches/122/cancel-unsent",
  ]);
  w.unmount();
});

it("clears an unsubmitted recipient draft when the feature closes, preserving legacy sessions without a flag", async () => {
  const client = clientFixture();
  const w = mount(TransferBatchPage, { props: { client } });
  await flushPromises();
  expect(button(w, "新建批量付款").attributes("disabled")).toBeUndefined();
  await button(w, "新建批量付款").trigger("click");
  await flushPromises();
  expect(w.find('[aria-label="新批次"]').exists()).toBe(true);
  await w.setProps({ newUseAllowed: false });
  await flushPromises();
  expect(w.find('[aria-label="新批次"]').exists()).toBe(false);
  expect(button(w, "新建批量付款").attributes("disabled")).toBeDefined();
  w.unmount();
});

it("cancels an open execute confirmation if the current feature permission is revoked", async () => {
  const client = clientFixture();
  const sends = vi
    .spyOn(client, "requestResult")
    .mockResolvedValue({ status: 200, data: batch });
  const w = mount(TransferBatchDetails, { props: { client, batchId: "122" } });
  await flushPromises();
  await button(w, "执行尚未发送项目").trigger("click");
  await flushPromises();
  expect(button(w, "确认仅执行未发送项目")).toBeDefined();
  await w.setProps({ newUseAllowed: false });
  await flushPromises();
  expect(
    w.findAll("button").some((b) => b.text() === "确认仅执行未发送项目"),
  ).toBe(false);
  expect(sends).not.toHaveBeenCalled();
  expect(button(w, "查询已发送项目").attributes("disabled")).toBeUndefined();
  w.unmount();
});
