import { expect, test } from "@playwright/test";
import { publicSiteFixture191 } from "./fixtures/site191";
import { closedLoginCaptchaFixture205 } from "./fixtures/login-captcha205";

for (const width of [1280, 375]) {
  test(`recharge shows the current account balance and cancels its draft without writes at ${width}`, async ({
    page,
  }) => {
    await page.setViewportSize({ width, height: 1000 });
    const writes: string[] = [];
    let failBalance = false;
    await page.route("**/console/v1/**", (route) => {
      const request = route.request(),
        path = new URL(request.url()).pathname;
      if (request.method() !== "GET") {
        writes.push(path);
        return route.abort();
      }
      const site = publicSiteFixture191(route);
      if (site) return site;
      const captcha = closedLoginCaptchaFixture205(route);
      if (captcha) return captcha;
      const ok = (data: unknown) => route.fulfill({ json: { code: 0, data } });
      if (path.endsWith("/session"))
        return ok({
          user: { id: "1000", name: "Synthetic merchant", role: "merchant" },
          csrf_token: "synthetic-recharge-224",
        });
      if (path.endsWith("/dashboard"))
        return failBalance
          ? route.fulfill({
              status: 503,
              json: { code: -1, message: "synthetic temporary read failure" },
            })
          : ok({ available_money: "90071992547409.93" });
      return ok({ items: [], total: 0 });
    });
    await page.goto("/merchant/account/recharge");
    const balance = page.getByRole("region", {
      name: "充值账户余额",
      exact: true,
    });
    await expect(balance.locator("dd")).toHaveText("90071992547409.93");
    await expect(balance).toContainText("当前余额（元）");
    const opener = page.getByRole("button", { name: "余额充值", exact: true });
    await opener.click();
    const dialog = page.getByRole("dialog", { name: "余额充值", exact: true });
    const amount = dialog.getByRole("textbox", {
      name: "金额（元）",
      exact: true,
    });
    await expect(amount).toBeFocused();
    await amount.fill("0.20");
    await dialog.getByRole("button", { name: "取消", exact: true }).click();
    await expect(dialog).toHaveCount(0);
    await expect(opener).toBeFocused();
    await opener.click();
    await expect(amount).toHaveValue("");
    await dialog.getByRole("button", { name: "取消", exact: true }).click();
    failBalance = true;
    await balance
      .getByRole("button", { name: "刷新余额", exact: true })
      .click();
    await expect(balance.locator("dd")).toHaveText("暂不可用");
    await expect(balance.getByRole("alert")).toContainText("余额读取失败");
    failBalance = false;
    await balance
      .getByRole("button", { name: "重新读取余额", exact: true })
      .click();
    await expect(balance.locator("dd")).toHaveText("90071992547409.93");
    expect(writes).toEqual([]);
    expect(
      await page.evaluate(
        () => document.documentElement.scrollWidth <= innerWidth,
      ),
    ).toBe(true);
  });
}
