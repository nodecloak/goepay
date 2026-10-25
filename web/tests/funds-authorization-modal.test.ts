import { it, expect, vi } from "vitest";
import { mount, flushPromises } from "@vue/test-utils";
import { ApiClient } from "../src/api";
import FundsAuthorizationModal from "../src/components/FundsAuthorizationModal.vue";
const future = () => new Date(Date.now() + 60000).toISOString();
it("requires summary and password confirmation plus a separate final click; unknown execution never resends", async () => {
  const request = vi
    .fn()
    .mockResolvedValueOnce({
      challenge_id: "challenge",
      expires_at: future(),
      summary: { action: "付款", recipient: "acc***", amount: "1.00" },
    })
    .mockResolvedValueOnce({
      authorization: "short-grant",
      expires_at: future(),
    });
  const requestResult = vi
    .fn()
    .mockResolvedValue({ status: 202, data: { status: "unknown", id: "TF1" } });
  const client = Object.assign(new ApiClient("admin"), {
    request,
    requestResult,
  });
  const body = {
    amount: "1.00",
    external_no: "original-key",
    account: "sensitive-account",
  };
  const wrapper = mount(FundsAuthorizationModal, {
    props: { client, target: "/console/v1/admin/transfers", body },
  });
  await flushPromises();
  expect(wrapper.text()).toContain("acc***");
  expect(wrapper.text()).not.toContain("sensitive-account");
  expect(wrapper.find("input[type=password]").exists()).toBe(false);
  await wrapper.get("button[data-action=confirm-summary]").trigger("click");
  await wrapper
    .get("input[name=payment_password]")
    .setValue("private-funds-password");
  await wrapper.get("form").trigger("submit");
  await flushPromises();
  expect(requestResult).not.toHaveBeenCalled();
  expect(wrapper.find("input[type=password]").exists()).toBe(false);
  await wrapper.get("button[data-action=execute]").trigger("click");
  await flushPromises();
  expect(requestResult).toHaveBeenCalledExactlyOnceWith("/transfers", {
    method: "POST",
    body,
    paymentAuthorization: "short-grant",
  });
  expect(wrapper.emitted("completed")?.[0]).toEqual([
    { status: 202, data: { status: "unknown", id: "TF1" } },
  ]);
  expect(wrapper.text()).toContain("只能查询原业务");
  expect(wrapper.find("button[data-action=execute]").exists()).toBe(false);
  wrapper.unmount();
});
it("expires an unused grant before final execution and never submits the protected action", async () => {
  vi.useFakeTimers({ toFake: ["Date", "setInterval", "clearInterval"] });
  const request = vi
    .fn()
    .mockResolvedValueOnce({
      challenge_id: "expiring",
      expires_at: future(),
      summary: { amount: "1.00" },
    })
    .mockResolvedValueOnce({
      authorization: "expires-unused",
      expires_at: future(),
    });
  const requestResult = vi.fn();
  const wrapper = mount(FundsAuthorizationModal, {
    props: {
      client: Object.assign(new ApiClient("admin"), { request, requestResult }),
      target: "/console/v1/admin/transfers",
      body: { amount: "1.00", external_no: "original" },
    },
  });
  try {
    await flushPromises();
    await wrapper.get("button[data-action=confirm-summary]").trigger("click");
    await wrapper.get("input[name=payment_password]").setValue("funds-secret");
    await wrapper.get("form").trigger("submit");
    await flushPromises();
    expect(wrapper.find("button[data-action=execute]").exists()).toBe(true);
    await vi.advanceTimersByTimeAsync(61000);
    expect(wrapper.find("button[data-action=execute]").exists()).toBe(false);
    expect(wrapper.text()).toContain("已过期");
    expect(requestResult).not.toHaveBeenCalled();
  } finally {
    wrapper.unmount();
    vi.useRealTimers();
  }
});
it("discards a ready grant if the immutable business tuple changes and never submits the old authorization", async () => {
  const request = vi
    .fn()
    .mockResolvedValueOnce({
      challenge_id: "first",
      expires_at: future(),
      summary: { amount: "1.00" },
    })
    .mockResolvedValueOnce({
      authorization: "bound-grant",
      expires_at: future(),
    });
  const requestResult = vi.fn();
  const client = Object.assign(new ApiClient("admin"), {
    request,
    requestResult,
  });
  const wrapper = mount(FundsAuthorizationModal, {
    props: {
      client,
      target: "/console/v1/admin/transfers",
      body: { amount: "1.00", external_no: "key" },
    },
  });
  await flushPromises();
  await wrapper.get("button[data-action=confirm-summary]").trigger("click");
  await wrapper.get("input[name=payment_password]").setValue("funds-password");
  await wrapper.get("form").trigger("submit");
  await flushPromises();
  expect(wrapper.find("button[data-action=execute]").exists()).toBe(true);
  await wrapper.setProps({ body: { amount: "2.00", external_no: "key" } });
  expect(wrapper.find("button[data-action=execute]").exists()).toBe(false);
  expect(wrapper.text()).toContain("操作参数已变化");
  expect(requestResult).not.toHaveBeenCalled();
  wrapper.unmount();
});
it("invalidates grants on body changes and clears failed authentication password without executing", async () => {
  const request = vi
    .fn()
    .mockResolvedValueOnce({
      challenge_id: "challenge",
      expires_at: future(),
      summary: { amount: "1.00" },
    })
    .mockRejectedValueOnce(new Error("wrong funds password"));
  const requestResult = vi.fn();
  const client = Object.assign(new ApiClient("admin"), {
    request,
    requestResult,
  });
  const wrapper = mount(FundsAuthorizationModal, {
    props: {
      client,
      target: "/console/v1/admin/transfers",
      body: { amount: "1.00", external_no: "same" },
    },
  });
  await flushPromises();
  await wrapper.get("button[data-action=confirm-summary]").trigger("click");
  await wrapper.get("input[name=payment_password]").setValue("wrong");
  await wrapper.get("form").trigger("submit");
  await flushPromises();
  expect(wrapper.text()).toContain("wrong funds password");
  for (const input of wrapper.findAll("input"))
    expect((input.element as HTMLInputElement).value).toBe("");
  await wrapper.setProps({ body: { amount: "2.00", external_no: "same" } });
  await flushPromises();
  expect(wrapper.text()).toContain("操作参数已变化");
  expect(wrapper.find("button[data-action=execute]").exists()).toBe(false);
  expect(requestResult).not.toHaveBeenCalled();
  wrapper.unmount();
});
