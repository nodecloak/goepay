// @vitest-environment jsdom
import { afterEach, expect, it, vi } from "vitest";
import { flushPromises, mount, type VueWrapper } from "@vue/test-utils";
import { ApiClient, ApiError } from "../src/api";
import MerchantSignaturePolicy from "../src/components/MerchantSignaturePolicy.vue";

const views: VueWrapper[] = [];
afterEach(() => {
  views.splice(0).forEach((view) => view.unmount());
  vi.restoreAllMocks();
  vi.unstubAllGlobals();
});
it.each([
  "original merchant session required",
  "own merchant session required",
  "own authenticated session required",
  "own merchant session required for membership purchase",
])(
  "explains %s without retrying or expiring the current session",
  async (message) => {
    const fetch = vi
      .fn()
      .mockResolvedValue(
        new Response(JSON.stringify({ code: -1, message }), { status: 403 }),
      );
    vi.stubGlobal("fetch", fetch);
    const client = new ApiClient("merchant");
    client.csrfToken = "synthetic-session-224";
    client.onUnauthorized = vi.fn();
    let result: unknown;
    try {
      await client.request("/account/profile");
    } catch (error) {
      result = error;
    }
    expect(result).toBeInstanceOf(ApiError);
    expect((result as ApiError).message).toContain("商户本人登录");
    expect((result as ApiError).message).not.toContain("刷新");
    expect((result as ApiError).status).toBe(403);
    expect((result as ApiError).code).toBe(-1);
    expect(client.csrfToken).toBe("synthetic-session-224");
    expect(client.onUnauthorized).not.toHaveBeenCalled();
    expect(fetch).toHaveBeenCalledTimes(1);
  },
);
it.each([
  [500, "original merchant session required"],
  [403, "unrecognized server reason"],
])("preserves unrelated server errors (%s)", (status, message) => {
  expect(new ApiError(String(message), Number(status), -1).message).toBe(
    message,
  );
});
it("retains unauthorized-session invalidation independently of the owner explanation", async () => {
  vi.stubGlobal(
    "fetch",
    vi
      .fn()
      .mockResolvedValue(
        new Response(
          JSON.stringify({ code: -1, message: "authentication required" }),
          { status: 401 },
        ),
      ),
  );
  const client = new ApiClient("merchant");
  client.csrfToken = "synthetic-session-224";
  client.onUnauthorized = vi.fn();
  await expect(client.request("/session")).rejects.toMatchObject({
    status: 401,
    message: "authentication required",
  });
  expect(client.csrfToken).toBe("");
  expect(client.onUnauthorized).toHaveBeenCalledTimes(1);
});
it("preserves typed refusal and decoding metadata used by money-request recovery", () => {
  const proof = {
    kind: "workflow_not_recorded",
    merchant_id: "1000",
    external_no: "synthetic-224",
    reason: "owner_required",
  };
  const error = new ApiError(
    "own merchant session required",
    403,
    -1,
    30,
    false,
    proof,
  );
  expect(error.message).toContain("商户本人登录");
  expect(error.requestNotRecorded).toBe(proof);
  expect(error.retryAfterSeconds).toBe(30);
  expect(error.responseDecoded).toBe(false);
});
it.each(["merchant", "admin"] as const)(
  "explains a forbidden %s signature-policy read rather than suggesting a refresh",
  async (area) => {
    const client = new ApiClient(area);
    const request = vi
      .spyOn(client, "request")
      .mockRejectedValue(
        new ApiError("original merchant session required", 403, -1),
      );
    const view = mount(MerchantSignaturePolicy, {
      props: {
        client,
        reload: 0,
        ...(area === "admin" ? { merchantID: "1000" } : {}),
      },
    });
    views.push(view);
    await flushPromises();
    expect(view.get('[role="alert"]').text()).toContain(
      area === "merchant" ? "商户本人登录" : "管理员没有读取签名方式的权限",
    );
    expect(view.get('[role="alert"]').text()).not.toContain("刷新重试");
    expect(request).toHaveBeenCalledTimes(1);
  },
);
it("retains a useful retry explanation for a transient signature-policy failure", async () => {
  const client = new ApiClient("merchant");
  vi.spyOn(client, "request").mockRejectedValue(
    new ApiError("upstream unavailable", 502, -1),
  );
  const view = mount(MerchantSignaturePolicy, { props: { client, reload: 0 } });
  views.push(view);
  await flushPromises();
  expect(view.get('[role="alert"]').text()).toBe(
    "签名方式读取失败，请刷新重试。",
  );
});
