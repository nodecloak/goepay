import { it, expect, vi } from "vitest";
import { mount, flushPromises } from "@vue/test-utils";
import OrderIncomePanel from "../src/components/OrderIncomePanel.vue";
import { ApiClient } from "../src/api";

it("confirms only the remaining held amount and hides release while a refund remains reserved", async () => {
  const wrapper = mount(OrderIncomePanel, {
    props: {
      client: new ApiClient("admin"),
      tradeNo: "T",
      canFreeze: false,
      canUnfreeze: true,
      hold: {
        id: "9",
        state: "held",
        amount: "1000",
        remaining_amount: "600",
        refund_reserved_amount: "0",
        source_mode: "fresh",
      },
    },
  });
  await wrapper
    .findAll("button")
    .find((b) => b.text() === "解冻订单收入")!
    .trigger("click");
  const confirmation = wrapper
    .findAll(".alert")
    .find((e) => e.text().includes("确认解冻指定批次"))!;
  expect(confirmation.text()).toContain("600");
  expect(confirmation.text()).not.toContain("1000");
  await wrapper.setProps({
    hold: {
      id: "9",
      state: "held",
      amount: "1000",
      remaining_amount: "600",
      refund_reserved_amount: "100",
      source_mode: "fresh",
    },
  });
  expect(
    wrapper.findAll("button").some((b) => b.text() === "解冻订单收入"),
  ).toBe(false);
  expect(wrapper.text()).toContain("退款结果尚未确定");
  await wrapper.setProps({
    hold: {
      id: "9",
      state: "held",
      amount: "1000",
      remaining_amount: "0",
      refund_reserved_amount: "0",
      refund_pending_count: "1",
      source_mode: "fresh",
    },
  });
  expect(
    wrapper.findAll("button").some((b) => b.text() === "解冻订单收入"),
  ).toBe(false);
  expect(wrapper.text()).toContain("退款结果尚未确定");
  wrapper.unmount();
});

it("retries historical adoption with the same hold and request after a lost response", async () => {
  const client = new ApiClient("admin");
  const request = vi
    .spyOn(client, "request")
    .mockImplementation(async (_path, options) => {
      await options?.beforeDispatch?.();
      throw new Error("response lost");
    });
  const wrapper = mount(OrderIncomePanel, {
    props: {
      client,
      tradeNo: "T",
      canFreeze: false,
      canUnfreeze: false,
      canAdopt: true,
      hold: {
        id: "9",
        state: "held",
        amount: "1000",
        source_mode: "legacy_unprepared",
      },
    },
  });
  const click = async (label: string) => {
    await wrapper
      .findAll("button")
      .find((b) => b.text() === label)!
      .trigger("click");
    await flushPromises();
  };
  await click("准备历史冻结款退款来源");
  expect(request).not.toHaveBeenCalled();
  await click("确认迁移指定冻结款");
  const first = request.mock.calls[0];
  expect(first[0]).toBe("/orders/T/adopt-income-hold");
  expect(first[1]?.body).toMatchObject({
    hold_id: "9",
    external_no: expect.any(String),
  });
  expect(wrapper.text()).toContain("迁移结果尚未核实");
  await click("重试原迁移请求");
  await click("确认迁移指定冻结款");
  const second = request.mock.calls[1];
  expect(second[0]).toEqual(first[0]);
  expect(second[1]?.body).toEqual(first[1]?.body);
  expect(second[1]).toMatchObject({
    method: "POST",
    body: first[1]?.body,
    beforeDispatch: expect.any(Function),
  });
  wrapper.unmount();
});

it("never offers historical source adoption to a merchant despite contradictory server flags", () => {
  const wrapper = mount(OrderIncomePanel, {
    props: {
      client: new ApiClient("merchant"),
      tradeNo: "T",
      canFreeze: false,
      canUnfreeze: false,
      canAdopt: true,
      hold: {
        id: "9",
        state: "held",
        amount: "1000",
        source_mode: "legacy_unprepared",
      },
    },
  });
  expect(wrapper.findAll("button")).toHaveLength(0);
  wrapper.unmount();
});
