import { it, expect } from "vitest";
import { mount } from "@vue/test-utils";
import OrderIncomePanel from "../src/components/OrderIncomePanel.vue";
import { ApiClient } from "../src/api";
it("never exposes platform income writes to merchant clients even with contradictory flags", () => {
  const wrapper = mount(OrderIncomePanel, {
    props: {
      client: new ApiClient("merchant"),
      tradeNo: "T",
      hold: { id: "H", state: "held" },
      canFreeze: true,
      canUnfreeze: true,
    },
  });
  expect(wrapper.findAll("button")).toHaveLength(0);
  expect(wrapper.text()).toContain("不是买家预授权");
  wrapper.unmount();
});
