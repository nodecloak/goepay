// @vitest-environment jsdom
import { afterEach, expect, it, vi } from "vitest";
import { mount, flushPromises } from "@vue/test-utils";
import PublicEnrollment from "../src/components/PublicEnrollment.vue";

type Call = { path: string; body: unknown };
const mounted: ReturnType<typeof mount>[] = [];
afterEach(() => {
  for (const w of mounted.splice(0)) w.unmount();
  document.body.innerHTML = "";
  vi.restoreAllMocks();
  vi.unstubAllGlobals();
});

function ok(data: unknown) {
  return new Response(JSON.stringify({ code: 0, message: "", data }), {
    status: 200,
  });
}
const caps = (sms: boolean) => ({
  review_required: false,
  registration_open: true,
  email_enabled: true,
  sms_enabled: sms,
  invite_required: false,
  expires_seconds: 600,
});

async function setup(sms: boolean, purpose: "register" | "recover") {
  const calls: Call[] = [];
  vi.stubGlobal(
    "fetch",
    vi.fn(async (input: string | URL | Request, init?: RequestInit) => {
      const path = String(input);
      const body = init?.body ? JSON.parse(String(init.body)) : undefined;
      calls.push({ path, body });
      if (path.endsWith("/public/capabilities")) return ok(caps(sms));
      if (path.endsWith("/public/sms-code")) return ok(null);
      if (path.endsWith("/public/register-phone"))
        return ok({ merchant_id: "5001", username: "phoneuser", pay: "1" });
      if (path.endsWith("/public/recover-password-phone")) return ok(null);
      throw new Error("unexpected " + path);
    }),
  );
  const w = mount(PublicEnrollment, {
    attachTo: document.body,
    props: { purpose },
  });
  mounted.push(w);
  await flushPromises();
  return { w, calls };
}

it("hides the phone channel when the SMS gateway is off", async () => {
  const { w } = await setup(false, "register");
  expect(w.text()).not.toContain("手机验证");
  expect(w.find('[name="enrollment-phone"]').exists()).toBe(false);
  // The email field is present and unchanged.
  expect(w.find('[name="enrollment-email"]').exists()).toBe(true);
});

it("offers phone register when SMS is on: sends via sms-code and submits to register-phone", async () => {
  const { w, calls } = await setup(true, "register");
  expect(w.text()).toContain("手机验证");
  // Switch to phone.
  await w
    .findAll("button")
    .find((b) => b.text() === "手机验证")!
    .trigger("click");
  await flushPromises();
  expect(w.find('[name="enrollment-phone"]').exists()).toBe(true);
  await w.get('[name="enrollment-phone"]').setValue("13800138000");
  await w
    .findAll("button")
    .find((b) => b.text() === "发送验证码")!
    .trigger("click");
  await flushPromises();
  const sms = calls.find((c) => c.path.endsWith("/public/sms-code"));
  expect(sms).toBeDefined();
  expect(sms!.body).toMatchObject({
    purpose: "register",
    phone: "13800138000",
  });
  // Fill the rest and submit. The code field takes 6 digits for phone.
  const inputs = w.findAll("input");
  await w.get('input[autocomplete="username"]').setValue("phoneuser");
  await w.get('input[autocomplete="organization"]').setValue("Phone Shop");
  await w.get('input[autocomplete="one-time-code"]').setValue("246810");
  const pwInputs = w.findAll('input[autocomplete="new-password"]');
  await pwInputs[0].setValue("a-strong-password-1");
  await pwInputs[1].setValue("a-strong-password-1");
  await w.get("form").trigger("submit");
  await flushPromises();
  const reg = calls.find((c) => c.path.endsWith("/public/register-phone"));
  expect(reg).toBeDefined();
  expect(reg!.body).toMatchObject({
    phone: "13800138000",
    code: "246810",
    username: "phoneuser",
    name: "Phone Shop",
    password: "a-strong-password-1",
  });
  // It never posted to the email register endpoint.
  expect(calls.some((c) => c.path.endsWith("/public/register"))).toBe(false);
  expect(inputs.length).toBeGreaterThan(0);
});

it("offers phone find-password and submits to recover-password-phone", async () => {
  const { w, calls } = await setup(true, "recover");
  await w
    .findAll("button")
    .find((b) => b.text() === "手机验证")!
    .trigger("click");
  await flushPromises();
  await w.get('[name="enrollment-phone"]').setValue("13800138111");
  await w
    .findAll("button")
    .find((b) => b.text() === "发送验证码")!
    .trigger("click");
  await flushPromises();
  await w.get('input[autocomplete="one-time-code"]').setValue("135791");
  const pw = w.findAll('input[autocomplete="new-password"]');
  await pw[0].setValue("another-strong-pass-9");
  await pw[1].setValue("another-strong-pass-9");
  await w.get("form").trigger("submit");
  await flushPromises();
  const rec = calls.find((c) =>
    c.path.endsWith("/public/recover-password-phone"),
  );
  expect(rec).toBeDefined();
  expect(rec!.body).toMatchObject({
    phone: "13800138111",
    code: "135791",
    new_password: "another-strong-pass-9",
  });
  expect(calls.some((c) => c.path.endsWith("/public/recover-password"))).toBe(
    false,
  );
});
