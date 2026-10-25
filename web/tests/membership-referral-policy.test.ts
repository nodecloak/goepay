import { it, expect, vi } from "vitest";
import { mount, flushPromises } from "@vue/test-utils";
import { reactive } from "vue";
import { ApiClient } from "../src/api";
import { isAdminFundsAction } from "../src/funds-authorization";
import {
  readMembershipReferralPolicy,
  membershipReferralPolicyBody,
} from "../src/membership-referral";
import MembershipReferralPolicy from "../src/components/MembershipReferralPolicy.vue";
const policy = {
  available: true,
  id: "3",
  scope: "global",
  scope_id: "0",
  revision: "2",
  enabled: false,
  rate_bps: 0,
};
const action = (w: ReturnType<typeof mount>, text: string) =>
  w.findAll("button").find((b) => b.text() === text)!;
it("requires typed availability and canonical independent scope, IDs, revision and nullable group fields", () => {
  expect(readMembershipReferralPolicy(policy, "global", "0")).toEqual(policy);
  for (const patch of [
    { available: "true" },
    { revision: 2 },
    { revision: "00" },
    { scope: "group" },
    { scope_id: "1" },
    { enabled: null },
    { rate_bps: null },
    { rate_bps: 10001 },
    { rate_bps: "0" },
  ])
    expect(() =>
      readMembershipReferralPolicy({ ...policy, ...patch }, "global", "0"),
    ).toThrow();
  const group = readMembershipReferralPolicy(
    {
      ...policy,
      scope: "group",
      scope_id: "12",
      enabled: null,
      rate_bps: null,
    },
    "group",
    "12",
  );
  expect(membershipReferralPolicyBody(group, "inherit", "")).toEqual({
    expected: "2",
    enabled: null,
    rate_bps: null,
  });
  expect(membershipReferralPolicyBody(group, "disabled", "0")).toEqual({
    expected: "2",
    enabled: false,
    rate_bps: 0,
  });
  expect(() =>
    membershipReferralPolicyBody(policy as never, "inherit", ""),
  ).toThrow();
});
it("reads only the exact unconfigured global null shape as default disabled while writes stay explicit", () => {
  const empty = {
    ...policy,
    id: "0",
    revision: "0",
    enabled: null,
    rate_bps: null,
  };
  expect(readMembershipReferralPolicy(empty, "global", "0")).toEqual(empty);
  expect(membershipReferralPolicyBody(empty as never, "disabled", "0")).toEqual(
    { expected: "0", enabled: false, rate_bps: 0 },
  );
  for (const patch of [
    { id: "1" },
    { revision: "1" },
    { enabled: false },
    { rate_bps: 0 },
  ])
    expect(() =>
      readMembershipReferralPolicy({ ...empty, ...patch }, "global", "0"),
    ).toThrow();
});
it("recognizes only canonical POST membership policy funds targets", () => {
  expect(
    isAdminFundsAction("/membership-referral-policies/global/0", "POST"),
  ).toBe(true);
  expect(
    isAdminFundsAction("/membership-referral-policies/group/12", "POST"),
  ).toBe(true);
  for (const path of [
    "/membership-referral-policies/global/1",
    "/membership-referral-policies/group/0",
    "/membership-referral-policies/global/0?x=1",
  ])
    expect(isAdminFundsAction(path, "POST")).toBe(false);
  expect(
    isAdminFundsAction("/membership-referral-policies/global/0", "GET"),
  ).toBe(false);
});
it("reviews one immutable three-field CAS body and never automatically retries an uncertain save", async () => {
  const request = vi.fn((_: string, options?: { method?: string }) =>
    options?.method === "POST"
      ? Promise.reject(new Error("连接中断"))
      : Promise.resolve(policy),
  );
  const w = mount(MembershipReferralPolicy, {
    props: {
      client: Object.assign(new ApiClient("admin"), { request }),
      adminClass: "super",
    },
  });
  await flushPromises();
  await w.findAll("select")[1]!.setValue("enabled");
  await w.findAll("input")[0]!.setValue("2500");
  await action(w, "核对会员奖励政策").trigger("click");
  await action(w, "确认保存并授权").trigger("click");
  await flushPromises();
  expect(request.mock.calls[1]).toMatchObject([
    "/membership-referral-policies/global/0",
    { method: "POST", body: { expected: "2", enabled: true, rate_bps: 2500 } },
  ]);
  expect(
    Object.keys((request.mock.calls[1]![1] as { body: object }).body).sort(),
  ).toEqual(["enabled", "expected", "rate_bps"]);
  expect(w.text()).toContain("不会自动重复提交");
  expect(request).toHaveBeenCalledTimes(2);
  w.unmount();
});
for (const key of ["area", "csrf", "role", "unmount"] as const)
  it(`discards late independent policy response after ${key} changes`, async () => {
    let finish: (v: unknown) => void = () => {};
    const request = vi.fn(
      () =>
        new Promise((resolve) => {
          finish = resolve;
        }),
    );
    const client = reactive(Object.assign(new ApiClient("admin"), { request }));
    const w = mount(MembershipReferralPolicy, {
      props: { client, adminClass: "super" },
    });
    if (key === "area") client.area = "merchant";
    if (key === "csrf") client.csrfToken = "changed";
    if (key === "role") await w.setProps({ adminClass: "operator" });
    if (key === "unmount") w.unmount();
    finish(policy);
    await flushPromises();
    if (key !== "unmount") {
      expect(w.text()).not.toContain("当前修订 2");
      w.unmount();
    }
    expect(request).toHaveBeenCalledTimes(1);
  });
