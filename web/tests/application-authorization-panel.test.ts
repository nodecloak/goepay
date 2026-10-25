import { afterEach, expect, it, vi } from "vitest";
import { flushPromises, mount } from "@vue/test-utils";
import { ApiClient } from "../src/api";
import Panel from "../src/components/ApplicationAuthorizationPanel.vue";
import IdentityLookupPage from "../src/components/IdentityLookupPage.vue";

const profile = {
  id: "21",
  revision: "2",
  issuer_app_id: "2021000000000001",
  expected_auth_app_id: "2021000000000002",
  launch_kind: "basic",
  app_types: [],
};
const requestID = "a".repeat(64),
  authorizationID = "b".repeat(64);
const metadata = (
  status = "pending",
  expires = new Date(Date.now() + 120000).toISOString(),
) => ({
  request_id: requestID,
  purpose: "alipay_app_authorization",
  merchant_id: "1000",
  profile_id: "21",
  profile_revision: "2",
  issuer_app_id: profile.issuer_app_id,
  expected_auth_app_id: profile.expected_auth_app_id,
  launch_kind: "basic",
  app_types: null,
  status,
  expires_at: expires,
});
const caps = {
  merchant_id: "1000",
  available: true,
  reason: "",
  authorization_origin: "https://pay.example.invalid",
  profiles: [profile],
};
const result = {
  provider: "alipay",
  issuer_app_id: profile.issuer_app_id,
  auth_app_id: profile.expected_auth_app_id,
  user_id: "2088000000000000",
  app_auth_token: "synthetic-private-app-token",
  token_expires_in_seconds: "0",
};
const wrappers: ReturnType<typeof mount>[] = [];
afterEach(() => {
  wrappers.splice(0).forEach((w) => w.unmount());
  vi.useRealTimers();
});
function mountPanel(request: (...args: any[]) => any, props = {}) {
  const w = mount(Panel, {
    props: {
      client: Object.assign(new ApiClient("admin"), { request }),
      area: "admin",
      ownSession: true,
      adminClass: "super",
      ...props,
    },
  });
  wrappers.push(w);
  return w;
}
async function select(w: ReturnType<typeof mountPanel>) {
  await w.get('[aria-label="应用授权商户ID"]').setValue("1000");
  await w.get('[data-action="app-capabilities"]').trigger("click");
  await flushPromises();
  await w.get('[aria-label="应用授权档案"]').setValue("21");
}
async function create(w: ReturnType<typeof mountPanel>) {
  await w.get('[data-action="app-review"]').trigger("click");
  await w.get('[data-action="app-confirm-create"]').trigger("click");
  await flushPromises();
}
function transport(
  options: { revealFailure?: boolean; expires?: string } = {},
) {
  const expiry = options.expires || new Date(Date.now() + 120000).toISOString();
  return vi.fn(async (path: string, input?: { method?: string }) => {
    if (path.includes("/capabilities")) return caps;
    if (path.endsWith("/reveal")) {
      if (options.revealFailure) throw Error("secret-provider-reply");
      return result;
    }
    if (input?.method === "POST")
      return {
        request: metadata("pending", expiry),
        authorization_url: `https://pay.example.invalid/identity/application/authorize/${authorizationID}`,
      };
    return metadata("verified", expiry);
  });
}
it("is lazy, super-only, and exposes honest default-off capability", async () => {
  for (const props of [
    { area: "merchant" },
    { adminClass: "operator" },
    { ownSession: false },
    { adminClass: undefined },
  ]) {
    const call = transport(),
      w = mountPanel(call, props);
    await flushPromises();
    expect(call).not.toHaveBeenCalled();
    expect(w.find('[data-action="app-capabilities"]').exists()).toBe(false);
  }
  const call = vi.fn(async () => ({
    ...caps,
    available: false,
    profiles: [],
    authorization_origin: "",
    reason: "application authorization is disabled",
  }));
  const w = mountPanel(call);
  expect(call).not.toHaveBeenCalled();
  await w.get('[aria-label="应用授权商户ID"]').setValue("1000");
  await w.get('[data-action="app-capabilities"]').trigger("click");
  await flushPromises();
  expect(w.text()).toContain("application authorization is disabled");
  expect(
    w.get('[data-action="app-review"]').attributes("disabled"),
  ).toBeDefined();
});
it("requires explicit confirmation, creates exact app-auth body and manually reveals once in a masked memory field", async () => {
  const call = transport(),
    w = mountPanel(call);
  await select(w);
  await w.get('[data-action="app-review"]').trigger("click");
  expect(call).toHaveBeenCalledTimes(1);
  expect(w.get('[role="dialog"]').text()).toContain(
    profile.expected_auth_app_id,
  );
  await w.get('[data-action="app-confirm-create"]').trigger("click");
  await flushPromises();
  expect(call).toHaveBeenCalledWith(
    "/application-authorizations",
    expect.objectContaining({
      method: "POST",
      body: { merchant_id: "1000", profile_id: "21", profile_revision: "2" },
    }),
  );
  expect(w.get('[data-action="app-launch"]').attributes("href")).toContain(
    "/identity/application/authorize/",
  );
  expect(w.find('[data-action="app-reveal"]').exists()).toBe(false);
  await w.get('[data-action="app-query"]').trigger("click");
  await flushPromises();
  await w.get('[data-action="app-reveal"]').trigger("click");
  await flushPromises();
  const field = w.get('[aria-label="应用授权Token"]');
  expect(field.attributes("type")).toBe("password");
  expect((field.element as HTMLInputElement).value).toBe(result.app_auth_token);
  expect(w.text()).not.toContain(result.app_auth_token);
  expect(w.text()).toContain("厂商有效期未知");
  expect(w.find('[data-action="app-reveal"]').exists()).toBe(false);
  expect(localStorage.length + sessionStorage.length).toBe(0);
  await w.get('[data-action="app-clear-token"]').trigger("click");
  expect(w.find('[aria-label="应用授权Token"]').exists()).toBe(false);
});
it("locks unknown creation until explicit reset and never accepts late context responses", async () => {
  let resolve!: (data: unknown) => void;
  const call = vi.fn(async (path: string) =>
    path.includes("capabilities")
      ? caps
      : new Promise((r) => {
          resolve = r;
        }),
  );
  const w = mountPanel(call);
  await select(w);
  await w.get('[data-action="app-review"]').trigger("click");
  await w.get('[data-action="app-confirm-create"]').trigger("click");
  await w.get('[aria-label="应用授权商户ID"]').setValue("1001");
  resolve({
    request: metadata(),
    authorization_url: `https://pay.example.invalid/identity/application/authorize/${authorizationID}`,
  });
  await flushPromises();
  expect(w.find('[data-action="app-launch"]').exists()).toBe(false);
  expect(w.text()).toContain("创建结果不确定");
  await w.get('[aria-label="应用授权商户ID"]').setValue("1000");
  await w.get('[data-action="app-capabilities"]').trigger("click");
  await flushPromises();
  await w.get('[aria-label="应用授权档案"]').setValue("21");
  expect(
    w.get('[data-action="app-review"]').attributes("disabled"),
  ).toBeDefined();
  await w.get('[data-action="app-reset"]').trigger("click");
  expect(w.text()).not.toContain("创建结果不确定");
});
it("treats failed reveal as consumed even if a subsequent metadata query says verified", async () => {
  const call = transport({ revealFailure: true }),
    w = mountPanel(call);
  await select(w);
  await create(w);
  await w.get('[data-action="app-query"]').trigger("click");
  await flushPromises();
  await w.get('[data-action="app-reveal"]').trigger("click");
  await flushPromises();
  expect(w.text()).toContain("读取结果不确定");
  expect(w.text()).not.toContain("secret-provider-reply");
  await w.get('[data-action="app-query"]').trigger("click");
  await flushPromises();
  expect(w.find('[data-action="app-reveal"]').exists()).toBe(false);
  expect(
    call.mock.calls.filter(([path]) => path.endsWith("/reveal")),
  ).toHaveLength(1);
});
it("clears credentials on expiry and privilege change, without automatic polling", async () => {
  vi.useFakeTimers();
  const expiry = new Date(Date.now() + 3000).toISOString();
  const call = transport({ expires: expiry }),
    w = mountPanel(call);
  await select(w);
  await create(w);
  await w.get('[data-action="app-query"]').trigger("click");
  await flushPromises();
  await w.get('[data-action="app-reveal"]').trigger("click");
  await flushPromises();
  expect(w.find('[aria-label="应用授权Token"]').exists()).toBe(true);
  const before = call.mock.calls.length;
  vi.advanceTimersByTime(4000);
  await flushPromises();
  expect(call).toHaveBeenCalledTimes(before);
  expect(w.find('[aria-label="应用授权Token"]').exists()).toBe(false);
  expect(w.text()).toContain("已过期");
  await w.setProps({ ownSession: false });
  expect(w.find('[data-action="app-query"]').exists()).toBe(false);
});
it("integrates a separate super-only tab without replacing existing identity flows", async () => {
  for (const adminClass of ["super", "operator", undefined]) {
    const call = transport(),
      w = mount(IdentityLookupPage, {
        props: {
          client: Object.assign(new ApiClient("admin"), { request: call }),
          area: "admin",
          ownSession: true,
          adminClass,
        },
      });
    wrappers.push(w);
    const buttons = w.findAll("button");
    expect(buttons.some((b) => b.text() === "原收款应用网页授权")).toBe(true);
    expect(buttons.some((b) => b.text() === "独立应用与小程序")).toBe(true);
    expect(buttons.some((b) => b.text() === "支付宝应用授权")).toBe(
      adminClass === "super",
    );
  }
});
it("shows only the selected assigned-app launch and never adds application types to the create body", async () => {
  const p = {
    ...profile,
    launch_kind: "assigned",
    app_types: ["WEBAPP", "TINYAPP"],
  };
  const call = vi.fn(async (path: string) =>
    path.includes("capabilities")
      ? { ...caps, profiles: [p] }
      : {
          request: {
            ...metadata(),
            launch_kind: "assigned",
            app_types: ["WEBAPP", "TINYAPP"],
          },
          authorization_url: `https://pay.example.invalid/identity/application/authorize/${authorizationID}`,
          wallet_authorization_url: `https://pay.example.invalid/identity/application/wallet/${authorizationID}`,
        },
  );
  const w = mountPanel(call);
  await select(w);
  await w.get('[aria-label="应用授权打开方式"]').setValue("wallet");
  await create(w);
  expect(w.findAll("a")).toHaveLength(1);
  expect(w.get('[data-action="app-launch"]').attributes("href")).toContain(
    "/identity/application/wallet/",
  );
  expect(call).toHaveBeenCalledWith(
    "/application-authorizations",
    expect.objectContaining({
      body: { merchant_id: "1000", profile_id: "21", profile_revision: "2" },
    }),
  );
});
it("does not echo or retry an unknown create failure", async () => {
  const call = vi.fn(async (path: string) => {
    if (path.includes("capabilities")) return caps;
    throw Error("private-upstream-token");
  });
  const w = mountPanel(call);
  await select(w);
  await create(w);
  expect(w.text()).toContain("创建结果不确定");
  expect(w.text()).not.toContain("private-upstream-token");
  await w.get('[data-action="app-review"]').trigger("click");
  await flushPromises();
  expect(
    call.mock.calls.filter(([path]) => path === "/application-authorizations"),
  ).toHaveLength(1);
});
it("clears the masked credential immediately on each merchant, profile, client or privilege change", async () => {
  for (const change of ["merchant", "profile", "client", "privilege"]) {
    const call = transport(),
      w = mountPanel(call);
    await select(w);
    await create(w);
    await w.get('[data-action="app-query"]').trigger("click");
    await flushPromises();
    await w.get('[data-action="app-reveal"]').trigger("click");
    await flushPromises();
    expect(w.find('[aria-label="应用授权Token"]').exists()).toBe(true);
    if (change === "merchant")
      await w.get('[aria-label="应用授权商户ID"]').setValue("1001");
    if (change === "profile") {
      (
        w.get('[aria-label="应用授权档案"]').element as HTMLSelectElement
      ).disabled = false;
      await w.get('[aria-label="应用授权档案"]').setValue("");
    }
    if (change === "client")
      await w.setProps({ client: new ApiClient("admin") });
    if (change === "privilege") await w.setProps({ ownSession: false });
    expect(w.find('[aria-label="应用授权Token"]').exists()).toBe(false);
  }
});
