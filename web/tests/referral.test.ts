import { expect, it } from "vitest";
import { sanitizePromotionLocation } from "../src/referral";
import {
  referralPolicyBody,
  type ReferralPolicyView,
  referralAmount,
} from "../src/referral";
import { viewKind, visibleMenu, navigationMenus } from "../src/menu";
import { isAdminFundsAction } from "../src/funds-authorization";
it("routes original invitation and super-only policy pages without exposing policy to operators", () => {
  expect(viewKind("merchant", "/invite")).toBe("referral");
  expect(viewKind("admin", "/system/referral-policies")).toBe(
    "referral-policy",
  );
  const operator = JSON.stringify(
    visibleMenu(navigationMenus("admin"), "R_ADMIN"),
  );
  expect(operator).not.toContain("/system/referral-policies");
  expect(isAdminFundsAction("/referral-policies/global/0", "POST")).toBe(true);
  expect(isAdminFundsAction("/referral-policies/group/9", "POST")).toBe(true);
  expect(isAdminFundsAction("/referral-policies/group/09", "POST")).toBe(false);
});
const policy: ReferralPolicyView = {
  id: "0",
  scope: "group",
  scope_id: "9",
  revision: "0",
  enabled: null,
  rate_bps: null,
  mode: "immediate",
  basis: "fee",
  refund_policy: "any_successful_refund_voids_reward",
};
it("policy preserves explicit zero vs inheritance and fixed immediate fee refund semantics", () => {
  expect(referralPolicyBody(policy, "inherit", "")).toEqual({
    expected: "0",
    enabled: null,
    rate_bps: null,
    mode: "immediate",
    basis: "fee",
    refund_policy: "any_successful_refund_voids_reward",
  });
  expect(referralPolicyBody(policy, "disabled", "0")).toMatchObject({
    enabled: false,
    rate_bps: 0,
  });
  expect(() =>
    referralPolicyBody(
      { ...policy, scope: "global", scope_id: "0" },
      "inherit",
      "",
    ),
  ).toThrow();
  for (const rate of ["-1", "10001", "0.1", "1e3"])
    expect(() => referralPolicyBody(policy, "enabled", rate)).toThrow();
  expect(referralAmount("9007199254740993")).toBe("90071992547409.93");
});
it("gross basis is explicit in the original CAS body, while default fee stays unchanged", () => {
  const gross = { ...policy, basis: "gross" } as ReferralPolicyView;
  expect(referralPolicyBody(gross, "enabled", "500")).toMatchObject({
    expected: "0",
    basis: "gross",
    mode: "immediate",
    rate_bps: 500,
  });
  expect(referralPolicyBody(policy, "inherit", "")).toMatchObject({
    basis: "fee",
  });
  expect(() =>
    referralPolicyBody(
      { ...policy, basis: "profit" } as unknown as ReferralPolicyView,
      "enabled",
      "500",
    ),
  ).toThrow();
});
const token = "rp_" + "A".repeat(43);
it("removes promotion bearer from URL before using a single explicit form prefill", () => {
  let clean = "";
  expect(
    sanitizePromotionLocation(
      "https://local.invalid/merchant/dashboard#promotion_token=" + token,
      (v) => (clean = v),
    ),
  ).toBe(token);
  expect(clean).toBe("/merchant/dashboard");
  expect(
    sanitizePromotionLocation(
      "https://local.invalid/merchant/dashboard?promotion_token=" +
        token +
        "&keep=1",
      (v) => (clean = v),
    ),
  ).toBe(token);
  expect(clean).toBe("/merchant/dashboard?keep=1");
});
it("does not confuse admission invite code with promotion and strips ambiguous or invalid promotion tokens", () => {
  let clean = "";
  expect(
    sanitizePromotionLocation(
      "https://local.invalid/merchant?invite_code=admission",
      (v) => (clean = v),
    ),
  ).toBe("");
  expect(clean).toBe("");
  expect(
    sanitizePromotionLocation(
      "https://local.invalid/merchant?promotion_token=" +
        token +
        "#promotion_token=" +
        token,
      (v) => (clean = v),
    ),
  ).toBe("");
  expect(clean).not.toContain(token);
  expect(
    sanitizePromotionLocation(
      "https://local.invalid/merchant?promotion_token=invalid",
      (v) => (clean = v),
    ),
  ).toBe("");
  expect(clean).not.toContain("promotion_token");
});
