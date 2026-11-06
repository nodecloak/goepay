import { test, expect } from "@playwright/test";
const ok = (data: unknown, status = 200) => ({
  status,
  contentType: "application/json",
  body: JSON.stringify({ code: 0, data }),
});
for (const width of [375, 1440])
  test(`original threshold mode and frozen authorized definition at ${width}px`, async ({
    page,
  }, info) => {
    await page.setViewportSize({ width, height: 1000 });
    let writes = 0;
    const paths: string[] = [];
    const plan = {
      id: "4",
      revision: "2",
      name: "商户余额自动转账",
      mode: "balance_threshold",
      enabled: true,
      last_authorization_id: "8",
      merchant_ids: ["2"],
      schedule: {
        frequency: "minute",
        interval: "1",
        anchor_at: "2026-10-24T00:00:00+08:00",
        execute_hour: 0,
        execute_minute: 0,
        execute_second: 0,
      },
    };
    await page.route("**/console/v1/**", (route) => {
      const req = route.request(),
        p = new URL(req.url()).pathname;
      paths.push(p);
      if (p.endsWith("/session"))
        return route.fulfill(
          ok({
            user: {
              id: "1",
              name: "Admin",
              role: "admin",
              admin_class: "super",
            },
            csrf_token: "threshold-local",
          }),
        );
      if (p.endsWith("/security/payment-password"))
        return route.fulfill(
          ok({
            configured: false,
            enforced: false,
            credential_epoch: 0,
            totp_required: false,
          }),
        );
      if (p.endsWith("/settlement-plans")) {
        if (req.method() === "POST") {
          writes++;
          expect(req.postDataJSON()).toEqual({
            name: "新的余额阈值计划",
            mode: "balance_threshold",
            merchant_ids: ["2"],
            schedule: plan.schedule,
          });
          return route.fulfill(
            ok(
              {
                ...plan,
                id: "5",
                revision: "1",
                enabled: false,
                last_authorization_id: "0",
              },
              201,
            ),
          );
        }
        return route.fulfill(ok({ items: [plan], next_cursor: "" }));
      }
      if (/\/settlement-plans\/[45]$/.test(p))
        return route.fulfill(ok({ ...plan, id: p.endsWith("5") ? "5" : "4" }));
      if (p.endsWith("/runs"))
        return route.fulfill(ok({ items: [], next_cursor: "" }));
      if (req.method() !== "GET") writes++;
      return route.fulfill(ok({}));
    });
    await page.goto("/admin/settle/plans");
    await page.getByRole("button", { name: "查看计划 4", exact: true }).click();
    await expect(page.getByText(/每次最多选 5/)).toBeVisible();
    await page
      .getByRole("button", { name: "修改计划定义", exact: true })
      .click();
    await expect(
      page.getByLabel("计划业务模式", { exact: true }),
    ).toBeDisabled();
    expect(writes).toBe(0);
    await page
      .getByRole("button", { name: "新建关闭的计划", exact: true })
      .click();
    await page
      .getByRole("button", { name: "计划业务模式", exact: true })
      .click();
    await page
      .getByRole("option", {
        name: "商户余额自动转账（整余额阈值）",
        exact: true,
      })
      .click();
    await page.getByLabel("计划名称", { exact: true }).fill("新的余额阈值计划");
    await page
      .getByRole("button", { name: "计划执行频率", exact: true })
      .click();
    await page.getByRole("option", { name: "按分钟", exact: true }).click();
    await page
      .getByLabel("锚点 RFC3339", { exact: true })
      .fill(plan.schedule.anchor_at);
    await page.getByLabel("执行小时（非按天为0）", { exact: true }).fill("0");
    await page
      .getByLabel("商户ID范围（逗号分隔，最多100）", { exact: true })
      .fill("2");
    await page
      .getByRole("button", { name: "核对计划定义", exact: true })
      .click();
    await expect(
      page.getByRole("region", { name: "计划操作确认", exact: true }),
    ).toContainText('"mode": "balance_threshold"');
    expect(writes).toBe(0);
    expect(
      await page.evaluate(
        () => document.documentElement.scrollWidth <= window.innerWidth,
      ),
    ).toBe(true);
    await page.screenshot({
      path: info.outputPath(`threshold-${width}.png`),
      fullPage: true,
      animations: "disabled",
    });
    await page
      .getByRole("button", { name: "确认保存关闭的定义", exact: true })
      .click();
    await expect.poll(() => writes).toBe(1);
    expect(
      paths.some((p) =>
        /auto-authorizations|payout.*execute|transfers/.test(p),
      ),
    ).toBe(false);
  });
