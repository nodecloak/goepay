import { it, expect, vi } from "vitest";
import { mount, flushPromises } from "@vue/test-utils";
import MerchantSignaturePolicy from "../src/components/MerchantSignaturePolicy.vue";
import { ApiClient } from "../src/api";
const state = (id: string, keytype: number | null = null, fp = "a") => ({
  merchant_id: id,
  keytype,
  algorithm: "MD5",
  rsa_configured: true,
  fingerprint: fp.repeat(64),
  can_update: true,
});
function client(
  request: ReturnType<typeof vi.fn>,
  area: "merchant" | "admin" = "merchant",
) {
  const c = new ApiClient(area);
  c.csrfToken = "context163";
  c.request = request as unknown as ApiClient["request"];
  return c;
}
it("an earlier policy GET cannot replace the next merchant session state", async () => {
  let resolve = (_: unknown) => {};
  const pending = new Promise((r) => (resolve = r)),
    first = vi.fn().mockReturnValue(pending),
    second = vi.fn().mockResolvedValue(state("9007199254740995", 1, "b"));
  const wrapper = mount(MerchantSignaturePolicy, {
    props: { client: client(first), reload: 0 },
  });
  await wrapper.setProps({ client: client(second) });
  await flushPromises();
  expect(wrapper.text()).toContain("当前方式：仅RSA");
  resolve(state("9007199254740993"));
  await flushPromises();
  expect(wrapper.text()).toContain("当前方式：仅RSA");
  expect(wrapper.text()).not.toContain("历史配置");
  expect(first).toHaveBeenCalledTimes(1);
  expect(second).toHaveBeenCalledTimes(1);
  wrapper.unmount();
});

it("late administrator policy reads cannot replace the next full target", async () => {
  let resolve = (_: unknown) => {};
  const pending = new Promise((r) => (resolve = r)),
    request = vi
      .fn()
      .mockReturnValueOnce(pending)
      .mockResolvedValueOnce(state("9007199254740995", 1, "b"));
  const wrapper = mount(MerchantSignaturePolicy, {
    props: {
      client: client(request, "admin"),
      merchantID: "9007199254740993",
      reload: 0,
    },
  });
  await wrapper.setProps({ merchantID: "9007199254740995" });
  await flushPromises();
  resolve(state("9007199254740993"));
  await flushPromises();
  expect(wrapper.text()).toContain("当前方式：仅RSA");
  expect(request.mock.calls.map((c) => c[0])).toEqual([
    "/merchants/9007199254740993/signature-policy",
    "/merchants/9007199254740995/signature-policy",
  ]);
  expect(
    wrapper
      .emitted("observed")
      ?.map((v) => (v[0] as { merchant_id: string }).merchant_id),
  ).toEqual(["9007199254740995"]);
  wrapper.unmount();
});

it("an administrator policy read for a different returned target never enables writes", async () => {
  const request = vi.fn().mockResolvedValue(state("9007199254740995"));
  const wrapper = mount(MerchantSignaturePolicy, {
    props: {
      client: client(request, "admin"),
      merchantID: "9007199254740993",
      reload: 0,
    },
  });
  await flushPromises();
  expect(wrapper.find("select").exists()).toBe(false);
  expect(wrapper.get('[role="alert"]').text()).toContain("读取失败");
  expect(wrapper.emitted("observed")).toBeUndefined();
  expect(request).toHaveBeenCalledTimes(1);
  wrapper.unmount();
});
it("an earlier committed PATCH response cannot mark another merchant session saved or alter its selection", async () => {
  let resolve = (_: unknown) => {};
  const pending = new Promise((r) => (resolve = r)),
    first = vi
      .fn()
      .mockResolvedValueOnce(state("9007199254740993"))
      .mockReturnValueOnce(pending),
    next = vi.fn().mockResolvedValue(state("9007199254740995", 0, "c"));
  const wrapper = mount(MerchantSignaturePolicy, {
    props: { client: client(first), reload: 0 },
  });
  await flushPromises();
  await wrapper.get("select").setValue("1");
  await wrapper
    .findAll("button")
    .find((b) => b.text() === "保存签名方式")!
    .trigger("click");
  await wrapper
    .findAll("button")
    .find((b) => b.text() === "确定修改")!
    .trigger("click");
  await flushPromises();
  expect(first).toHaveBeenCalledTimes(2);
  await wrapper.setProps({ client: client(next) });
  await flushPromises();
  resolve(state("9007199254740993", 1, "b"));
  await flushPromises();
  expect(wrapper.text()).toContain("当前方式：MD5+RSA兼容");
  expect((wrapper.get("select").element as HTMLSelectElement).value).toBe("0");
  expect(wrapper.text()).not.toContain("签名方式已保存");
  expect(next).toHaveBeenCalledTimes(1);
  wrapper.unmount();
});
