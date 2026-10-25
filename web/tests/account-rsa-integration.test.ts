// @vitest-environment jsdom
import { it, expect, vi } from "vitest";
import { mount, flushPromises } from "@vue/test-utils";
import { reactive } from "vue";
import { ApiClient } from "../src/api";
import AccountPage from "../src/components/AccountPage.vue";
it("clears old account capability and prevents delayed metadata reviving RSA in another section", async () => {
  const client = new ApiClient("merchant");
  client.csrfToken = "csrf";
  let finish!: (v: unknown) => void;
  vi.spyOn(client, "request").mockImplementation(
    () =>
      new Promise((r) => {
        finish = r;
      }) as never,
  );
  const w = mount(AccountPage, { props: { client, section: "api-info" } });
  await w.setProps({ section: "security" });
  finish({
    algorithm: "RSA",
    rsa_fingerprint: "a".repeat(64),
    can_rotate_rsa: true,
  });
  await flushPromises();
  expect(w.text()).toContain("修改登录密码");
  expect(w.text()).not.toContain("正在加载账户");
  expect(w.text()).not.toContain("RSA 密钥对");
  w.unmount();
});

it("a transient same-client role change burns the old API-info request", async () => {
  const client = reactive(new ApiClient("merchant"));
  client.csrfToken = "csrf";
  const finishes: ((v: unknown) => void)[] = [];
  vi.spyOn(client, "request").mockImplementation(
    () => new Promise((r) => finishes.push(r)) as never,
  );
  const w = mount(AccountPage, { props: { client, section: "api-info" } });
  client.area = "admin";
  client.area = "merchant";
  finishes[0]({
    algorithm: "RSA",
    rsa_fingerprint: "a".repeat(64),
    can_rotate_rsa: true,
  });
  await flushPromises();
  expect(w.text()).not.toContain("生成 RSA 密钥对");
  w.unmount();
});
