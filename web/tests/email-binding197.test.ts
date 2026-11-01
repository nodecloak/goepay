// @vitest-environment jsdom
import { it, expect, vi } from "vitest";
import { mount, flushPromises } from "@vue/test-utils";
import AccountPage from "../src/components/AccountPage.vue";
import { ApiClient, ApiError } from "../src/api";

async function setup(verified = true) {
  const client = new ApiClient("merchant");
  client.csrfToken = "email197-session";
  let flow: Record<string, unknown> | undefined;
  const writes: Array<{ path: string; body: any }> = [];
  const request = vi.fn(async (path: string, options?: any): Promise<any> => {
    if (path === "/account/profile")
      return {
        name: "本地商户",
        email: verified ? "old197@example.test" : "legacy-display@example.test",
        pay: "1",
      };
    if (path === "/account/email" && options?.method !== "POST")
      return {
        email: verified ? "old197@example.test" : "",
        verified,
        email_enabled: true,
        requires: verified ? "email" : "password",
        fingerprint: "a".repeat(64),
        email_interval: 60,
      };
    if (options?.method === "POST") {
      writes.push({ path, body: structuredClone(options.body) });
      if (path === "/account/email")
        flow = {
          flow_id: options.body.flow_id,
          phase: verified ? "old" : "new",
          original_email: verified ? "old197@example.test" : "",
          email: "",
          delivery: "",
          expires_at: new Date(Date.now() + 600000).toISOString(),
          reauthenticate: false,
        };
      if (path.endsWith("/old-code"))
        flow = {
          ...flow,
          delivery: "sent",
          old_challenge_id: "o".repeat(43),
          send_id: options.body.send_id,
          sent_at: new Date().toISOString(),
        };
      if (path.endsWith("/old-verify"))
        flow = {
          ...flow,
          phase: "new",
          delivery: "",
          send_id: undefined,
          sent_at: undefined,
        };
      if (path.endsWith("/new-code"))
        flow = {
          ...flow,
          email: options.body.email,
          delivery: "sent",
          send_id: options.body.send_id,
          sent_at: new Date().toISOString(),
        };
      if (path.endsWith("/complete"))
        flow = { ...flow, phase: "completed", reauthenticate: true };
      return flow;
    }
    if (path.startsWith("/account/email/") && options?.method === "DELETE") {
      flow = { ...flow, phase: "closed" };
      return flow;
    }
    if (path.startsWith("/account/email/")) return flow;
    return {};
  });
  client.request = request as ApiClient["request"];
  const wrapper = mount(AccountPage, {
    props: { client, section: "profile" },
    attachTo: document.body,
  });
  await flushPromises();
  return { wrapper, client, writes, request };
}
it("shows original binding row and verifies old address before asking for a new target", async () => {
  const { wrapper: w, writes } = await setup();
  expect(w.text()).toContain("绑定邮箱");
  await w.get('[data-email197="change"]').trigger("click");
  await flushPromises();
  const old = w.get('[role="dialog"][aria-label="安全验证"]');
  expect(
    old
      .findAll<HTMLInputElement>("input[readonly]")
      .map((x) => x.element.value),
  ).toContain("old197@example.test");
  expect(w.find('input[autocomplete="email"]').exists()).toBe(false);
  await old.get('[data-email197="send"]').trigger("click");
  await flushPromises();
  expect(old.get('#email197-code').attributes('aria-label')).toBe('验证码');
  await old.get('input[autocomplete="one-time-code"]').setValue("1234567");
  await old.get("form").trigger("submit");
  await flushPromises();
  const next = w.get('[role="dialog"][aria-label="修改绑定邮箱"]');
  await next.get('input[type="email"]').setValue("new197@example.test");
  await next.get('[data-email197="send"]').trigger("click");
  await flushPromises();
  await next.get('input[autocomplete="one-time-code"]').setValue("2345678");
  await next.get("form").trigger("submit");
  await flushPromises();
  expect(writes.map((x) => x.path.split("/").at(-1))).toEqual([
    "email",
    "old-code",
    "old-verify",
    "new-code",
    "complete",
  ]);
  expect(w.emitted("reauthenticate")).toHaveLength(1);
  expect(w.emitted("reauthenticate")?.[0]).toEqual([
    "修改绑定成功，请重新登录",
  ]);
  expect(w.find('[role="dialog"]').exists()).toBe(false);
  w.unmount();
});
it("queries a lost send response before enabling an explicit resend with a fresh send identity", async () => {
  vi.useFakeTimers({ toFake: ["Date", "setInterval", "clearInterval"] });
  const { wrapper: w, client, writes, request } = await setup();
  try {
    const send = request.getMockImplementation()!;
    let lost = true;
    client.request = vi.fn(async (path: string, options?: any) => {
      const result = await send(path, options);
      if (path.endsWith("/old-code") && lost) {
        lost = false;
        throw Error("synthetic reply lost");
      }
      return result;
    }) as ApiClient["request"];
    await w.get('[data-email197="change"]').trigger("click");
    await flushPromises();
    await w.get('[data-email197="send"]').trigger("click");
    await flushPromises();
    expect(w.get('[role="dialog"]').text()).toContain("请先查询原验证流程");
    expect(
      w.get('[data-email197="send"]').attributes("disabled"),
    ).toBeDefined();
    await w
      .findAll("button")
      .find((b) => b.text() === "查询验证状态")!
      .trigger("click");
    await flushPromises();
    expect(writes.filter((x) => x.path.endsWith("/old-code"))).toHaveLength(1);
    await vi.advanceTimersByTimeAsync(61000);
    await flushPromises();
    await w.get('[data-email197="send"]').trigger("click");
    await flushPromises();
    const attempts = writes.filter((x) => x.path.endsWith("/old-code"));
    expect(attempts).toHaveLength(2);
    expect(attempts[1].body.send_id).not.toBe(attempts[0].body.send_id);
  } finally {
    w.unmount();
    vi.useRealTimers();
  }
});
it("first binding confirms the current password and never promotes the legacy display email", async () => {
  const { wrapper: w, writes } = await setup(false);
  await w.get('[data-email197="change"]').trigger("click");
  await flushPromises();
  const dialog = w.get('[role="dialog"]');
  expect(dialog.attributes("aria-label")).toBe("安全验证");
  expect(dialog.find('input[type="email"]').exists()).toBe(false);
  await dialog
    .get('input[autocomplete="current-password"]')
    .setValue("current-secret197");
  await dialog.get("form").trigger("submit");
  await flushPromises();
  expect(writes[0].body.current_password).toBe("current-secret197");
  expect(w.get('[role="dialog"]').attributes("aria-label")).toBe(
    "修改绑定邮箱",
  );
  expect(w.get<HTMLInputElement>('input[type="email"]').element.value).toBe("");
  w.unmount();
});
it("clears the original flow and secrets immediately when the merchant session changes", async () => {
  const { wrapper: w, client } = await setup(false);
  await w.get('[data-email197="change"]').trigger("click");
  await flushPromises();
  await w
    .get('[role="dialog"]')
    .get('input[type="password"]')
    .setValue("never-retain197");
  const replacement = new ApiClient("merchant");
  replacement.csrfToken = "replacement197";
  replacement.request = client.request;
  await w.setProps({ client: replacement });
  await flushPromises();
  expect(w.find('[role="dialog"]').exists()).toBe(false);
  expect(w.html()).not.toContain("never-retain197");
  w.unmount();
});
it("allows local dismissal when the original server confirms the flow has expired", async () => {
  vi.useFakeTimers({ toFake: ["Date", "setInterval", "clearInterval"] });
  const { wrapper: w, client, request } = await setup();
  try {
    const send = request.getMockImplementation()!;
    client.request = vi.fn(async (path: string, options?: any) => {
      if (options?.method === "DELETE")
        throw new ApiError("验证已过期", 422, -1);
      return send(path, options);
    }) as ApiClient["request"];
    await w.get('[data-email197="change"]').trigger("click");
    await flushPromises();
    await vi.advanceTimersByTimeAsync(601000);
    await flushPromises();
    expect(w.get('[role="dialog"]').text()).toContain("验证已过期");
    await w
      .get('[role="dialog"]')
      .findAll("button")
      .find((b) => b.text() === "取消")!
      .trigger("click");
    await flushPromises();
    expect(w.find('[role="dialog"]').exists()).toBe(false);
  } finally {
    w.unmount();
    vi.useRealTimers();
  }
});
