import { it, expect, vi } from "vitest";
import { mount, flushPromises } from "@vue/test-utils";
import { ApiClient } from "../src/api";
import { referralPolicyBody, type ReferralPolicyView } from "../src/referral";
import ReferralPolicyPage from "../src/components/ReferralPolicyPage.vue";

const policy = {
  id: "8",
  scope: "global",
  scope_id: "0",
  revision: "7",
  enabled: true,
  rate_bps: 500,
  mode: "immediate",
  basis: "gross",
  refund_policy: "any_successful_refund_voids_reward",
  daily_mode_available: true,
} as ReferralPolicyView;
const button = (w: ReturnType<typeof mount>, text: string) =>
  w.findAll("button").find((b) => b.text() === text)!;
it("freezes an explicit daily mode and original fee budget in the CAS body", () => {
  expect(
    referralPolicyBody(policy, "enabled", "500", "gross", "daily"),
  ).toEqual({
    expected: "7",
    enabled: true,
    rate_bps: 500,
    mode: "daily",
    basis: "gross",
    refund_policy: "any_successful_refund_voids_reward",
  });
});
it("never silently downgrades an original daily policy, and requires explicit capability", () => {
  const daily = { ...policy, mode: "daily" } as ReferralPolicyView;
  expect(referralPolicyBody(daily, "enabled", "500")).toMatchObject({
    mode: "daily",
  });
  for (const view of [
    { ...daily, daily_mode_available: false },
    { ...daily, daily_mode_available: undefined },
  ])
    expect(() => referralPolicyBody(view, "enabled", "500")).toThrow();
  expect(() =>
    referralPolicyBody(
      { ...policy, daily_mode_available: false },
      "enabled",
      "500",
      "fee",
      "daily",
    ),
  ).toThrow();
});
it("loads daily without converting to immediate and binds the reviewed mode to the existing save", async () => {
  const request = vi.fn().mockResolvedValue({ ...policy, mode: "daily" });
  const w = mount(ReferralPolicyPage, {
    props: {
      client: Object.assign(new ApiClient("admin"), { request }),
      adminClass: "super",
    },
  });
  await flushPromises();
  const mode = w.find('button.pa-select-trigger[aria-label="发奖时机"]');
  expect(mode.exists()).toBe(true);
  expect(mode.text()).toContain("次日北京时间");
  await button(w, "核对政策变更").trigger("click");
  expect(w.text()).toContain("次日北京时间");
  expect(w.text()).toContain("专用调度器");
  await button(w, "确认保存并授权").trigger("click");
  await flushPromises();
  expect(request.mock.calls[1]).toMatchObject([
    "/referral-policies/global/0",
    { method: "POST", body: { expected: "7", mode: "daily", basis: "gross" } },
  ]);
  w.unmount();
});
it("an unavailable daily capability blocks editing a daily GET rather than posting immediate", async () => {
  const request = vi
    .fn()
    .mockResolvedValue({
      ...policy,
      mode: "daily",
      daily_mode_available: false,
    });
  const w = mount(ReferralPolicyPage, {
    props: {
      client: Object.assign(new ApiClient("admin"), { request }),
      adminClass: "super",
    },
  });
  await flushPromises();
  expect(w.find('[role="alert"]').exists()).toBe(true);
  expect(button(w, "核对政策变更")).toBeUndefined();
  expect(request).toHaveBeenCalledTimes(1);
  w.unmount();
});
