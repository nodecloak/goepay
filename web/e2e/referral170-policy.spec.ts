import { test, expect } from "@playwright/test";
import policies from "./referral170-policies.json" with { type: "json" };
for (const width of [1440, 375])
  test(`referral170 current common policy conditions and examples ${width}`, async ({
    page,
  }, info) => {
    await page.setViewportSize({ width, height: 1000 });
    let index = 0;
    let version = 0;
    const bearer = "rp_" + "T".repeat(43);
    const writes: string[] = [],
      errors: string[] = [];
    page.on("pageerror", (e) => errors.push(e.message));
    await page.route("**/*", (route) => {
      const r = route.request(),
        u = new URL(r.url());
      if (!["127.0.0.1", "localhost"].includes(u.hostname))
        return route.abort();
      if (!u.pathname.startsWith("/console/v1/")) return route.continue();
      if (r.method() !== "GET") writes.push(u.pathname);
      let data: unknown = { items: [], next_cursor: "" };
      if (u.pathname.endsWith("/session"))
        data = {
          user: { id: "1", name: "本地商户170", role: "merchant" },
          csrf_token: "synthetic170",
        };
      else if (u.pathname.endsWith("/referral/overview"))
        data = {
          invited_count: "1",
          issued_amount: "9007199254740993",
          voided_amount: "0",
          recovery_pending_amount: "0",
          today_issued: "0",
          yesterday_issued: "0",
          daily_queue_available: true,
        };
      else if (u.pathname.endsWith("/referral/token"))
        data = {
          id: String(version),
          merchant_id: "1",
          version: String(version),
          enabled: version > 0,
          masked: version ? "rp_****TTTT" : "",
          expires_at: version
            ? new Date(Date.now() + 86400000).toISOString()
            : undefined,
        };
      else if (u.pathname.endsWith("/referral/token/rotate")) {
        expect(r.postDataJSON()).toEqual({ expected: "0" });
        version++;
        data = {
          id: "1",
          merchant_id: "1",
          version: "1",
          enabled: true,
          masked: "rp_****TTTT",
          token: bearer,
          expires_at: new Date(Date.now() + 86400000).toISOString(),
        };
      } else if (u.pathname.endsWith("/referral/current-policy"))
        data = policies[index];
      return route.fulfill({
        status: 200,
        contentType: "application/json",
        body: JSON.stringify({ code: 0, data }),
      });
    });
    await page.goto("/merchant/invite");
    const card = page.getByRole("region", {
      name: "当前邀请返现政策",
      exact: true,
    });
    await expect(card).toBeVisible();
    const layout = await page.evaluate(() => ({
      scrollX,
      documentWidth: document.documentElement.scrollWidth,
      width: innerWidth,
      rects: Array.from(
        document.querySelectorAll(
          "main,.referral,.current-policy,aside,.console-shell,.table-scroll",
        ),
      ).map((e) => ({
        element: e.tagName,
        class: e.className,
        x: e.getBoundingClientRect().x,
        width: e.getBoundingClientRect().width,
        scrollWidth: e.scrollWidth,
        overflowX: getComputedStyle(e).overflowX,
      })),
    }));
    await info.attach("referral170-layout.json", {
      body: JSON.stringify(layout, null, 2),
      contentType: "application/json",
    });
    if (width === 1440) {
      // The 200px sidebar is the UI kit's deliberate width; assert the policy
      // card clears the sidebar's right edge (no overlap) rather than a stale
      // hard-coded 230.
      const sidebarRight = await page
        .locator(".console-sidebar")
        .evaluate((el) => el.getBoundingClientRect().right);
      expect((await card.boundingBox())!.x).toBeGreaterThanOrEqual(
        sidebarRight,
      );
    }
    await expect(card).toContainText("33.33%");
    await expect(card).toContainText("100.00 元手续费");
    await expect(card).toContainText("33.33 元");
    await expect(card).toContainText("真实订单预估：无法估算");
    await expect(
      page.getByText("90071992547409.93 元", { exact: true }),
    ).toBeVisible();
    await page.screenshot({
      path: info.outputPath(`referral170-${width}-active.png`),
      fullPage: true,
      animations: "disabled",
    });
    if (width === 1440)
      await info.attach("referral170-after-screenshot.json", {
        body: JSON.stringify(
          await page.evaluate(() => ({
            width: innerWidth,
            height: innerHeight,
            scrollX,
            main: Array.from(
              document.querySelectorAll(
                ".main-content,.referral,.current-policy",
              ),
            ).map((e) => ({
              class: e.className,
              x: e.getBoundingClientRect().x,
              width: e.getBoundingClientRect().width,
              marginLeft: getComputedStyle(e).marginLeft,
            })),
          })),
        ),
        contentType: "application/json",
      });
    for (const item of [
      { i: 1, text: "取决于付款商户条件" },
      { i: 2, text: "手续费利润" },
      { i: 4, text: "当前政策无法确认" },
      { i: 5, text: "未开启邀请返现" },
      { i: 6, text: "当前未开启手续费预算" },
    ]) {
      index = item.i;
      await card
        .getByRole("button", { name: "刷新当前政策", exact: true })
        .click();
      await expect(card).toContainText(item.text);
      if (item.i === 2) await expect(card).toContainText("当前有效会员组");
    }
    await expect(card).toContainText("不产生奖励");
    await page.screenshot({
      path: info.outputPath(`referral170-${width}-disabled.png`),
      fullPage: true,
      animations: "disabled",
    });
    expect(writes).toEqual([]);
    await expect(
      page.getByRole("region", { name: "每日待发奖励", exact: true }),
    ).toBeVisible();
    await page
      .getByRole("button", { name: "发行或轮换推广令牌", exact: true })
      .click();
    expect(writes).toEqual([]);
    await page
      .getByRole("button", { name: "确认轮换并作废旧链接", exact: true })
      .click();
    await expect(page.getByLabel("本次推广链接")).toHaveAttribute(
      "type",
      "password",
    );
    expect(writes).toEqual(["/console/v1/merchant/referral/token/rotate"]);
    expect(
      await page.evaluate(
        () => JSON.stringify(localStorage) + JSON.stringify(sessionStorage),
      ),
    ).not.toContain(bearer);
    await page
      .getByRole("button", { name: "重新读取邀请资料", exact: true })
      .click();
    await expect(page.getByLabel("本次推广链接")).toHaveCount(0);
    expect(writes).toHaveLength(1);
    expect(errors).toEqual([]);
    expect(
      await page.evaluate(
        () => document.documentElement.scrollWidth > innerWidth,
      ),
    ).toBe(false);
  });
