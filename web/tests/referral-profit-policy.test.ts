import { it, expect, vi } from "vitest";
import { mount, flushPromises } from "@vue/test-utils";
import { ApiClient } from "../src/api";
import { referralPolicyBody, type ReferralPolicyView } from "../src/referral";
import ReferralPolicyPage from "../src/components/ReferralPolicyPage.vue";

const base: ReferralPolicyView = {
  id: "9",
  scope: "global",
  scope_id: "0",
  revision: "8",
  enabled: true,
  rate_bps: 500,
  mode: "immediate",
  basis: "fee",
  refund_policy: "any_successful_refund_voids_reward",
  daily_mode_available: true,
};
const button = (w: ReturnType<typeof mount>, label: string) =>
  w.findAll("button").find((x) => x.text() === label)!;

it("requires explicit true capability for profit in both original and newly selected policy", () => {
  for (const capability of [undefined, false, "true", 1]) {
    const view = {
      ...base,
      profit_basis_available: capability,
    } as unknown as ReferralPolicyView;
    expect(() =>
      referralPolicyBody(view, "enabled", "500", "profit"),
    ).toThrow();
    expect(() =>
      referralPolicyBody({ ...view, basis: "profit" }, "enabled", "500", "fee"),
    ).toThrow();
  }
});

it("preserves the original six-field CAS body for explicit profit, without accepting client cost inputs", () => {
  for (const mode of ["immediate", "daily"] as const) {
    const view = { ...base, mode, profit_basis_available: true };
    expect(referralPolicyBody(view, "enabled", "500", "profit", mode)).toEqual({
      expected: "8",
      enabled: true,
      rate_bps: 500,
      mode,
      basis: "profit",
      refund_policy: "any_successful_refund_voids_reward",
    });
  }
});

it("keeps unavailable profit hidden and rejects an untrusted old profit response", async () => {
  const unavailable = mount(ReferralPolicyPage, {
    props: {
      client: Object.assign(new ApiClient("admin"), {
        request: vi.fn().mockResolvedValue(base),
      }),
      adminClass: "super",
    },
  });
  await flushPromises();
  await unavailable
    .get('button.pa-select-trigger[aria-label="奖励基数"]')
    .trigger("click");
  expect(
    unavailable
      .findAll('[role="option"]')
      .some((o) => o.text().includes("配置估计利润")),
  ).toBe(false);
  unavailable.unmount();
  const request = vi.fn().mockResolvedValue({
    ...base,
    basis: "profit",
    profit_basis_available: false,
  });
  const old = mount(ReferralPolicyPage, {
    props: {
      client: Object.assign(new ApiClient("admin"), { request }),
      adminClass: "super",
    },
  });
  await flushPromises();
  expect(old.find('[role="alert"]').exists()).toBe(true);
  expect(button(old, "核对政策变更")).toBeUndefined();
  expect(request).toHaveBeenCalledTimes(1);
  old.unmount();
});

it("reviews configured estimated profit and sends once with the original revision and basis", async () => {
  const request = vi
    .fn()
    .mockResolvedValue({ ...base, profit_basis_available: true });
  const w = mount(ReferralPolicyPage, {
    props: {
      client: Object.assign(new ApiClient("admin"), { request }),
      adminClass: "super",
    },
  });
  await flushPromises();
  await w
    .get('button.pa-select-trigger[aria-label="奖励基数"]')
    .trigger("click");
  await w
    .findAll('[role="option"]')
    .find((o) => o.text().includes("配置估计利润"))!
    .trigger("click");
  await button(w, "核对政策变更").trigger("click");
  expect(w.text()).toContain("配置估计利润");
  expect(w.text()).toContain("非实际账单");
  expect(w.text()).toContain("可信成本");
  expect(w.text()).toContain("不使用其它订单资金");
  await button(w, "确认保存并授权").trigger("click");
  await flushPromises();
  expect(request.mock.calls[1]).toMatchObject([
    "/referral-policies/global/0",
    {
      method: "POST",
      body: {
        expected: "8",
        basis: "profit",
        mode: "immediate",
        rate_bps: 500,
      },
    },
  ]);
  expect(Object.keys(request.mock.calls[1]![1].body).sort()).toEqual([
    "basis",
    "enabled",
    "expected",
    "mode",
    "rate_bps",
    "refund_policy",
  ]);
  w.unmount();
});
