// @vitest-environment jsdom
import { it, expect, vi, afterEach } from "vitest";
import { mount, flushPromises } from "@vue/test-utils";
import MerchantBalanceAdjustDialog from "../src/components/MerchantBalanceAdjustDialog.vue";
import FundsAuthorizationModal from "../src/components/FundsAuthorizationModal.vue";
import KitRadio from "../src/components/KitRadio.vue";
import { ApiClient } from "../src/api";
import { isAdminFundsAction } from "../src/funds-authorization";

afterEach(() => {
  document.body.innerHTML = "";
});

// KitModal teleports to <body>, so query the live DOM for its inputs.
function fill(selector: string, value: string) {
  const el = document.querySelector<HTMLInputElement>(selector);
  if (!el) throw new Error("missing input " + selector);
  el.value = value;
  el.dispatchEvent(new Event("input", { bubbles: true }));
}

it("the admin 后台加款/扣款 POST is routed through funds authorization", () => {
  expect(isAdminFundsAction("/workflows/balance-adjustments", "POST")).toBe(
    true,
  );
  expect(isAdminFundsAction("/workflows/balance-adjustments", "GET")).toBe(
    false,
  );
});

it("the dialog collects direction/amount/reason and hands a signed body to funds authorization", async () => {
  const client = new ApiClient("admin");
  vi.spyOn(client, "request").mockResolvedValue({
    challenge_id: "c1",
    expires_at: new Date(Date.now() + 60000).toISOString(),
    summary: {},
  } as any);
  const wrapper = mount(MerchantBalanceAdjustDialog, {
    props: { client, merchantID: "1000" },
  });
  // credit + debit radios present (Pro's dialog); funds-auth not shown yet.
  expect(wrapper.findAllComponents(KitRadio).length).toBe(2);
  expect(wrapper.findComponent(FundsAuthorizationModal).exists()).toBe(false);

  await wrapper.findAllComponents(KitRadio)[1]!.vm.$emit("change", "debit");
  await wrapper
    .findAllComponents(KitRadio)[1]!
    .vm.$emit("update:modelValue", "debit");
  fill('input[inputmode="decimal"]', "12.34");
  fill('input[maxlength="240"]', "错误入账回收");
  await flushPromises();
  (document.querySelector("form") as HTMLFormElement).dispatchEvent(
    new Event("submit", { bubbles: true, cancelable: true }),
  );
  await flushPromises();

  const auth = wrapper.findComponent(FundsAuthorizationModal);
  expect(auth.exists()).toBe(true);
  expect(auth.props("target")).toBe(
    "/console/v1/admin/workflows/balance-adjustments",
  );
  const body = auth.props("body") as Record<string, unknown>;
  expect(body.merchant_id).toBe("1000");
  expect(body.direction).toBe("debit");
  expect(body.amount).toBe("12.34");
  expect(body.reason).toBe("错误入账回收");
  expect(String(body.external_no)).toMatch(/^adj-1000-/);
  wrapper.unmount();
});

it("rejects a non-positive amount before authorization", async () => {
  const client = new ApiClient("admin");
  const wrapper = mount(MerchantBalanceAdjustDialog, {
    props: { client, merchantID: "7" },
  });
  fill('input[inputmode="decimal"]', "0");
  fill('input[maxlength="240"]', "x");
  await flushPromises();
  (document.querySelector("form") as HTMLFormElement).dispatchEvent(
    new Event("submit", { bubbles: true, cancelable: true }),
  );
  await flushPromises();
  expect(wrapper.findComponent(FundsAuthorizationModal).exists()).toBe(false);
  expect(document.querySelector(".alert.error")).not.toBeNull();
  wrapper.unmount();
});
