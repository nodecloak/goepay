import { afterEach, expect, it, vi } from "vitest";
import { ApiClient, ApiError } from "../src/api";

afterEach(() => vi.unstubAllGlobals());

for (const operation of ["request", "download"] as const) {
  it.each(["csrf", "area", "abort", "same"] as const)(
    `${operation} only expires the session that produced a current, active 401 (%s)`,
    async (change) => {
      let finish!: (value: Response) => void;
      vi.stubGlobal("fetch", vi.fn(() => new Promise<Response>((resolve) => { finish = resolve; })));
      const client = new ApiClient("merchant");
      client.csrfToken = "original-context199";
      client.onUnauthorized = vi.fn();
      const controller = new AbortController();
      const outcome = (operation === "request"
        ? client.request("/account/profile", { signal: controller.signal })
        : client.downloadCSV("/orders/export", "orders.csv", { signal: controller.signal })
      ).catch((error: unknown) => error);
      if (change === "csrf") client.csrfToken = "replacement-context199";
      if (change === "area") client.area = "admin";
      if (change === "abort") controller.abort();
      const currentToken = client.csrfToken;
      finish(new Response(JSON.stringify({ code: 401, message: "original session expired" }), { status: 401 }));
      expect(await outcome).toBeInstanceOf(ApiError);
      if (change === "same") {
        expect(client.csrfToken).toBe("");
        expect(client.onUnauthorized).toHaveBeenCalledOnce();
      } else {
        expect(client.csrfToken).toBe(currentToken);
        expect(client.onUnauthorized).not.toHaveBeenCalled();
      }
    },
  );
}
