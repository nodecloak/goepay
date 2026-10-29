// @vitest-environment jsdom
import { it, expect, vi } from "vitest";
import { mount, flushPromises } from "@vue/test-utils";
import MerchantBindings134 from "../src/components/MerchantBindings134.vue";
import { ApiClient } from "../src/api";

// KitModal teleports; stub it so text()/html() do not recurse.
const KitModalStub = {
  props: ["title", "loading", "submitText"],
  emits: ["close", "submit"],
  template: `<div class="modal-stub" :aria-label="title"><form @submit.prevent="$emit('submit')"><slot/><button type="submit">ok</button></form></div>`,
};

function setup(state: any) {
  const client = new ApiClient("merchant");
  client.csrfToken = "bind-session";
  const calls: any[] = [];
  let current = state;
  client.request = vi.fn(async (path: string, options?: any): Promise<any> => {
    calls.push({ path, method: options?.method || "GET", body: options?.body });
    if (path === "/account/bindings") return current;
    if (path === "/account/bindings/mobile/send-code") return null;
    if (path === "/account/bindings/mobile") {
      current = { ...current, mobile: "138****8000", mobile_bound: true };
      return null;
    }
    if (path.endsWith("/start")) {
      return {
        authorize_url: "https://open.example.invalid/authorize?state=s",
      };
    }
    if (path.startsWith("/account/bindings/connect/")) {
      current = {
        ...current,
        third_party: current.third_party.map((t: any) =>
          path.endsWith(t.provider) ? { ...t, bound: false } : t,
        ),
      };
      return null;
    }
    throw Error("unexpected " + path);
  }) as ApiClient["request"];
  const w = mount(MerchantBindings134, {
    props: { client },
    global: { stubs: { KitModal: KitModalStub } },
  });
  return { w, calls };
}

it("shows mobile + third-party bind state and unbinds a bound provider", async () => {
  const { w, calls } = setup({
    mobile: "",
    mobile_bound: false,
    sms_ready: true,
    third_party: [
      { provider: "qq", bound: true },
      { provider: "wx", bound: false },
      { provider: "alipay", bound: false },
      { provider: "telegram", bound: false },
    ],
  });
  await flushPromises();
  expect(w.text()).toContain("账号绑定");
  expect(w.text()).toContain("未绑定");
  expect(w.text()).toContain("QQ快捷登录");
  // qq is bound -> a 解绑 button exists
  const unbind = w.findAll("button").find((b) => b.text() === "解绑");
  expect(unbind).toBeTruthy();
  await unbind!.trigger("click");
  await flushPromises();
  expect(
    calls.some(
      (c) => c.method === "DELETE" && c.path === "/account/bindings/connect/qq",
    ),
  ).toBe(true);
  w.unmount();
});

it("sends a code and binds the mobile through the authenticated routes", async () => {
  const { w, calls } = setup({
    mobile: "",
    mobile_bound: false,
    sms_ready: true,
    third_party: [],
  });
  await flushPromises();
  await w
    .findAll("button")
    .find((b) => b.text() === "修改绑定")!
    .trigger("click");
  await flushPromises();
  const dialog = w.get(".modal-stub");
  const inputs = dialog.findAll("input");
  await inputs[0]!.setValue("13800138000"); // phone
  await dialog
    .findAll("button")
    .find((b) => b.text() === "获取验证码")!
    .trigger("click");
  await flushPromises();
  expect(
    calls.some((c) => c.path === "/account/bindings/mobile/send-code"),
  ).toBe(true);
  await inputs[1]!.setValue("654321"); // code
  await dialog.get("form").trigger("submit");
  await flushPromises();
  const bind = calls.find((c) => c.path === "/account/bindings/mobile");
  expect(bind.method).toBe("POST");
  expect(bind.body).toEqual({ phone: "13800138000", code: "654321" });
  w.unmount();
});

it("disables mobile 修改绑定 when the SMS gateway is not ready", async () => {
  const { w } = setup({
    mobile: "",
    mobile_bound: false,
    sms_ready: false,
    third_party: [],
  });
  await flushPromises();
  expect(w.findAll("button").some((b) => b.text() === "修改绑定")).toBe(false);
  expect(w.text()).toContain("短信网关未开启");
  w.unmount();
});

it("starts the OAuth bind for an unbound provider and navigates to the authorize url", async () => {
  const { w, calls } = setup({
    mobile: "",
    mobile_bound: false,
    sms_ready: false,
    third_party: [{ provider: "qq", bound: false }],
  });
  await flushPromises();
  const nav: string[] = [];
  Object.defineProperty(window, "location", {
    configurable: true,
    value: {
      set href(v: string) {
        nav.push(v);
      },
      get href() {
        return "";
      },
    },
  });
  await w
    .findAll("button")
    .find((b) => b.text() === "立即绑定")!
    .trigger("click");
  await flushPromises();
  expect(
    calls.some((c) => c.path === "/account/bindings/connect/qq/start"),
  ).toBe(true);
  expect(nav).toContain("https://open.example.invalid/authorize?state=s");
  w.unmount();
});
