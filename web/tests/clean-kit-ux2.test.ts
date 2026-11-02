import { it, expect, vi } from "vitest";
import { mount, flushPromises } from "@vue/test-utils";
import OperationsPage from "../src/components/OperationsPage.vue";

function client() {
  return {
    area: "admin",
    csrfToken: "t",
    request: vi.fn().mockResolvedValue({ items: [], total: 0 }),
  } as unknown as import("../src/api").ApiClient;
}

it("clean page shows Pro's 自定义清理 grid with every clean type", async () => {
  const wrapper = mount(OperationsPage, {
    props: {
      client: client(),
      operation: "cleanup",
      ownSession: true,
      adminClass: "super",
    },
  });
  await flushPromises();
  const text = wrapper.text();
  // No page-title heading; the custom-clean section keeps its own heading.
  expect(wrapper.find("h2").exists()).toBe(false);
  expect(wrapper.findAll("h3.clean-subhead").map((h) => h.text())).toEqual([
    "快捷清理",
    "自定义清理",
  ]);
  for (const t of [
    "登录日志",
    "订单记录",
    "退款记录",
    "结算记录",
    "资金明细",
    "付款记录",
    "分账记录",
    "投诉记录",
    "风控记录",
  ])
    expect(text).toContain(t);
});

it("financial clean types are disabled with a one-line reason; only 登录日志 is actionable", async () => {
  const wrapper = mount(OperationsPage, {
    props: {
      client: client(),
      operation: "cleanup",
      ownSession: true,
      adminClass: "super",
    },
  });
  await flushPromises();
  const items = wrapper.findAll(".clean-item");
  expect(items.length).toBe(9);
  // 8 financial/non-prunable types carry the 不可删除 tag + reason, no date input.
  const off = wrapper.findAll(".clean-item--off");
  expect(off.length).toBe(8);
  expect(wrapper.text()).toContain("不可变财务记录");
  // exactly one type (登录日志) exposes the date input + 预览清理范围 button.
  expect(wrapper.findAll('input[type="date"].pa-input').length).toBe(1);
  const preview = wrapper
    .findAll("button")
    .filter((b) => b.text() === "预览清理范围");
  expect(preview.length).toBe(1);
});
