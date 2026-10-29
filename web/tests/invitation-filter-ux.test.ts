// @vitest-environment jsdom
import { it, expect, vi, afterEach } from "vitest";
import { mount, flushPromises } from "@vue/test-utils";
import InvitationCodesPage from "../src/components/InvitationCodesPage.vue";
import { ApiClient } from "../src/api";

afterEach(() => {
  document.body.innerHTML = "";
  vi.restoreAllMocks();
});

// Pro's 邀请码管理 filter: 邀请码 input + 状态 select, then 重置 and 查询.
it("the invite-code filter mirrors Pro (邀请码 + 状态 + 重置 + 查询)", async () => {
  const client = new ApiClient("admin");
  vi.spyOn(client, "request").mockResolvedValue({
    items: [],
    total: 0,
  } as any);
  const wrapper = mount(InvitationCodesPage, {
    props: { client, ownSession: true },
  });
  await flushPromises();
  expect(wrapper.find('input[aria-label="搜索邀请码"]').exists()).toBe(true);
  const status = wrapper.find('select[aria-label="状态筛选"]');
  expect(status.exists()).toBe(true);
  expect(status.findAll("option").map((o) => o.text())).toEqual([
    "全部状态",
    "未使用",
    "已使用",
  ]);
  const buttons = wrapper
    .findAll(".filter-actions button")
    .map((b) => b.text());
  // 重置 precedes 查询, per Pro.
  expect(buttons).toEqual(["重置", "查询"]);
  // the Pro toolbar actions are present
  const all = wrapper.findAll("button").map((b) => b.text());
  for (const label of ["生成邀请码", "清空全部", "清空已使用"])
    expect(all).toContain(label);
  wrapper.unmount();
});
