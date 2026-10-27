import { test, expect, type Page } from "@playwright/test";
import { publicSiteFixture191 } from "./fixtures/site191";

async function cashier(page: Page, loseApply = false, losePrepare = false) {
  const sends: { path: string; body: any }[] = [];
  let result: any = null;
  await page.route("**/console/v1/**", async (route) => {
    const site = publicSiteFixture191(route);
    if (site) return site;
    const req = route.request(),
      p = new URL(req.url()).pathname;
    const reply = (data: any, status = 200) =>
      route.fulfill({
        status,
        contentType: "application/json",
        body: JSON.stringify({ code: status === 200 ? 0 : -1, data }),
      });
    if (p.endsWith("/session"))
      return reply({
        user: { id: "1", name: "本地商户", role: "merchant" },
        csrf_token: "scan192-csrf",
      });
    if (p.endsWith("/scanpay/capabilities"))
      return reply({
        platform_open: true,
        can_prepare: true,
        pay_types: ["wxpay", "alipay"],
        max_input_money: "999999.99",
      });
    if (req.method() === "POST") {
      expect(req.headers()["x-csrf-token"]).toBe("scan192-csrf");
      const body = req.postDataJSON();
      sends.push({ path: p, body });
      if (p.endsWith("/prepare")) {
        expect(Object.keys(body).sort()).toEqual([
          "auth_code",
          "money",
          "remark",
          "request_id",
        ]);
        result = {
          request_id: body.request_id,
          trade_no: "192001",
          money: body.money,
          charge_money: "1.25",
          net_money: "1.23",
          fee_money: "0.02",
          remark: body.remark,
          type: "wxpay",
          state: "prepared",
          can_apply: true,
          can_query: false,
        };
        if (losePrepare) {
          losePrepare = false;
          return route.abort("failed");
        }
        return reply(result);
      }
      if (p.endsWith("/apply")) {
        expect(body).toEqual({ auth_code: "134567890123456789" });
        result = {
          ...result,
          state: "paid",
          can_apply: false,
          can_query: true,
        };
        if (loseApply) {
          loseApply = false;
          return route.abort("failed");
        }
        return reply(result);
      }
      if (p.endsWith("/close")) {
        result = { ...result, state: "closed", can_apply: false };
        return reply(result);
      }
    }
    if (result && p.endsWith("/intents/" + result.request_id))
      return reply(result);
    return reply(null, 404);
  });
  return sends;
}
for (const width of [375, 1280])
  test(`original cashier keypad remark and actual-charge confirmation ${width}`, async ({
    page,
  }) => {
    await page.setViewportSize({ width, height: 900 });
    const sends = await cashier(page);
    await page.goto("/merchant/scanpay");
    await expect(
      page.getByRole("heading", { name: "付款码收款", exact: true }),
    ).toBeVisible();
    for (const key of ["1", ".", "2", "3"])
      await page
        .getByRole("button", { name: "金额键 " + key, exact: true })
        .click();
    await expect(page.getByLabel("收款金额", { exact: true })).toHaveValue(
      "1.23",
    );
    await page.getByRole("button", { name: "添加备注", exact: true }).click();
    await page.getByLabel("收款备注", { exact: true }).fill("取消的备注");
    await page.getByRole("button", { name: "取消备注", exact: true }).click();
    await page.getByRole("button", { name: "添加备注", exact: true }).click();
    await page.getByLabel("收款备注", { exact: true }).fill("本地收款");
    await page.getByRole("button", { name: "确认备注", exact: true }).click();
    await page.getByRole("button", { name: "扫一扫收款", exact: true }).click();
    await page.getByLabel("付款码", { exact: true }).fill("134567890123456789");
    await page.getByRole("button", { name: "核对收款", exact: true }).click();
    await expect(
      page.getByText("付款人实付 ¥1.25", { exact: true }),
    ).toBeVisible();
    expect(sends).toHaveLength(1);
    expect(sends[0].body).toMatchObject({ money: "1.23", remark: "本地收款" });
    await page
      .getByRole("button", { name: "确认收款 ¥1.25", exact: true })
      .click();
    await expect(
      page.getByRole("heading", { name: "收款成功", exact: true }),
    ).toBeVisible();
    expect(sends).toHaveLength(2);
    expect(
      await page.evaluate(
        () =>
          Object.values(sessionStorage).join() +
          Object.values(localStorage).join(),
      ),
    ).not.toContain("134567890123456789");
    expect(page.url()).not.toContain("134567890123456789");
    expect(
      await page.evaluate(
        () => document.documentElement.scrollWidth <= innerWidth,
      ),
    ).toBe(true);
  });
test("lost cashier apply response recovers the same request without scanning or charging again", async ({
  page,
}) => {
  const sends = await cashier(page, true);
  await page.goto("/merchant/scanpay");
  await page.getByRole("button", { name: "金额键 1", exact: true }).click();
  await page.getByRole("button", { name: "扫一扫收款", exact: true }).click();
  await page.getByLabel("付款码", { exact: true }).fill("134567890123456789");
  await page.getByRole("button", { name: "核对收款", exact: true }).click();
  await page
    .getByRole("button", { name: "确认收款 ¥1.25", exact: true })
    .click();
  await expect(
    page.getByRole("button", { name: "恢复原收款结果", exact: true }),
  ).toBeVisible();
  await page.reload();
  await page
    .getByRole("button", { name: "恢复原收款结果", exact: true })
    .click();
  await expect(
    page.getByRole("heading", { name: "收款成功", exact: true }),
  ).toBeVisible();
  expect(sends.map((x) => x.path.split("/").pop())).toEqual([
    "prepare",
    "apply",
  ]);
});

test("original onecode entry opens a token-free cashier QR instead of adding sidebar menu", async ({
  page,
}) => {
  const sends = await cashier(page);
  await page.goto("/merchant/onecode");
  await page.getByRole("button", { name: "付款码收款", exact: true }).click();
  await expect(
    page.getByRole("img", { name: "付款码收款页面二维码", exact: true }),
  ).toBeVisible();
  const link = page.getByRole("link", { name: "打开收款页面", exact: true });
  expect(await link.getAttribute("href")).toBe("/merchant/scanpay");
  expect(sends).toHaveLength(0);
});

test("lost preparation can be explicitly closed before starting any replacement charge", async ({
  page,
}) => {
  const sends = await cashier(page, false, true);
  await page.goto("/merchant/scanpay");
  await page.getByRole("button", { name: "金额键 1", exact: true }).click();
  await page.getByRole("button", { name: "扫一扫收款", exact: true }).click();
  await page.getByLabel("付款码", { exact: true }).fill("134567890123456789");
  await page.getByRole("button", { name: "核对收款", exact: true }).click();
  await expect(
    page.getByRole("button", { name: "取消本次收款", exact: true }),
  ).toBeVisible();
  await page.getByRole("button", { name: "取消本次收款", exact: true }).click();
  await expect(
    page.getByRole("heading", { name: "已取消收款", exact: true }),
  ).toBeVisible();
  expect(sends.map((x) => x.path.split("/").pop())).toEqual([
    "prepare",
    "close",
  ]);
});

test("cashier cannot send when a durable browser request reference cannot be saved", async ({
  page,
}) => {
  await page.addInitScript(() => {
    const original = Storage.prototype.setItem;
    Storage.prototype.setItem = function (key, value) {
      if (key.startsWith("goepay.scan192."))
        throw new DOMException(
          "synthetic storage denied",
          "QuotaExceededError",
        );
      return original.call(this, key, value);
    };
  });
  const sends = await cashier(page);
  await page.goto("/merchant/scanpay");
  await page.getByRole("button", { name: "金额键 1", exact: true }).click();
  await page.getByRole("button", { name: "扫一扫收款", exact: true }).click();
  await page.getByLabel("付款码", { exact: true }).fill("134567890123456789");
  await page.getByRole("button", { name: "核对收款", exact: true }).click();
  await expect(page.getByRole("alert").last()).toContainText(
    "无法安全保存原请求号",
  );
  expect(sends).toHaveLength(0);
});
