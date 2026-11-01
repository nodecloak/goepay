import { expect, it, vi } from "vitest";
import { mount, flushPromises } from "@vue/test-utils";
import { ApiClient } from "../src/api";
import ComplaintsPage from "../src/components/ComplaintsPage.vue";

it("filters the complete task list by exact channel/status, resets and only selects visible rows", async () => {
  const client = new ApiClient("admin"),
    writes: string[] = [];
  const request = vi
    .spyOn(client, "request")
    .mockImplementation(async (path, options) => {
      if (options?.method && options.method !== "GET") writes.push(path);
      return {
        items: [
          {
            channel_id: "7",
            name: "微信",
            enabled: true,
            frequency: 6,
            num: 20,
          },
          {
            channel_id: "9007199254740993",
            name: "支付宝",
            enabled: false,
            frequency: 6,
            num: 20,
          },
        ],
      } as never;
    });
  const view = mount(ComplaintsPage, {
    attachTo: document.body,
    props: {
      client,
      area: "admin",
      path: "/complain/task",
      canConfigure: true,
    },
    global: { stubs: { teleport: true } },
  });
  try {
    await flushPromises();
    expect(view.findAll("tbody tr")).toHaveLength(2);
    await view.get('input[aria-label="通道ID"]').setValue("9007199254740993");
    await view.get('button[aria-label="状态"]').trigger("click");
    await view
      .findAll('[role="option"]')
      .find((x) => x.text() === "已禁用")!
      .trigger("click");
    const before = request.mock.calls.length;
    await view.get("form.pa-filter").trigger("submit");
    await flushPromises();
    expect(request.mock.calls.length).toBe(before + 1);
    expect(view.findAll("tbody tr")).toHaveLength(1);
    expect(view.find("tbody").text()).toContain("支付宝");
    await view.get('input[aria-label="全选"]').setValue(true);
    expect(view.get('[data-action="batch-delete"]').text()).toContain("1");
    await view
      .findAll("form.pa-filter button")
      .find((b) => b.text() === "重置")!
      .trigger("click");
    await flushPromises();
    expect(view.findAll("tbody tr")).toHaveLength(2);
    expect(
      view.get('[data-action="batch-delete"]').attributes("disabled"),
    ).toBeDefined();
    expect(writes).toEqual([]);
    await view.get('input[aria-label="通道ID"]').setValue("01");
    const count = request.mock.calls.length;
    await view.get("form.pa-filter").trigger("submit");
    await flushPromises();
    expect(request.mock.calls.length).toBe(count);
    expect(view.get('[role="alert"]').text()).toContain("通道ID");
  } finally {
    view.unmount();
  }
});
