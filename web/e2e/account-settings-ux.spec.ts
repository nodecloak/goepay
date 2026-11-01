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
        uid: "1",
        registration_time_known: true,
        addtime: "2026-10-25 08:00:00",
        has_pwd: true,
        name: "本地商户",
        email: "account121@example.invalid",
        pay: "1",
      };
    else if (p.endsWith("/account/personal-settings"))
      data = {
        merchant_id: "1",
        qq: "",
        pay_minmoney: "",
        pay_maxmoney: "",
        remain_money: "",
        bounds_valid: true,
        fingerprint: "a".repeat(64),
        can_update: true,
      };
    else if (p.endsWith("/account/api-switches"))
      data = {
        merchant_id: "1",
        keylogin: 1,
        refund: 1,
        transfer: 0,
        configured: false,
        revision: "0",
        fingerprint: "b".repeat(64),
        can_update: true,
        transfer_granted: false,
      };
    else if (p.endsWith("/account/buyer-fees"))
      data = {
        merchant_id: "1",
        mode: 0,
        mode_feerate: "",
        configured: false,
        revision: "0",
        fingerprint: "c".repeat(64),
        can_update: true,
        partial_available: true,
      };
    else if (p.endsWith("/public/merchant-access"))
      data = { close_keylogin: 0, user_refund: 1 };
    else if (p.endsWith("/account/email"))
      data = {
        email: "",
        verified: false,
        email_enabled: false,
        requires: "password",
        fingerprint: "d".repeat(64),
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
      body: JSON.stringify({ code: 0, message: "", data }),
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
  test(`account sections, independent settings links and own login-log dialog work together at ${width}px`, async ({
    page,
  }, info) => {
    await page.setViewportSize({ width, height: 1000 });
    const f = await fixture(page);
    await expect(
      page.getByRole("heading", { name: "联系方式与功能设置", exact: true }),
    ).toBeVisible();
    await expect(
      page.getByRole("navigation", { name: "消息与打印设置", exact: true }),
    ).toBeVisible();
    const shortcuts = page.getByRole("navigation", {
      name: "消息与打印设置",
      exact: true,
    });
    await expect(
      shortcuts.getByRole("link", { name: "消息提醒设置", exact: true }),
    ).toHaveAttribute("href", "/merchant/account/notice");
    await expect(
      shortcuts.getByRole("link", { name: "订单打印设置", exact: true }),
    ).toHaveAttribute("href", "/merchant/account/printing");
    await expect(
      page.locator(".notice-settings, .printing-settings"),
    ).toHaveCount(0);
    await expect(page.getByLabel("联系QQ", { exact: true })).toBeVisible();
    await expect(
      page.getByRole("combobox", { name: "手续费承担方", exact: true }),
    ).toBeVisible();
    await expect(
      page.getByRole("checkbox", { name: /开启密钥登录/ }),
    ).toBeVisible();
    expect(
      await page.evaluate(
        () => document.documentElement.scrollWidth <= innerWidth,
      ),
    ).toBe(true);
    const layout = await page
      .locator(".merchant-account-settings")
      .evaluate((root) => {
        const content = root.closest(".content")!.getBoundingClientRect();
        return {
          contentX: content.x,
          contentRight: content.right,
          cards: Array.from(root.children).map((e) => {
            const r = e.getBoundingClientRect();
            return {
              className: e.className,
              x: r.x,
              right: r.right,
              width: r.width,
            };
          }),
        };
      });
    for (const card of layout.cards) {
      expect(card.x, card.className).toBeGreaterThanOrEqual(layout.contentX);
      expect(card.right, card.className).toBeLessThanOrEqual(
        layout.contentRight,
      );
    }
    const profileBounds = await page
      .locator(".merchant-account-profile > .account-pages > .account-section")
      .boundingBox();
    const functionsBounds = await page
      .locator(".merchant-account-functions")
      .boundingBox();
    expect(profileBounds!.width).toBeCloseTo(functionsBounds!.width, 0);
    await info.attach("account-layout-geometry", {
      body: JSON.stringify(layout),
      contentType: "application/json",
    });
    await page.screenshot({
      path: info.outputPath("account-settings-viewport-" + width + ".png"),
    });
    await page.screenshot({
      path: info.outputPath("account-settings-layout-" + width + ".png"),
      fullPage: true,
    });
    expect(
      f.reads.some((u) => u.pathname === "/console/v1/merchant/account/notice"),
    ).toBe(false);
    expect(
      f.reads.some(
        (u) => u.pathname === "/console/v1/merchant/account/printing",
      ),
    ).toBe(false);
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
    await dialog.getByRole("button", { name: "下一页", exact: true }).click();
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
      dialog.getByRole("button", { name: "下一页", exact: true }),
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
      /\/account\/(login-log|notice|printing|personal-settings)$/.test(
        u.pathname,
      ),
    ),
  ).toBe(false);
  expect(f.writes).toHaveLength(0);
  expect(f.errors).toHaveLength(0);
});
