// @vitest-environment jsdom
import { it, expect, vi } from "vitest";
import { mount, flushPromises } from "@vue/test-utils";
import SettingsPage from "../src/components/SettingsPage.vue";
import { ApiClient } from "../src/api";

// KitModal teleports to <body>; @vue/test-utils serialization recurses on a
// Teleport in the tree, so stub it with an inline passthrough that keeps the
// slot content queryable and exposes a submit button. The real component is
// exercised in the browser screenshots.
const KitModalStub = {
  props: ["title", "loading", "submitText", "cancelText", "danger"],
  emits: ["close", "submit"],
  template: `<div class="modal-stub" :aria-label="title">
    <h2>{{ title }}</h2>
    <form @submit.prevent="$emit('submit')"><slot /><button type="submit">{{ submitText || '保存' }}</button></form>
    <button type="button" class="modal-close" @click="$emit('close')">取消</button>
  </div>`,
};

function gateway(active = "", status = 0, aliyunConfigured = false) {
  return {
    providers: [
      {
        provider: "aliyun",
        configured: aliyunConfigured,
        template: aliyunConfigured ? "SMS_TEST_1" : "",
        sign: aliyunConfigured ? "测试签名" : "",
        principal: aliyunConfigured ? "LT****st" : "",
      },
      {
        provider: "qcloud",
        configured: false,
        template: "",
        sign: "",
        principal: "",
      },
      {
        provider: "smsbao",
        configured: false,
        template: "",
        sign: "",
        principal: "",
      },
    ],
    active,
    status,
    fingerprint: (aliyunConfigured ? "b" : "a").repeat(64),
  };
}

async function setup() {
  const client = new ApiClient("admin");
  client.csrfToken = "sms134-session";
  const writes: any[] = [];
  let current = gateway();
  const request = vi.fn(async (path: string, options?: any): Promise<any> => {
    if (path === "/resources/settings/schema")
      return { fields: [], permissions: { create: true, update: true } };
    if (path.startsWith("/resources/settings?")) return { items: [], total: 0 };
    if (path === "/settings/sms-channels") {
      if (options?.method === "PATCH") {
        writes.push(structuredClone(options.body));
        current = gateway(options.body.active, options.body.status, true);
        return current;
      }
      return current;
    }
    throw Error("independent setting unavailable");
  });
  client.request = request as ApiClient["request"];
  const w = mount(SettingsPage, {
    props: { client, adminClass: "super", ownSession: true },
    global: { stubs: { KitModal: KitModalStub } },
  });
  await flushPromises();
  await w.get("#settings-tab-sms").trigger("click");
  await flushPromises();
  return { w, client, writes };
}

it("lists the three original SMS providers and opens the aliyun dialog with its fields", async () => {
  const { w } = await setup();
  const section = w.get('section[aria-label="短信配置"]');
  expect(section.text()).toContain("阿里云短信");
  expect(section.text()).toContain("腾讯云短信");
  expect(section.text()).toContain("短信宝");
  expect(section.text()).toContain("已关闭");
  await w
    .findAll("button")
    .find((b) => b.text() === "服务商设置")!
    .trigger("click");
  await flushPromises();
  const dialog = w.get(".modal-stub");
  expect(dialog.attributes("aria-label")).toBe("阿里云短信设置");
  expect(dialog.text()).toContain("AccessKeyId");
  expect(dialog.text()).toContain("AccessKeySecret");
  expect(dialog.text()).toContain("模板");
  expect(dialog.text()).toContain("签名");
  w.unmount();
});

it("saves aliyun credentials with the current fingerprint and never echoes the secret", async () => {
  const { w, writes } = await setup();
  await w
    .findAll("button")
    .find((b) => b.text() === "服务商设置")!
    .trigger("click");
  await flushPromises();
  const dialog = w.get(".modal-stub");
  const inputs = dialog.findAll("input");
  // order in body: AccessKeyId, AccessKeySecret, 模板, 签名, enable-switch
  await inputs[0]!.setValue("LTAI-test");
  await inputs[1]!.setValue("synthetic134-secret-not-for-provider");
  await inputs[2]!.setValue("SMS_TEST_1");
  await inputs[3]!.setValue("测试签名");
  await inputs[inputs.length - 1]!.setValue(true);
  await dialog.get("form").trigger("submit");
  await flushPromises();
  expect(writes).toEqual([
    {
      provider: "aliyun",
      credentials: {
        template: "SMS_TEST_1",
        sign: "测试签名",
        access_key_id: "LTAI-test",
        access_key_secret: "synthetic134-secret-not-for-provider",
      },
      active: "aliyun",
      status: 1,
      expected_fingerprint: "a".repeat(64),
    },
  ]);
  expect(w.find(".modal-stub").exists()).toBe(false);
  expect(w.html()).not.toContain("synthetic134-secret-not-for-provider");
  expect(w.get('section[aria-label="短信配置"]').text()).toContain("保存成功");
  expect(w.get('section[aria-label="短信配置"]').text()).toContain("已开启");
  w.unmount();
});

it("requires complete provider fields before saving", async () => {
  const { w, writes } = await setup();
  await w
    .findAll("button")
    .find((b) => b.text() === "服务商设置")!
    .trigger("click");
  await flushPromises();
  await w.get(".modal-stub form").trigger("submit");
  await flushPromises();
  expect(writes).toEqual([]);
  expect(w.get(".modal-stub").text()).toContain(
    "请填写完整的服务商密钥、模板与签名",
  );
  w.unmount();
});
