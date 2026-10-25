import { it, expect, vi } from "vitest";
import { mount, flushPromises } from "@vue/test-utils";
import { reactive } from "vue";
import { ApiClient } from "../src/api";
import ReferralPolicyPage from "../src/components/ReferralPolicyPage.vue";
it("leaving policy page during a late response cannot start a new read after unmount", async () => {
  const policy = {
    id: "0",
    scope: "global",
    scope_id: "0",
    revision: "0",
    enabled: false,
    rate_bps: 0,
    mode: "immediate",
    basis: "fee",
    refund_policy: "any_successful_refund_voids_reward",
  };
  let finish: (v: unknown) => void = () => {};
  const request = vi.fn((_path: string, options?: { method?: string }) =>
    options?.method === "POST"
      ? new Promise((resolve) => (finish = resolve))
      : Promise.resolve(policy),
  );
  const wrapper = mount(ReferralPolicyPage, {
    props: {
      client: Object.assign(new ApiClient("admin"), { request }),
      adminClass: "super",
    },
  });
  await flushPromises();
  await wrapper
    .findAll("button")
    .find((b) => b.text() === "核对政策变更")!
    .trigger("click");
  await wrapper
    .findAll("button")
    .find((b) => b.text() === "确认保存并授权")!
    .trigger("click");
  expect(request).toHaveBeenCalledTimes(2);
  wrapper.unmount();
  finish({ ...policy, id: "1", revision: "1" });
  await flushPromises();
  expect(request).toHaveBeenCalledTimes(2);
});

const currentPolicy = {
  id: "8",
  scope: "global",
  scope_id: "0",
  revision: "7",
  enabled: true,
  rate_bps: 500,
  mode: "immediate",
  basis: "fee",
  refund_policy: "any_successful_refund_voids_reward",
};
const action = (wrapper: ReturnType<typeof mount>, text: string) =>
  wrapper.findAll("button").find((b) => b.text() === text)!;
it("selects an explicit gross basis and freezes it in the reviewed CAS body", async () => {
  const request = vi.fn().mockResolvedValue(currentPolicy);
  const wrapper = mount(ReferralPolicyPage, {
    props: {
      client: Object.assign(new ApiClient("admin"), { request }),
      adminClass: "super",
    },
  });
  await flushPromises();
  const basis = wrapper
    .findAll("select")
    .find((s) => s.element.closest("label")?.textContent?.includes("奖励基数"));
  expect(basis).toBeDefined();
  await basis!.setValue("gross");
  await action(wrapper, "核对政策变更").trigger("click");
  expect(wrapper.text()).toContain("原订单金额");
  expect(wrapper.text()).toContain("原订单平台手续费");
  expect(request).toHaveBeenCalledTimes(1);
  await action(wrapper, "确认保存并授权").trigger("click");
  await flushPromises();
  expect(request.mock.calls[1]).toMatchObject([
    "/referral-policies/global/0",
    { method: "POST", body: { expected: "7", basis: "gross", rate_bps: 500 } },
  ]);
  wrapper.unmount();
});

it("clears a loaded policy and pending confirmation when the administrator loses the original role", async () => {
  const request = vi.fn().mockResolvedValue(currentPolicy);
  const wrapper = mount(ReferralPolicyPage, {
    props: {
      client: Object.assign(new ApiClient("admin"), { request }),
      adminClass: "super",
    },
  });
  await flushPromises();
  await action(wrapper, "核对政策变更").trigger("click");
  await wrapper.setProps({ adminClass: "operator" });
  expect(wrapper.text()).not.toContain("当前修订 7");
  expect(action(wrapper, "确认保存并授权")).toBeUndefined();
  expect(request).toHaveBeenCalledTimes(1);
  wrapper.unmount();
});

it("ignores a late policy read after client/session context changes", async () => {
  let finish: (v: unknown) => void = () => {};
  const oldRequest = vi.fn(
    () =>
      new Promise((resolve) => {
        finish = resolve;
      }),
  );
  const nextRequest = vi
    .fn()
    .mockResolvedValue({ ...currentPolicy, revision: "9" });
  const wrapper = mount(ReferralPolicyPage, {
    props: {
      client: Object.assign(new ApiClient("admin"), { request: oldRequest }),
      adminClass: "super",
    },
  });
  await wrapper.setProps({
    client: Object.assign(new ApiClient("admin"), { request: nextRequest }),
  });
  finish(currentPolicy);
  await flushPromises();
  expect(wrapper.text()).not.toContain("当前修订 7");
  expect(nextRequest).not.toHaveBeenCalled();
  wrapper.unmount();
});

it("a late old save cannot publish notice or start a read in a new client context", async () => {
  let finish: (v: unknown) => void = () => {};
  const request = vi.fn((_path: string, options?: { method?: string }) =>
    options?.method === "POST"
      ? new Promise((resolve) => {
          finish = resolve;
        })
      : Promise.resolve(currentPolicy),
  );
  const nextRequest = vi
    .fn()
    .mockResolvedValue({ ...currentPolicy, revision: "9" });
  const wrapper = mount(ReferralPolicyPage, {
    props: {
      client: Object.assign(new ApiClient("admin"), { request }),
      adminClass: "super",
    },
  });
  await flushPromises();
  await action(wrapper, "核对政策变更").trigger("click");
  await action(wrapper, "确认保存并授权").trigger("click");
  await wrapper.setProps({
    client: Object.assign(new ApiClient("admin"), { request: nextRequest }),
  });
  finish(currentPolicy);
  await flushPromises();
  expect(nextRequest).not.toHaveBeenCalled();
  expect(wrapper.text()).not.toContain("新政策版本已保存");
  expect(wrapper.text()).not.toContain("当前修订 7");
  wrapper.unmount();
});

for (const context of ["area", "csrf", "scope", "group"] as const)
  it(`rejects late data after the ${context} context changes`, async () => {
    let finish: (v: unknown) => void = () => {};
    const request = vi.fn(
      () =>
        new Promise((resolve) => {
          finish = resolve;
        }),
    );
    const client = reactive(Object.assign(new ApiClient("admin"), { request }));
    const wrapper = mount(ReferralPolicyPage, {
      props: { client, adminClass: "super" },
    });
    if (context === "area") client.area = "merchant";
    else if (context === "csrf") client.csrfToken = "new-session-csrf";
    else {
      await wrapper.findAll("select")[0]!.setValue("group");
      if (context === "group") await wrapper.find("input").setValue("9");
    }
    finish(currentPolicy);
    await flushPromises();
    expect(wrapper.text()).not.toContain("当前修订 7");
    expect(request).toHaveBeenCalledTimes(1);
    wrapper.unmount();
  });

it("keeps an uncertain POST blocked until an explicit successful read without automatic resend", async () => {
  const request = vi.fn((_path: string, options?: { method?: string }) =>
    options?.method === "POST"
      ? Promise.reject(new Error("synthetic uncertain response"))
      : Promise.resolve(currentPolicy),
  );
  const wrapper = mount(ReferralPolicyPage, {
    props: {
      client: Object.assign(new ApiClient("admin"), { request }),
      adminClass: "super",
    },
  });
  await flushPromises();
  await action(wrapper, "核对政策变更").trigger("click");
  await action(wrapper, "确认保存并授权").trigger("click");
  await flushPromises();
  expect(request).toHaveBeenCalledTimes(2);
  expect(action(wrapper, "核对政策变更").attributes("disabled")).toBeDefined();
  expect(wrapper.text()).toContain("不会自动重复提交");
  await wrapper.find("form").trigger("submit");
  await flushPromises();
  expect(
    request.mock.calls.filter(([, o]) => o?.method === "POST"),
  ).toHaveLength(1);
  expect(request).toHaveBeenCalledTimes(3);
  expect(
    action(wrapper, "核对政策变更").attributes("disabled"),
  ).toBeUndefined();
  wrapper.unmount();
});
