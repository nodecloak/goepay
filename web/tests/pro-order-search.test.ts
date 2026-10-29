import { it, expect } from "vitest";
import { mount } from "@vue/test-utils";
import ListFilters from "../src/components/ListFilters.vue";
import { freshListFilters, appendListFilters } from "../src/list-filters";

it("passes order field and notification state exactly and resets both", () => {
  const filters = {
    ...freshListFilters(),
    search_field: "realmoney",
    notification_state: "dead",
  };
  const order = new URLSearchParams({ q: "92233720368547758.07" });
  appendListFilters(order, filters, "orders", "merchant");
  expect(Object.fromEntries(order)).toEqual({
    q: "92233720368547758.07",
    search_field: "realmoney",
    notification_state: "dead",
  });
  const refund = new URLSearchParams();
  appendListFilters(refund, filters, "refunds", "admin");
  expect(refund.has("search_field")).toBe(false);
  expect(refund.has("notification_state")).toBe(false);
  expect((freshListFilters() as Record<string, string>).search_field).toBe("");
  expect(
    (freshListFilters() as Record<string, string>).notification_state,
  ).toBe("");
});

it("offers separate notification states without conflating payment and notification", async () => {
  const model = freshListFilters();
  const wrapper = mount(ListFilters, {
    props: { kind: "orders", area: "admin", modelValue: model },
  });
  // Notification state lives in the 展开 (extra) section of the kit FilterBar.
  await wrapper.get("button.pa-filter-toggle").trigger("click");
  const notification = wrapper.get(
    'button.pa-select-trigger[aria-label="通知状态筛选"]',
  );
  await notification.trigger("click");
  const options = wrapper.findAll('[role="option"]').map((x) => x.text());
  expect(options).toEqual([
    "全部通知状态",
    "尚无通知任务",
    "等待通知",
    "正在通知",
    "通知成功",
    "通知失败",
  ]);
  // Payment status is a separate field, never conflated with notification.
  expect(
    wrapper.find('button.pa-select-trigger[aria-label="状态筛选"]').exists(),
  ).toBe(true);
  const dead = wrapper
    .findAll('[role="option"]')
    .find((o) => o.text() === "通知失败")!;
  await dead.trigger("click");
  expect(model).toMatchObject({ notification_state: "dead" });
  wrapper.unmount();
  const other = mount(ListFilters, {
    props: {
      kind: "refunds",
      area: "merchant",
      modelValue: freshListFilters(),
    },
  });
  await other.get("button.pa-filter-toggle").trigger("click");
  expect(
    other.find('button.pa-select-trigger[aria-label="通知状态筛选"]').exists(),
  ).toBe(false);
});
