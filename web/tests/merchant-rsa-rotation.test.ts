// @vitest-environment jsdom
import { it, expect, vi } from "vitest";
import { mount, flushPromises } from "@vue/test-utils";
import { reactive } from "vue";
import { ApiClient } from "../src/api";
import MerchantRSAKeyRotation from "../src/components/MerchantRSAKeyRotation.vue";

const fingerprint = "a".repeat(64);
const publicKey =
  "-----BEGIN PUBLIC KEY-----\n" +
  "A".repeat(392) +
  "\n-----END PUBLIC KEY-----\n";
const privateKey =
  "-----BEGIN PRIVATE KEY-----\n" +
  "B".repeat(1624) +
  "\n-----END PRIVATE KEY-----\n";
const dto = {
  algorithm: "RSA",
  public_key: publicKey,
  private_key: privateKey,
  fingerprint: "b".repeat(64),
};
const setup = () => {
  const client = new ApiClient("merchant");
  client.csrfToken = "csrf";
  return {
    client,
    w: mount(MerchantRSAKeyRotation, {
      props: {
        client,
        allowed: true,
        algorithm: "RSA",
        fingerprint,
        hasKey: true,
      },
    }),
  };
};
const button = (w: ReturnType<typeof setup>["w"], name: string) =>
  w.findAll("button").find((b) => b.text() === name)!;
async function submit(w: ReturnType<typeof setup>["w"]) {
  await button(w, "重置 RSA 密钥对").trigger("click");
  await w.find('input[type="password"]').setValue("current-password-strong");
  await w.find("form").trigger("submit");
  await flushPromises();
}

it("requires an explicit password and CAS then exposes private once without storage", async () => {
  const { client, w } = setup();
  const request = vi.spyOn(client, "request").mockResolvedValue(dto);
  expect(request).not.toHaveBeenCalled();
  await submit(w);
  expect(request).toHaveBeenCalledTimes(1);
  expect(request.mock.calls[0][0]).toBe("/account/rsa-key/rotate");
  expect(request.mock.calls[0][1]?.body).toEqual({
    current_password: "current-password-strong",
    expected_fingerprint: fingerprint,
  });
  expect(w.find('textarea[aria-label="新 RSA 私钥"]').element).toHaveProperty(
    "value",
    privateKey,
  );
  expect(localStorage.length + sessionStorage.length).toBe(0);
  await button(w, "已保存，关闭私钥").trigger("click");
  expect(w.text()).not.toContain(privateKey);
  expect(w.find("textarea").exists()).toBe(false);
  w.unmount();
});
it("default-off and admin clients never offer generation", () => {
  const hidden = mount(MerchantRSAKeyRotation, {
    props: { client: new ApiClient("merchant") },
  });
  expect(hidden.find("button").exists()).toBe(false);
  hidden.unmount();
  const admin = mount(MerchantRSAKeyRotation, {
    props: {
      client: new ApiClient("admin"),
      allowed: true,
      algorithm: "RSA",
      fingerprint,
      hasKey: true,
    },
  });
  expect(admin.find("button").exists()).toBe(false);
  admin.unmount();
});
it("aborts close and ignores late success without reviving a private key", async () => {
  const { client, w } = setup();
  let finish!: (v: unknown) => void;
  const request = vi.spyOn(client, "request").mockImplementation(
    () =>
      new Promise((r) => {
        finish = r;
      }) as never,
  );
  await submit(w);
  await button(w, "关闭").trigger("click");
  finish(dto);
  await flushPromises();
  expect(w.find("textarea").exists()).toBe(false);
  expect(request).toHaveBeenCalledTimes(1);
  w.unmount();
});
it("clears on CSRF/client/context changes and rejects late old-owner response", async () => {
  const { client, w } = setup();
  let finish!: (v: unknown) => void;
  vi.spyOn(client, "request").mockImplementation(
    () =>
      new Promise((r) => {
        finish = r;
      }) as never,
  );
  await submit(w);
  const next = new ApiClient("merchant");
  next.csrfToken = "new";
  await w.setProps({ client: next });
  finish(dto);
  await flushPromises();
  expect(w.find("textarea").exists()).toBe(false);
  w.unmount();
});
it("unknown response cannot automatically retry or expose secrets from an error", async () => {
  const { client, w } = setup();
  const request = vi
    .spyOn(client, "request")
    .mockRejectedValue(new Error(privateKey));
  await submit(w);
  expect(w.text()).not.toContain(privateKey);
  expect(w.text()).toContain("刷新接入信息");
  expect(request).toHaveBeenCalledTimes(1);
  expect(w.find('input[type="password"]').exists()).toBe(false);
  expect(w.emitted("refresh")).toHaveLength(1);
  w.unmount();
});
it("rejects malformed or wrong-algorithm successful DTO without revealing private material", async () => {
  const { client, w } = setup();
  vi.spyOn(client, "request").mockResolvedValue({ ...dto, algorithm: "MD5" });
  await submit(w);
  expect(w.find("textarea").exists()).toBe(false);
  expect(w.text()).not.toContain(privateKey);
  w.unmount();
});

it("synchronously burns private state and pending responses on transient same-client area changes", async () => {
  for (const pending of [false, true]) {
    const client = reactive(new ApiClient("merchant"));
    client.csrfToken = "csrf";
    const w = mount(MerchantRSAKeyRotation, {
      props: {
        client,
        allowed: true,
        algorithm: "RSA",
        fingerprint,
        hasKey: true,
      },
    });
    let finish!: (v: unknown) => void;
    vi.spyOn(client, "request").mockImplementation(() =>
      pending
        ? (new Promise((r) => {
            finish = r;
          }) as never)
        : (Promise.resolve(dto) as never),
    );
    await submit(w);
    client.area = "admin";
    client.area = "merchant";
    if (pending) finish(dto);
    await flushPromises();
    expect(w.find("textarea").exists()).toBe(false);
    w.unmount();
  }
});
