import { it, expect, vi, afterEach } from "vitest";
import { mount, flushPromises } from "@vue/test-utils";
import { ApiClient } from "../src/api";
import PaymentPasswordSettings from "../src/components/PaymentPasswordSettings.vue";
afterEach(() => vi.unstubAllGlobals());
it("uses own administrator login, existing funds password and required TOTP, clears every secret after failure", async () => {
  const request = vi
    .fn()
    .mockResolvedValueOnce({
      configured: true,
      enforced: true,
      credential_epoch: 2,
      totp_required: true,
    })
    .mockRejectedValueOnce(new Error("verification rejected"));
  const client = Object.assign(new ApiClient("admin"), { request });
  const wrapper = mount(PaymentPasswordSettings, { props: { client } });
  await flushPromises();
  await wrapper.get("button").trigger("click");
  await wrapper.get("input[name=current_password]").setValue("login-password");
  await wrapper
    .get("input[name=current_payment_password]")
    .setValue("old-funds-password");
  await wrapper
    .get("input[name=new_payment_password]")
    .setValue("new-funds-password");
  await wrapper.get("input[name=confirmation]").setValue("new-funds-password");
  await wrapper.get("input[name=code]").setValue("123456");
  await wrapper.get("form").trigger("submit");
  await flushPromises();
  expect(request).toHaveBeenLastCalledWith(
    "/security/payment-password",
    expect.objectContaining({
      method: "PUT",
      body: {
        current_password: "login-password",
        current_payment_password: "old-funds-password",
        new_payment_password: "new-funds-password",
        code: "123456",
      },
    }),
  );
  expect(wrapper.text()).toContain("verification rejected");
  for (const input of wrapper.findAll("input"))
    expect((input.element as HTMLInputElement).value).toBe("");
  wrapper.unmount();
});
it("initial setup does not request an old funds password and cancellation clears the component form", async () => {
  const request = vi
    .fn()
    .mockResolvedValue({
      configured: false,
      enforced: false,
      credential_epoch: 0,
      totp_required: false,
    });
  const wrapper = mount(PaymentPasswordSettings, {
    props: { client: Object.assign(new ApiClient("admin"), { request }) },
  });
  await flushPromises();
  await wrapper.get("button").trigger("click");
  expect(wrapper.find("input[name=current_payment_password]").exists()).toBe(
    false,
  );
  expect(wrapper.find("input[name=code]").exists()).toBe(false);
  await wrapper.get("input[name=current_password]").setValue("login-secret");
  await wrapper
    .get("input[name=new_payment_password]")
    .setValue("new-funds-secret");
  await wrapper.get("button[type=button]").trigger("click");
  expect(wrapper.find("form").exists()).toBe(false);
  await wrapper.get("button").trigger("click");
  for (const input of wrapper.findAll("input"))
    expect((input.element as HTMLInputElement).value).toBe("");
  expect(request).toHaveBeenCalledTimes(1);
  wrapper.unmount();
});
