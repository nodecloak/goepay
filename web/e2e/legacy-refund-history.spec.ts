import { test, expect } from "@playwright/test";

test("refund page exposes separate read only imported history with explicit owner", async ({
  page,
}) => {
  let historyReads = 0;
  await page.route("**/console/v1/**", (route) => {
    const url = new URL(route.request().url());
    const ok = (data: unknown) =>
      route.fulfill({
        contentType: "application/json",
        body: JSON.stringify({ code: 0, data }),
      });
    if (url.pathname.endsWith("/session"))
      return ok({
        user: {
          id: "1",
          role: "admin",
          username: "admin",
          admin_class: "super",
        },
        csrf_token: "csrf",
      });
    if (url.pathname.endsWith("/security/payment-password"))
      return ok({ configured: false, enforced: false });
    if (url.pathname.endsWith("/legacy-refund-history")) {
      expect(route.request().method()).toBe("GET");
      expect(url.searchParams.get("merchant_id")).toBe("9001");
      historyReads++;
      return ok({
        items: [
          {
            id: "1",
            export_id: "sanitized-local-fixture",
            trade_no: "HIST-TRADE",
            refund_no: "HIST-REFUND",
            amount: "50",
            legacy_status: "unknown",
            verified_payment_source: false,
          },
        ],
        next_cursor: "",
      });
    }
    return ok({ items: [], total: 0 });
  });
  await page.setViewportSize({ width: 375, height: 812 });
  await page.goto("/admin/trade/refund");
  await page.getByText("查看导入退款历史（只读）", { exact: true }).click();
  expect(historyReads).toBe(0);
  await page.getByLabel("历史所属商户ID").fill("9001");
  await page.getByRole("button", { name: "查询历史记录", exact: true }).click();
  await expect(page.getByText("HIST-REFUND", { exact: true })).toBeVisible();
  await expect(page.getByText("原系统未明确", { exact: true })).toBeVisible();
  await expect(page.getByText("0.50", { exact: true })).toBeVisible();
  expect(historyReads).toBe(1);
  const overflow = await page.evaluate(
    () => document.documentElement.scrollWidth > window.innerWidth,
  );
  expect(overflow).toBe(false);
  await page.getByLabel("历史所属商户ID").fill("9002");
  await expect(page.getByText("HIST-REFUND", { exact: true })).toHaveCount(0);
  expect(historyReads).toBe(1);
});
