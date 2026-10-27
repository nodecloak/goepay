// @vitest-environment jsdom
import { afterEach, expect, it, vi } from "vitest";
import { mount, flushPromises, type VueWrapper } from "@vue/test-utils";
import { reactive } from "vue";
import AccountPage from "../src/components/AccountPage.vue";
import { ApiClient } from "../src/api";

const mounted: VueWrapper[] = [];
afterEach(() => {
  for (const w of mounted.splice(0)) w.unmount();
  vi.unstubAllGlobals();
});

it.each(["csrf", "section", "unmount"] as const)(
  "a late real-transport 401 after %s change cannot invalidate the current session",
  async (change) => {
    let finish!: (response: Response) => void;
    const pending = new Promise<Response>((resolve) => {
      finish = resolve;
    });
    const reply = (data: unknown) =>
      new Response(JSON.stringify({ code: 0, data }), { status: 200 });
    vi.stubGlobal(
      "fetch",
      vi.fn(async (input: RequestInfo | URL) => {
        const url = String(input);
        if (url.endsWith("/account/api-key/rotate")) return pending;
        if (url.endsWith("/account/api-info"))
          return reply({
            pid: "1000",
            algorithm: "MD5",
            api_key_masked: "old****key",
          });
        return reply({ name: "Replacement", email: "", has_pwd: true });
      }),
    );
    const client = reactive(new ApiClient("merchant"));
    client.csrfToken = "credentials199-real-transport-original";
    const unauthorized = vi.fn();
    client.onUnauthorized = unauthorized;
    const w = mount(AccountPage, {
      props: { client, section: "api-info" },
      global: {
        stubs: {
          MerchantEmailBinding197: true,
          MerchantRSAKeyRotation: true,
          MerchantSignaturePolicy: true,
        },
      },
    });
    mounted.push(w);
    await flushPromises();
    const reset = w
      .findAll("button")
      .find((button) => button.text() === "重置 API 密钥");
    expect(reset).toBeDefined();
    await reset!.trigger("click");
    await w
      .get('input[autocomplete="current-password"]')
      .setValue("old-password199-real-transport");
    await w.get('[role="dialog"] form').trigger("submit");
    if (change === "csrf")
      client.csrfToken = "credentials199-real-transport-replacement";
    else if (change === "section") await w.setProps({ section: "profile" });
    else w.unmount();
    await flushPromises();
    const currentCSRF = client.csrfToken;
    finish(
      new Response(
        JSON.stringify({ code: 401, message: "original session expired" }),
        { status: 401 },
      ),
    );
    await flushPromises();
    expect(client.csrfToken).toBe(currentCSRF);
    expect(unauthorized).not.toHaveBeenCalled();
    expect(w.emitted("reauthenticate")).toBeUndefined();
  },
);
