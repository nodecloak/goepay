import { test, expect } from "@playwright/test";
const ok = (data: unknown) => ({
  status: 200,
  contentType: "application/json",
  body: JSON.stringify({ code: 0, data }),
});
for (const width of [375, 1280])
  test(`original settlement batch fields and frozen retry at ${width}px`, async ({
    page,
  }, testInfo) => {
    await page.setViewportSize({ width, height: 900 });
    let grouped = false,
      posts = 0;
    const posted: unknown[] = [];
    const writes: string[] = [];
    const row = {
      id: "84",
      batch: "20261025000000000084",
      external_no: "batch84",
      allmoney: "9.70",
      count: 1,
      time: "2026-10-25T00:00:00Z",
      status: "active",
      all_paid: false,
      items: [
        {
          request_id: "12",
          uid: "1000",
          position: 1,
          type: "wxpay",
          status: "approved",
          gross_amount_cents: "1000",
          fee_amount_cents: "30",
          net_amount_cents: "970",
          funding_state: "reserved",
          payout_state: "unknown",
          transfer_id: "90",
        },
      ],
    };
    await page.route("**/console/v1/**", (route) => {
      const req = route.request(),
        url = new URL(req.url()),
        path = url.pathname;
      if (path.endsWith("/session"))
        return route.fulfill(
          ok({
            user: {
              id: "1",
              name: "Local",
              role: "admin",
              admin_class: "super",
            },
            csrf_token: "csrf84",
          }),
        );
      if (path.endsWith("/security/payment-password"))
        return route.fulfill(
          ok({
            configured: false,
            enforced: false,
            credential_epoch: 0,
            totp_required: false,
          }),
        );
      if (req.method() !== "GET") writes.push(path);
      if (path === "/console/v1/admin/settlement-batches") {
        if (req.method() === "POST") {
          expect(req.headers()["x-csrf-token"]).toBe("csrf84");
          posts++;
          posted.push(req.postDataJSON());
          if (posts === 1)
            return route.fulfill({
              status: 503,
              contentType: "application/json",
              body: JSON.stringify({ code: -1, message: "模拟丢失确认" }),
            });
          grouped = true;
          return route.fulfill(ok(row));
        }
        return route.fulfill(
          ok({
            items: grouped ? [row] : [],
            total: grouped ? 1 : 0,
            next_before_id: "0",
          }),
        );
      }
      if (path === "/console/v1/admin/settlement-batches/84")
        return route.fulfill(ok(row));
      return route.fulfill(ok({}));
    });
    await page.goto(
      "/admin/settle/batch" + (width === 375 ? "?request_ids=12" : ""),
    );
    if (width !== 375)
      await page
        .getByRole("button", { name: "生成结算批次", exact: true })
        .click();
    const dialog = page.getByRole("dialog", { name: "生成结算批次" });
    if (width === 375)
      await expect(dialog.getByLabel("原结算申请编号")).toHaveValue("12");
    await dialog.getByLabel("原结算申请编号").fill("12");
    await dialog.getByRole("button", { name: "生成批次", exact: true }).click();
    await expect(dialog.getByLabel("原结算申请编号")).toBeDisabled();
    await dialog.getByRole("button", { name: "按原请求重试" }).click();
    await expect(dialog).not.toBeVisible();
    await expect(page.getByRole("region", { name: "批次详情" })).toBeVisible();
    expect(posted).toHaveLength(2);
    expect(posted[0]).toEqual(posted[1]);
    await expect(
      page.getByRole("columnheader", { name: "批次号", exact: true }),
    ).toBeVisible();
    await expect(
      page.getByRole("columnheader", { name: "总数量", exact: true }),
    ).toBeVisible();
    await expect(
      page.getByRole("button", { name: "完成批次", exact: true }),
    ).toBeDisabled();
    await expect(
      page.getByRole("button", { name: "核对原出款" }),
    ).toBeVisible();
    expect(writes).toEqual([
      "/console/v1/admin/settlement-batches",
      "/console/v1/admin/settlement-batches",
    ]);
    expect(
      await page.evaluate(
        () => document.documentElement.scrollWidth > innerWidth,
      ),
    ).toBe(false);
    await page.screenshot({
      path: testInfo.outputPath("batch-" + width + ".png"),
      fullPage: true,
    });
  });
