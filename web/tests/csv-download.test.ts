import { afterEach, expect, it, vi } from "vitest";
import { ApiClient } from "../src/api";
afterEach(() => vi.unstubAllGlobals());
it("preserves original GBK bytes only for the two original settlement batch export formats", async () => {
  const create = vi.fn().mockReturnValue("blob:gbk84"),
    revoke = vi.fn();
  vi.stubGlobal(
    "URL",
    class extends URL {
      static createObjectURL = create;
      static revokeObjectURL = revoke;
    },
  );
  const click = vi
    .spyOn(HTMLAnchorElement.prototype, "click")
    .mockImplementation(() => {});
  const raw = new Uint8Array([0xca, 0xd5, 0xbf, 0xee, 0x2c, 0x31, 0x0d, 0x0a]);
  const fetcher = vi.fn(() =>
    Promise.resolve(
      new Response(raw, {
        headers: {
          "Content-Type": "text/csv; charset=gbk",
          "Content-Disposition": "attachment",
        },
      }),
    ),
  );
  vi.stubGlobal("fetch", fetcher);
  const client = new ApiClient("admin");
  await client.downloadCSV(
    "/settlement-batches/1/export?format=alipay",
    "pay_alipay_20261025000000000001.csv",
    { encoding: "gbk" },
  );
  expect(click).toHaveBeenCalledOnce();
  expect(create).toHaveBeenCalledOnce();
  await expect(
    client.downloadCSV("/orders?format=csv", "orders.csv", { encoding: "gbk" }),
  ).rejects.toThrow();
  await expect(
    new ApiClient("merchant").downloadCSV(
      "/settlement-batches/1/export?format=alipay",
      "pay.csv",
      { encoding: "gbk" },
    ),
  ).rejects.toThrow();
  fetcher.mockImplementation(() =>
    Promise.resolve(
      new Response(raw, {
        headers: {
          "Content-Type": "text/csv; charset=utf-8",
          "Content-Disposition": "attachment",
        },
      }),
    ),
  );
  await expect(
    client.downloadCSV(
      "/settlement-batches/1/export?format=mybank",
      "pay.csv",
      { encoding: "gbk" },
    ),
  ).rejects.toThrow();
  expect(click).toHaveBeenCalledOnce();
  click.mockRestore();
});
it("expires the session and never downloads JSON failures, including HTTP200 application errors", async () => {
  const client = new ApiClient("merchant");
  client.csrfToken = "csrf";
  client.onUnauthorized = vi.fn();
  const save = vi.spyOn(HTMLAnchorElement.prototype, "click");
  for (const status of [401, 422, 429, 503, 200]) {
    vi.stubGlobal(
      "fetch",
      vi.fn().mockResolvedValue(
        new Response(JSON.stringify({ code: -1, message: "export denied" }), {
          status,
          headers: { "Content-Type": "application/json" },
        }),
      ),
    );
    await expect(
      client.downloadCSV("/orders?format=csv", "orders.csv"),
    ).rejects.toThrow("export denied");
  }
  expect(client.csrfToken).toBe("");
  expect(client.onUnauthorized).toHaveBeenCalledOnce();
  expect(save).not.toHaveBeenCalled();
  save.mockRestore();
});
it("requires the CSV attachment and BOM and aborts before creating a file", async () => {
  const client = new ApiClient("admin");
  const cases: Record<string, string>[] = [
    { "Content-Type": "text/html" },
    { "Content-Type": "text/csv" },
    { "Content-Type": "text/csv", "Content-Disposition": "attachment" },
  ];
  for (const headers of cases) {
    vi.stubGlobal(
      "fetch",
      vi.fn().mockResolvedValue(new Response("not csv", { headers })),
    );
    await expect(
      client.downloadCSV("/ledger?format=csv", "ledger.csv"),
    ).rejects.toThrow();
  }
  const controller = new AbortController();
  controller.abort();
  vi.stubGlobal(
    "fetch",
    vi.fn().mockResolvedValue(
      new Response('\uFEFF"id"\r\n"1"', {
        headers: {
          "Content-Type": "text/csv",
          "Content-Disposition": "attachment",
        },
      }),
    ),
  );
  await expect(
    client.downloadCSV("/ledger?format=csv", "ledger.csv", {
      signal: controller.signal,
    }),
  ).rejects.toThrow();
});
it("streams at most8MiB, preserves scope/filter and credentials, sanitizes filename and revokes the object URL", async () => {
  const create = vi.fn().mockReturnValue("blob:local-csv"),
    revoke = vi.fn();
  vi.stubGlobal(
    "URL",
    class extends URL {
      static createObjectURL = create;
      static revokeObjectURL = revoke;
    },
  );
  const click = vi
    .spyOn(HTMLAnchorElement.prototype, "click")
    .mockImplementation(function (this: HTMLAnchorElement) {
      expect(this.download).toBe("orders.csv");
      expect(this.href).toBe("blob:local-csv");
    });
  const fetcher = vi.fn().mockResolvedValue(
    new Response('\uFEFF"trade_no"\r\n"ONE"', {
      headers: {
        "Content-Type": "text/csv;charset=utf-8",
        "Content-Disposition": 'attachment; filename="ignored.csv"',
      },
    }),
  );
  vi.stubGlobal("fetch", fetcher);
  await new ApiClient("merchant").downloadCSV(
    "/orders?status=paid&format=csv",
    "orders.csv",
  );
  expect(fetcher).toHaveBeenCalledWith(
    "/console/v1/merchant/orders?status=paid&format=csv",
    expect.objectContaining({
      credentials: "same-origin",
      headers: expect.objectContaining({ Accept: "text/csv" }),
    }),
  );
  expect(click).toHaveBeenCalledOnce();
  expect(revoke).toHaveBeenCalledWith("blob:local-csv");
  fetcher.mockResolvedValueOnce(
    new Response(new Uint8Array(8 * 1024 * 1024 + 1), {
      headers: {
        "Content-Type": "text/csv",
        "Content-Disposition": "attachment",
      },
    }),
  );
  await expect(
    new ApiClient("merchant").downloadCSV("/orders?format=csv", "orders.csv"),
  ).rejects.toThrow("8 MiB");
  expect(click).toHaveBeenCalledOnce();
  click.mockRestore();
});
