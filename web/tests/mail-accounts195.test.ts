import { it, expect, vi } from "vitest";
import { mount, flushPromises } from "@vue/test-utils";
import SettingsPage from "../src/components/SettingsPage.vue";
import { ApiClient } from "../src/api";

const account = () => ({
  name: "smtp",
  title: "SMTP发信",
  configured: false,
  status: 0,
  config: { host: "", port: "465", username: "" },
  password_set: false,
  revision: "0",
  fingerprint: "a".repeat(64),
  test_available: false,
  effective_source: "deployment",
});
async function setup(testEnabled = false) {
  const client = new ApiClient("admin");
  client.csrfToken = "mail195-session";
  const writes: any[] = [];
  let current = account();
  const request = vi.fn(async (path: string, options?: any): Promise<any> => {
    if (path === "/resources/settings/schema")
      return { fields: [], permissions: { create: true, update: true } };
    if (path.startsWith("/resources/settings?")) return { items: [], total: 0 };
    if (path === "/settings/mail-channels")
      return {
        items: [
          {
            name: "smtp",
            title: "SMTP发信",
            status: current.status,
            configured: current.configured,
          },
        ],
        total: 1,
      };
    if (path === "/settings/mail-channels/smtp") {
      if (options?.method === "PATCH") {
        writes.push(structuredClone(options.body));
        current = {
          ...current,
          status: options.body.status,
          config: {
            host: options.body.config.host,
            port: options.body.config.port,
            username: options.body.config.username,
          },
          password_set: true,
          configured: true,
          revision: "1",
          fingerprint: "b".repeat(64),
          effective_source: "console",
          test_available: testEnabled,
        };
        return current;
      }
      return current;
    }
    throw Error("independent setting unavailable");
  });
  client.request = request as ApiClient["request"];
  const w = mount(SettingsPage, {
    props: { client, adminClass: "super", ownSession: true },
  });
  await flushPromises();
  await w.get("#settings-tab-mail").trigger("click");
  await flushPromises();
  return { w, client, writes, request };
}
it("original mail channel row opens its SMTP account dialog, saves original four fields and never echoes password", async () => {
  const { w, writes } = await setup();
  expect(w.text()).toContain("邮箱渠道配置");
  expect(w.text()).toContain("SMTP发信");
  expect(w.text()).toContain("已关闭");
  await w
    .findAll("button")
    .find((b) => b.text() === "账号设置")!
    .trigger("click");
  await flushPromises();
  const dialog = w.get('[role="dialog"][aria-label="SMTP发信账号设置"]');
  for (const [label, value] of [
    ["SMTP服务器", "smtp.example.invalid"],
    ["SMTP端口", "465"],
    ["邮箱账号", "sender195@example.invalid"],
    ["邮箱密码", "synthetic195-private-password"],
  ])
    await dialog.get(`input[aria-label="${label}"]`).setValue(value);
  expect(dialog.get('input[aria-label="邮箱密码"]').attributes("type")).toBe(
    "password",
  );
  await dialog.get('input[aria-label="启用状态"]').setValue(true);
  await dialog.get("form").trigger("submit");
  await flushPromises();
  expect(writes).toEqual([
    {
      name: "smtp",
      status: 1,
      config: {
        host: "smtp.example.invalid",
        port: "465",
        username: "sender195@example.invalid",
        password: "synthetic195-private-password",
      },
      expected_fingerprint: "a".repeat(64),
    },
  ]);
  expect(w.find('[role="dialog"]').exists()).toBe(false);
  expect(w.html()).not.toContain("synthetic195-private-password");
  expect(w.text()).toContain("已开启");
  expect(w.text()).toContain("保存成功");
  w.unmount();
});

it("an original test email keeps one request after an unknown reply and reads its result without another POST", async () => {
  const { w, request } = await setup(true);
  await w
    .findAll("button")
    .find((b) => b.text() === "账号设置")!
    .trigger("click");
  await flushPromises();
  for (const [label, value] of [
    ["SMTP服务器", "smtp.example.invalid"],
    ["SMTP端口", "465"],
    ["邮箱账号", "sender195@example.invalid"],
    ["邮箱密码", "synthetic195-private-password"],
  ])
    await w.get(`input[aria-label="${label}"]`).setValue(value);
  await w.get('input[aria-label="启用状态"]').setValue(true);
  await w.get('[role="dialog"] form').trigger("submit");
  await flushPromises();
  let original = "";
  let posts = 0,
    reads = 0;
  request.mockImplementation(async (path, options) => {
    if (path === "/settings/mail-channels/smtp/test") {
      posts++;
      original = options.body.request_id;
      return { request_id: original, status: "unknown" };
    }
    if (path === "/settings/mail-channels/smtp/tests/" + original) {
      reads++;
      return { request_id: original, status: "accepted" };
    }
    throw Error("unexpected test action");
  });
  await w
    .findAll("button")
    .find((b) => b.text() === "发送测试邮件")!
    .trigger("click");
  await flushPromises();
  await w
    .get('input[aria-label="收件邮箱地址"]')
    .setValue("recipient195@example.test");
  await w.get('form[aria-label="发送测试邮件"]').trigger("submit");
  await flushPromises();
  expect(posts).toBe(1);
  expect(original).toMatch(/^[a-f0-9-]{36}$/);
  expect(w.text()).toContain("发送结果尚未确认");
  await w
    .findAll("button")
    .find((b) => b.text() === "查询原发送结果")!
    .trigger("click");
  await flushPromises();
  expect(reads).toBe(1);
  expect(posts).toBe(1);
  expect(w.text()).toContain("SMTP已接受");
  w.unmount();
});
it("cancel clears SMTP draft and same-client session rotation prevents its submission", async () => {
  const { w, client, writes } = await setup();
  await w
    .findAll("button")
    .find((b) => b.text() === "账号设置")!
    .trigger("click");
  await flushPromises();
  await w.get('input[aria-label="邮箱密码"]').setValue("synthetic195-discard");
  client.csrfToken = "rotated-session";
  await w.get('[role="dialog"] form').trigger("submit");
  await flushPromises();
  expect(writes).toEqual([]);
  expect(w.find('input[aria-label="邮箱密码"]').exists()).toBe(false);
  expect(w.text()).toContain("重新读取");
  w.unmount();
});
