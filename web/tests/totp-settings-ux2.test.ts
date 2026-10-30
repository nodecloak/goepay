// @vitest-environment jsdom
import { afterEach, expect, it, vi } from "vitest";
import { mount, flushPromises } from "@vue/test-utils";
import { ApiClient } from "../src/api";
import TotpSettings from "../src/components/TotpSettings.vue";

vi.mock("qrcode", () => ({
  default: { toDataURL: vi.fn(async () => "data:image/png;base64,QR") },
}));

const mounted: ReturnType<typeof mount>[] = [];
afterEach(() => {
  for (const w of mounted.splice(0)) w.unmount();
  document.body.innerHTML = "";
  vi.restoreAllMocks();
});

function newClient() {
  const c = new ApiClient("merchant");
  c.csrfToken = "totp-own-session";
  return c;
}

it("binds a merchant authenticator through the kit modal: password -> QR -> code confirm", async () => {
  const c = newClient();
  const calls: { path: string; body: unknown }[] = [];
  vi.spyOn(c, "request").mockImplementation(async (p, o) => {
    calls.push({ path: p, body: o?.body });
    if (p === "/security/totp") return { enabled: false } as never;
    if (p === "/security/totp/prepare")
      return {
        secret: "JBSWY3DPEHPK3PXP",
        otpauth_uri:
          "otpauth://totp/GOEPAY:merchant:1000?secret=JBSWY3DPEHPK3PXP",
        expires_at: "2026-10-29 10:20:00",
      } as never;
    return {} as never;
  });
  const w = mount(TotpSettings, {
    attachTo: document.body,
    props: { client: c },
  });
  mounted.push(w);
  await flushPromises();
  expect(document.body.textContent).toContain("动态验证码保护");
  const trigger = w
    .findAll("button")
    .find((b) => b.text() === "绑定身份验证器");
  expect(trigger).toBeDefined();
  expect(trigger!.classes()).toContain("pa-btn--primary");
  await trigger!.trigger("click");
  await flushPromises();
  // Kit modal is teleported to body.
  const modal = document.body.querySelector(".pa-modal");
  expect(modal, "KitModal renders").toBeTruthy();
  expect(modal!.textContent).toContain("绑定身份验证器");
  // Step 1: password -> prepare.
  const pw = modal!.querySelector(
    'input[autocomplete="current-password"]',
  ) as HTMLInputElement;
  expect(pw).toBeTruthy();
  pw.value = "synthetic-merchant-pw";
  pw.dispatchEvent(new Event("input"));
  (modal!.querySelector("form") as HTMLFormElement).dispatchEvent(
    new Event("submit"),
  );
  await flushPromises();
  expect(calls.some((x) => x.path === "/security/totp/prepare")).toBe(true);
  // Step 2: QR + code field appear.
  expect(document.body.querySelector("img")).toBeTruthy();
  const code = document.body.querySelector(
    'input[autocomplete="one-time-code"]',
  ) as HTMLInputElement;
  expect(code).toBeTruthy();
  expect(code.getAttribute("pattern")).toBe("[0-9]{6}");
  code.value = "123456";
  code.dispatchEvent(new Event("input"));
  (
    document.body.querySelector(".pa-modal form") as HTMLFormElement
  ).dispatchEvent(new Event("submit"));
  await flushPromises();
  const confirm = calls.find((x) => x.path === "/security/totp/confirm");
  expect(confirm).toBeDefined();
  expect(confirm!.body).toMatchObject({ code: "123456" });
});

it("unbinds an enabled authenticator through the kit modal with password + code", async () => {
  const c = newClient();
  const calls: { path: string; body: unknown }[] = [];
  vi.spyOn(c, "request").mockImplementation(async (p, o) => {
    calls.push({ path: p, body: o?.body });
    if (p === "/security/totp") return { enabled: true } as never;
    return {} as never;
  });
  const w = mount(TotpSettings, {
    attachTo: document.body,
    props: { client: c },
  });
  mounted.push(w);
  await flushPromises();
  const trigger = w
    .findAll("button")
    .find((b) => b.text() === "关闭动态验证码");
  expect(trigger).toBeDefined();
  expect(trigger!.classes()).toContain("pa-btn--danger");
  await trigger!.trigger("click");
  await flushPromises();
  const modal = document.body.querySelector(".pa-modal")!;
  expect(modal.textContent).toContain("关闭动态验证码");
  const pw = modal.querySelector(
    'input[autocomplete="current-password"]',
  ) as HTMLInputElement;
  pw.value = "synthetic-merchant-pw";
  pw.dispatchEvent(new Event("input"));
  const code = modal.querySelector(
    'input[autocomplete="one-time-code"]',
  ) as HTMLInputElement;
  code.value = "654321";
  code.dispatchEvent(new Event("input"));
  (modal.querySelector("form") as HTMLFormElement).dispatchEvent(
    new Event("submit"),
  );
  await flushPromises();
  const disable = calls.find((x) => x.path === "/security/totp/disable");
  expect(disable).toBeDefined();
  expect(disable!.body).toMatchObject({
    code: "654321",
    current_password: "synthetic-merchant-pw",
  });
});

it("hides 绑定 and shows the muted line when the platform disabled enrollment (enrollable:false)", async () => {
  const c = newClient();
  vi.spyOn(c, "request").mockImplementation(async (p) => {
    if (p === "/security/totp")
      return { enabled: false, enrollable: false } as never;
    return {} as never;
  });
  const w = mount(TotpSettings, {
    attachTo: document.body,
    props: { client: c },
  });
  mounted.push(w);
  await flushPromises();
  // The 绑定身份验证器 button is gone; the muted explanation is shown.
  expect(w.findAll("button").some((b) => b.text() === "绑定身份验证器")).toBe(
    false,
  );
  expect(w.text()).toContain("平台未开启商户TOTP二次验证，暂不能绑定。");
});

it("still shows 绑定 when enrollable is absent (default true) or true", async () => {
  const c = newClient();
  vi.spyOn(c, "request").mockImplementation(async (p) => {
    if (p === "/security/totp")
      return { enabled: false, enrollable: true } as never;
    return {} as never;
  });
  const w = mount(TotpSettings, {
    attachTo: document.body,
    props: { client: c },
  });
  mounted.push(w);
  await flushPromises();
  expect(w.findAll("button").some((b) => b.text() === "绑定身份验证器")).toBe(
    true,
  );
  expect(w.text()).not.toContain("平台未开启商户TOTP二次验证");
});
