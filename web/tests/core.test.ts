import { describe, it, expect, vi, afterEach } from "vitest";
import {
  menus,
  flattenMenu,
  visibleMenu,
  resolvePage,
  menuRole,
} from "../src/menu";
import { ApiClient, ApiError, validAmount } from "../src/api";
describe("original menu parity", () => {
  it("maps the initial server administrator while keeping explicit limited roles", () => {
    expect(menuRole("admin", "super")).toBe("R_SUPER");
    expect(menuRole("admin", "operator")).toBe("R_ADMIN");
    expect(menuRole("admin")).toBe("R_ADMIN");
    expect(menuRole("R_ADMIN")).toBe("R_ADMIN");
    expect(menuRole("merchant")).toBe("merchant");
  });
  it("retains all 54 admin and 28 merchant leaves, including hidden pages", () => {
    expect(flattenMenu(menus.admin)).toHaveLength(54);
    expect(flattenMenu(menus.merchant)).toHaveLength(28);
    expect(resolvePage("admin", "/applyments/form")?.hidden).toBe(true);
  });
  it("removes hidden pages only from navigation and enforces role filtering", () => {
    expect(
      flattenMenu(visibleMenu(menus.admin, "R_ADMIN")).some(
        (x) => x.path === "/app/app-store",
      ),
    ).toBe(false);
    expect(
      flattenMenu(visibleMenu(menus.admin, "R_SUPER")).some(
        (x) => x.path === "/applyments/form",
      ),
    ).toBe(false);
  });
});
describe("console transport", () => {
  it("preserves server cooldown seconds without inventing a retry period", async () => {
    vi.stubGlobal(
      "fetch",
      vi
        .fn()
        .mockResolvedValue(
          new Response(JSON.stringify({ code: -1, message: "cooldown" }), {
            status: 429,
            headers: { "Retry-After": "60" },
          }),
        ),
    );
    const client = new ApiClient("merchant");
    client.csrfToken = "csrf";
    await expect(
      client.request("/orders/ONE/resend-notification", {
        method: "POST",
        body: { external_no: "uuid" },
      }),
    ).rejects.toMatchObject({ status: 429, retryAfterSeconds: 60 });
  });
  it("expires the local console session when a protected child request returns 401", async () => {
    vi.stubGlobal(
      "fetch",
      vi
        .fn()
        .mockResolvedValue(
          new Response(
            JSON.stringify({ code: -1, message: "session expired" }),
            { status: 401 },
          ),
        ),
    );
    const onUnauthorized = vi.fn();
    const c = Object.assign(new ApiClient("merchant"), { onUnauthorized });
    c.csrfToken = "old-token";
    await expect(c.request("/resources/domains")).rejects.toBeInstanceOf(
      ApiError,
    );
    expect(onUnauthorized).toHaveBeenCalledOnce();
    expect(c.csrfToken).toBe("");
  });
  afterEach(() => vi.unstubAllGlobals());
  it("rejects writes before a CSRF token exists", async () => {
    const fetcher = vi.fn();
    vi.stubGlobal("fetch", fetcher);
    await expect(
      new ApiClient("admin").request("/orders", { method: "POST", body: {} }),
    ).rejects.toThrow("会话");
    expect(fetcher).not.toHaveBeenCalled();
  });
  it("uses scope-specific cookie sessions and CSRF without storing credentials", async () => {
    const fetcher = vi
      .fn()
      .mockResolvedValue(
        new Response(JSON.stringify({ code: 0, data: { id: "r1" } })),
      );
    vi.stubGlobal("fetch", fetcher);
    const c = new ApiClient("merchant");
    c.csrfToken = "token";
    expect(
      await c.request("/orders/o1/refund", {
        method: "POST",
        body: { amount: "0.01" },
      }),
    ).toEqual({ id: "r1" });
    expect(fetcher).toHaveBeenCalledWith(
      "/console/v1/merchant/orders/o1/refund",
      expect.objectContaining({
        credentials: "same-origin",
        headers: expect.objectContaining({ "X-CSRF-Token": "token" }),
        body: '{"amount":"0.01"}',
      }),
    );
  });
  it("does not treat application-level failure as success", async () => {
    vi.stubGlobal(
      "fetch",
      vi
        .fn()
        .mockResolvedValue(
          new Response(JSON.stringify({ code: 403, message: "无权限" })),
        ),
    );
    await expect(
      new ApiClient("admin").request("/orders"),
    ).rejects.toBeInstanceOf(ApiError);
  });
  it("rejects unsafe money formats without converting through floating point", () => {
    for (const x of ["0", "-1", "1e3", "1.001", "NaN", "01.00"])
      expect(validAmount(x)).toBe(false);
    expect(validAmount("123456789.01")).toBe(true);
    expect(validAmount("0.01")).toBe(true);
  });
});
