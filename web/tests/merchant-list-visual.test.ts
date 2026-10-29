// @vitest-environment jsdom
import { it, expect, vi, afterEach } from "vitest";
import { mount, flushPromises } from "@vue/test-utils";
import ResourcePage from "../src/components/ResourcePage.vue";
import ActionDropdown from "../src/components/ActionDropdown.vue";
import { ApiClient } from "../src/api";
import {
  originalMerchantFields,
  stubMerchantList,
} from "./merchant-list-fixture";

afterEach(() => {
  document.body.innerHTML = "";
  vi.restoreAllMocks();
});

it("shows Pro-style row actions: 编辑, a disabled 删除 with reason, and 更多", async () => {
  const client = new ApiClient("admin");
  stubMerchantList(client, originalMerchantFields);
  const wrapper = mount(ResourcePage, {
    props: {
      client,
      resource: "merchants",
      ownSession: true,
      adminClass: "super",
    },
  });
  await flushPromises();
  const actionButtons = wrapper.findAll(".row-actions button");
  const edit = actionButtons.find((b) => b.text() === "编辑");
  expect(edit).toBeTruthy();
  expect(edit!.classes()).toContain("pa-btn--primary");
  // 删除 present but disabled (merchants have no reversible hard delete) with a reason.
  const del = actionButtons.find((b) => b.text() === "删除");
  expect(del).toBeTruthy();
  expect(del!.attributes("disabled")).toBeDefined();
  expect(del!.classes()).toContain("pa-btn--danger");
  expect(del!.attributes("title")).toContain("不可删除");
  // 详情 and 代登录商户 are NOT standalone buttons — they live in 更多.
  expect(actionButtons.some((b) => b.text() === "详情")).toBe(false);
  expect(actionButtons.some((b) => b.text() === "代登录商户")).toBe(false);
  const more = wrapper.findComponent(ActionDropdown);
  expect(more.exists()).toBe(true);
  const labels = (more.props("items") as { label: string }[]).map(
    (i) => i.label,
  );
  for (const l of ["详情", "代登录商户", "加款/扣款"])
    expect(labels).toContain(l);
  wrapper.unmount();
});
