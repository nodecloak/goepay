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
  const notification = wrapper.get('select[aria-label="通知状态筛选"]');
  expect(
    notification.findAll("option").map((x) => x.attributes("value")),
  ).toEqual(["", "none", "pending", "running", "succeeded", "dead"]);
  await notification.setValue("dead");
  expect(model).toMatchObject({
    notification_state: "dead",
  });
  wrapper.unmount();
  const other = mount(ListFilters, {
    props: {
      kind: "refunds",
      area: "merchant",
      modelValue: freshListFilters(),
    },
  });
  expect(other.find('select[aria-label="通知状态筛选"]').exists()).toBe(false);
});
