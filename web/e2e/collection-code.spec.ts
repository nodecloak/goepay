import { test, expect } from "@playwright/test";
const ok = (data: unknown) => ({
  status: 200,
  contentType: "application/json",
  body: JSON.stringify({ code: 0, data }),
});
test("collection code links generate a local QR and downloadable image without external image requests", async ({
  page,
}) => {
  let linkRequests = 0;
  await page.route("**/console/v1/**", (route) => {
    const p = new URL(route.request().url()).pathname;
    if (p.endsWith("/session"))
      return route.fulfill(
        ok({
          user: { id: "1000", name: "Merchant", role: "merchant" },
          csrf_token: "token",
        }),
      );
    if (p.endsWith("/schema"))
      return route.fulfill(
        ok({
          title: "聚合收款",
          fields: [{ name: "name", label: "名称", type: "string" }],
          permissions: { create: true, update: true, delete: true },
        }),
      );
    if (p.endsWith("/link")) {
      linkRequests++;
      expect(p).toBe("/console/v1/merchant/collection-codes/1/link");
      return route.fulfill(
        ok({
          url: "http://127.0.0.1:8181/collect/1?expires=123&token=test-token",
          expires_at: "2026-10-24T00:00:00+08:00",
        }),
      );
    }
    const row = { id: "1", name: "Test collection", enabled: true };
    return route.fulfill(
      ok(p.endsWith("/1") ? row : { items: [row], total: 1 }),
    );
  });
  await page.goto("/merchant/onecode");
  await page.getByRole("button", { name: "详情", exact: true }).click();
  expect(linkRequests).toBe(0);
  await page.getByRole("button", { name: "生成收款链接" }).click();
  const qr = page.getByAltText("聚合收款二维码");
  await expect(qr).toBeVisible();
  expect(await qr.getAttribute("src")).toMatch(/^data:image\/png;base64,/);
  await expect(page.getByLabel("收款链接", { exact: true })).toHaveValue(
    "http://127.0.0.1:8181/collect/1?expires=123&token=test-token",
  );
  const download = page.waitForEvent("download");
  await page.getByRole("button", { name: "下载二维码" }).click();
  expect((await download).suggestedFilename()).toBe("collection-code-1.png");
  expect(linkRequests).toBe(1);
});
