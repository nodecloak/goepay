import { test, expect, type Page } from "@playwright/test";
async function fixture(page: Page, delegated = false) {
  const reads: URL[] = [],
    writes: string[] = [],
    errors: string[] = [];
  let fail = false;
  page.on("pageerror", (e) => errors.push(e.message));
  await page.route("**/console/v1/**", (route) => {
    const req = route.request(),
      u = new URL(req.url()),
      p = u.pathname;
    let data: unknown = { enabled: false };
    if (req.method() !== "GET") writes.push(p);
    else reads.push(u);
    if (p.endsWith("/session"))
      data = {
        user: { id: "1", name: "本地商户", role: "merchant" },
        csrf_token: "account121-csrf",
        ...(delegated
          ? { delegated_by: { id: "2", name: "Local admin" } }
          : {}),
      };
    else if (p.endsWith("/account/profile"))
      data = {
        name: "本地商户",
        email: "account121@example.invalid",
        pay: "1",
      };
    else if (p.endsWith("/account/notice"))
      data = {
        id: "0",
        merchant_id: "1",
        revision: "0",
        voice_order: false,
        email_masked: "l***@example.invalid",
        msgconfig: {
          order: 0,
          settle: 0,
          login: 0,
          complain: 0,
          mchrisk: 0,
          balance: 0,
          group: 0,
          domain_audit: 0,
          daily_report: 0,
          order_money: "0.00",
          balance_money: "0.00",
        },
        configured: {},
        target_masks: {},
      };
    else if (p.endsWith("/notice/events"))
      data = { items: [], next_cursor: "" };
    else if (p.endsWith("/account/printing"))
      data = {
        revision: "0",
        mode: 0,
        copies: 1,
        voice: false,
        device_masked: "",
        device_present: false,
      };
    else if (p.endsWith("/account/login-log")) {
      if (fail)
        return route.fulfill({
          status: 503,
          contentType: "application/json",
          body: JSON.stringify({
            code: -1,
            message: "local login log unavailable",
          }),
        });
      const n = u.searchParams.get("page") === "2" ? 21 : 1;
      data = {
        items: [
          {
            id: String(n),
            type: "",
            ip: "192.0.2." + n,
            city: "",
            created_at: "2026-10-25T00:00:00Z",
          },
        ],
        total: 21,
      };
    }
    return route.fulfill({
      status: 200,
      contentType: "application/json",
      body: JSON.stringify({ code: 0, data }),
    });
  });
  await page.goto("/merchant/account/settings");
  await expect(
    page.getByRole("heading", { name: "账户资料", exact: true }),
  ).toBeVisible();
  return {
    reads,
    writes,
    errors,
    fail: () => {
      fail = true;
    },
  };
}
for (const width of [1280, 375]) {
  test(`original account cards and own login-log dialog work together at ${width}px`, async ({
    page,
  }, info) => {
    await page.setViewportSize({ width, height: 1000 });
    const f = await fixture(page);
    await expect(
      page.getByRole("heading", { name: "消息推送", exact: true }),
    ).toBeVisible();
    await expect(
      page.getByRole("heading", { name: "小票打印设置", exact: true }),
    ).toBeVisible();
    expect(
      f.reads.some((u) => u.pathname === "/console/v1/merchant/account/notice"),
    ).toBe(true);
    expect(
      f.reads.some(
        (u) => u.pathname === "/console/v1/merchant/account/printing",
      ),
    ).toBe(true);
    expect(f.reads.some((u) => u.pathname.endsWith("/account/login-log"))).toBe(
      false,
    );
    const opener = page.getByRole("button", { name: "登录日志", exact: true });
    await opener.click();
    const dialog = page.getByRole("dialog", { name: "登录日志", exact: true });
    await expect(dialog.getByText("192.0.2.1", { exact: true })).toBeVisible();
    await expect(dialog.getByRole("columnheader")).toHaveText([
      "登录方式",
      "IP地址",
      "归属地",
      "时间",
    ]);
    await expect(dialog.getByText("未记录", { exact: true })).toHaveCount(2);
    await expect(
      dialog.getByText("2026-10-25 08:00:00", { exact: true }),
    ).toBeVisible();
    await dialog
      .getByRole("button", { name: "下一页登录日志", exact: true })
      .click();
    await expect(dialog.getByText("192.0.2.21", { exact: true })).toBeVisible();
    expect(f.reads.at(-1)!.searchParams.toString()).toBe("page=2&limit=20");
    const bounds = await dialog.boundingBox();
    expect(bounds!.x).toBeGreaterThanOrEqual(0);
    expect(bounds!.x + bounds!.width).toBeLessThanOrEqual(width);
    expect(
      await dialog
        .locator(".table-scroll")
        .evaluate((e) => e.scrollWidth <= e.clientWidth),
    ).toBe(true);
    await page.screenshot({
      path: info.outputPath("original-account-login-log-" + width + ".png"),
    });
    await dialog
      .getByRole("button", { name: "关闭登录日志", exact: true })
      .press("Escape");
    await expect(dialog).toHaveCount(0);
    await expect(opener).toBeFocused();
    expect(f.writes).toHaveLength(0);
    expect(f.errors).toHaveLength(0);
  });
  test(`a refused own log refresh clears previous rows at ${width}px`, async ({
    page,
  }) => {
    await page.setViewportSize({ width, height: 1000 });
    const f = await fixture(page);
    await page.getByRole("button", { name: "登录日志", exact: true }).click();
    const dialog = page.getByRole("dialog", { name: "登录日志", exact: true });
    await expect(dialog.getByText("192.0.2.1", { exact: true })).toBeVisible();
    f.fail();
    await dialog
      .getByRole("button", { name: "刷新登录日志", exact: true })
      .click();
    await expect(dialog.getByRole("alert")).toContainText(
      "local login log unavailable",
    );
    await expect(dialog.getByText("192.0.2.1", { exact: true })).toHaveCount(0);
    await expect(
      dialog.getByRole("button", { name: "下一页登录日志", exact: true }),
    ).toBeDisabled();
    expect(f.writes).toHaveLength(0);
  });
}
test("delegated merchant settings cannot read personal logs or notification/device secrets", async ({
  page,
}) => {
  const f = await fixture(page, true);
  await expect(
    page.getByText("安全设置和本人登录日志需要商户本人登录后查看。", {
      exact: true,
    }),
  ).toBeVisible();
  await expect(
    page.getByRole("button", { name: "登录日志", exact: true }),
  ).toHaveCount(0);
  expect(
    f.reads.some((u) =>
      /\/account\/(login-log|notice|printing)$/.test(u.pathname),
    ),
  ).toBe(false);
  expect(f.writes).toHaveLength(0);
  expect(f.errors).toHaveLength(0);
});
