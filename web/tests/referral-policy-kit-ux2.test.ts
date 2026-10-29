import { it, expect, vi } from "vitest";
import { mount, flushPromises } from "@vue/test-utils";
import ReferralPolicyPage from "../src/components/ReferralPolicyPage.vue";

function client() {
  return {
    area: "admin",
    csrfToken: "t",
    request: vi.fn().mockResolvedValue({
      scope: "global",
      scope_id: "0",
      revision: "0",
      id: "0",
      enabled: null,
      rate_bps: null,
      mode: "immediate",
      basis: "fee",
      refund_policy: "any_successful_refund_voids_reward",
      daily_mode_available: false,
      profit_basis_available: false,
    }),
  } as unknown as import("../src/api").ApiClient;
}

it("referral policy page uses kit field + kit select for 政策范围 and the policy form", async () => {
  const wrapper = mount(ReferralPolicyPage, {
    props: { client: client(), adminClass: "super" },
    global: { stubs: { MembershipReferralPolicy: true } },
  });
  await flushPromises();
  // 政策范围 kit select renders in the top form
  expect(wrapper.find(".referral-form .pa-select-trigger").exists()).toBe(true);
  expect(wrapper.text()).toContain("政策范围");
  // policy form kit selects (启用状态/奖励基数/发奖时机) once a version loads
  expect(wrapper.findAll(".pa-select-trigger").length).toBeGreaterThanOrEqual(
    3,
  );
  expect(wrapper.text()).toContain("启用状态");
  expect(wrapper.text()).toContain("返现比例 BPS");
});
