import { describe, expect, it, vi } from "vitest";
import { flushPromises, mount } from "@vue/test-utils";
import OAuthCredentialSettings from "../src/components/OAuthCredentialSettings.vue";
import { ApiClient, ApiError } from "../src/api";

// 快捷登录 secrets (GET|PATCH /settings/oauth-credentials): super admin only,
// write-only (old values never shown), every write carries the fingerprint
// it was based on, and an unconfirmed or conflicting result is never retried.

const fp = (c: string) => c.repeat(64);
const meta = (over: Record<string, unknown> = {}) => ({
  qq_set: false,
  wx_set: true,
  alipay_set: false,
  telegram_set: false,
  fingerprint: fp("a"),
  ...over,
});

function client(
  request: (...args: any[]) => any,
  area: "admin" | "merchant" = "admin",
) {
  return Object.assign(new ApiClient(area), { request, csrfToken: "csrf-1" });
}

const render = (
  request: (...args: any[]) => any,
  props: Record<string, unknown> = {},
) =>
  mount(OAuthCredentialSettings, {
    props: {
      client: client(request),
      ownSession: true,
      adminClass: "super",
      ...props,
    },
  });

const row = (w: ReturnType<typeof render>, title: string) =>
  w.findAll(".oauth-secret").find((r) => r.text().includes(title))!;

describe("OAuthCredentialSettings", () => {
  it("asks for the super admin's own session and reads nothing otherwise", async () => {
    for (const props of [{ adminClass: "operator" }, { ownSession: false }]) {
      const request = vi.fn();
      const w = render(request, props);
      await flushPromises();
      expect(w.text()).toContain("请使用超级管理员本人会话配置快捷登录密钥。");
      expect(request).not.toHaveBeenCalled();
      w.unmount();
    }
  });

  it("shows only whether each secret is set, never its value", async () => {
    const request = vi.fn(async (_path: string, _options?: any) => meta());
    const w = render(request);
    await flushPromises();
    expect(request).toHaveBeenCalledTimes(1);
    expect(request.mock.calls[0]![0]).toBe("/settings/oauth-credentials");
    expect(row(w, "QQ互联 APPKEY").text()).toContain("未配置");
    expect(row(w, "微信开放平台 AppSecret").text()).toContain("已配置");
    expect(w.find('[aria-label="清除微信开放平台 AppSecret"]').exists()).toBe(
      true,
    );
    expect(row(w, "QQ互联 APPKEY").text()).not.toContain("清除");
    w.unmount();
  });

  it("replaces a secret with the fingerprint it was based on, then forgets the typed value", async () => {
    const request = vi.fn(async (_path: string, options?: any) =>
      options?.method === "PATCH"
        ? meta({ qq_set: true, fingerprint: fp("b") })
        : meta(),
    );
    const w = render(request);
    await flushPromises();
    await row(w, "QQ互联 APPKEY").get("button").trigger("click");
    await w.get('input[type="password"]').setValue("qq-secret-1");
    await w.get("form").trigger("submit");
    await flushPromises();
    const patch = request.mock.calls.find((c) => c[1]?.method === "PATCH")!;
    expect(patch[1].body).toEqual({
      field: "qq",
      expected_fingerprint: fp("a"),
      value: "qq-secret-1",
    });
    expect(w.text()).toContain("QQ互联 APPKEY已保存");
    expect(row(w, "QQ互联 APPKEY").text()).toContain("已配置");
    expect(w.find('input[type="password"]').exists()).toBe(false);
    expect(w.html()).not.toContain("qq-secret-1");
    w.unmount();
  });

  it("stages and undoes clearing, then clears only the chosen secret on explicit save", async () => {
    const request = vi.fn(async (_path: string, options?: any) =>
      options?.method === "PATCH"
        ? meta({ wx_set: false, fingerprint: fp("c") })
        : meta(),
    );
    const w = render(request);
    await flushPromises();
    const clear = row(w, "微信开放平台 AppSecret")
      .findAll("button")
      .find((b) => b.text().startsWith("清除"))!;
    await clear.trigger("click");
    await flushPromises();
    expect(
      request.mock.calls.filter((c) => c[1]?.method === "PATCH"),
    ).toHaveLength(0);
    expect(w.text()).toContain("保存后清除");
    await w
      .get('[aria-label="撤销清除微信开放平台 AppSecret"]')
      .trigger("click");
    expect(w.text()).not.toContain("保存后清除");
    await w.get('[aria-label="清除微信开放平台 AppSecret"]').trigger("click");
    await w.get("form").trigger("submit");
    await flushPromises();
    const patch = request.mock.calls.find((c) => c[1]?.method === "PATCH")!;
    expect(patch[1].body).toEqual({
      field: "wx",
      expected_fingerprint: fp("a"),
      clear: true,
    });
    expect(w.text()).toContain("微信开放平台 AppSecret已清除");
    expect(
      request.mock.calls.filter((c) => c[1]?.method === "PATCH"),
    ).toHaveLength(1);
    w.unmount();
  });

  it("rejects a value with control characters before any write", async () => {
    const request = vi.fn(async (_path: string, _options?: any) => meta());
    const w = render(request);
    await flushPromises();
    await row(w, "Telegram Bot Token").get("button").trigger("click");
    await w.get('input[type="password"]').setValue("bad\u0001token");
    await w.get("form").trigger("submit");
    await flushPromises();
    expect(w.text()).toContain("密钥须为1到512位，不能包含换行或控制字符");
    expect(request.mock.calls.some((c) => c[1]?.method === "PATCH")).toBe(
      false,
    );
    w.unmount();
  });

  it("cancels a typed replacement without clearing it or changing any other platform", async () => {
    const request = vi.fn(async () => meta());
    const w = render(request);
    await flushPromises();
    await w.get('[aria-label="修改微信开放平台 AppSecret"]').trigger("click");
    await w.get('input[type="password"]').setValue("private-cancel-oauth245");
    await w
      .get('[aria-label="取消修改微信开放平台 AppSecret"]')
      .trigger("click");
    expect(w.find("form").exists()).toBe(false);
    expect(request).toHaveBeenCalledTimes(1);
    await w.get('[aria-label="修改微信开放平台 AppSecret"]').trigger("click");
    expect(w.get('input[type="password"]').element).toHaveProperty("value", "");
    expect(
      w.get('[aria-label="设置QQ互联 APPKEY"]').attributes("disabled"),
    ).toBeDefined();
    w.unmount();
  });

  it("forgets staged clearing when the authenticated context changes", async () => {
    const request = vi.fn(async () => meta());
    const w = render(request);
    await flushPromises();
    await w.get('[aria-label="清除微信开放平台 AppSecret"]').trigger("click");
    await w.setProps({ ownSession: false });
    await flushPromises();
    expect(w.find("form").exists()).toBe(false);
    expect(w.text()).not.toContain("保存后清除");
    expect(request).toHaveBeenCalledTimes(1);
    w.unmount();
  });

  it("a conflict drops the state and asks for a fresh read, without retrying", async () => {
    const request = vi.fn(async (_path: string, options?: any) => {
      if (options?.method === "PATCH") throw new ApiError("conflict", 409, -1);
      return meta();
    });
    const w = render(request);
    await flushPromises();
    await row(w, "QQ互联 APPKEY").get("button").trigger("click");
    await w.get('input[type="password"]').setValue("qq-secret-2");
    await w.get("form").trigger("submit");
    await flushPromises();
    expect(w.text()).toContain("刷新页面");
    expect(w.findAll(".oauth-secret")).toHaveLength(0);
    expect(
      request.mock.calls.filter((c) => c[1]?.method === "PATCH"),
    ).toHaveLength(1);
    w.unmount();
  });

  it("an answer with an unchanged fingerprint is treated as unconfirmed", async () => {
    const request = vi.fn(async (_path: string, _options?: any) => meta());
    const w = render(request);
    await flushPromises();
    await row(w, "QQ互联 APPKEY").get("button").trigger("click");
    await w.get('input[type="password"]').setValue("qq-secret-3");
    await w.get("form").trigger("submit");
    await flushPromises();
    expect(w.text()).toContain("刷新页面");
    w.unmount();
  });

  it("a malformed status reply is an error, not a blank form", async () => {
    const request = vi.fn(async () => ({ qq_set: "yes" }));
    const w = render(request);
    await flushPromises();
    expect(w.text()).toContain("刷新页面");
    expect(w.findAll(".oauth-secret")).toHaveLength(0);
    w.unmount();
  });
});
