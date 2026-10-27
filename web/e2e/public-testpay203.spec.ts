import { expect, test, type BrowserContext, type Page } from "@playwright/test";
const csrf = "c".repeat(43);
const initial = {
  csrf_token: csrf,
  trade_no: "TP-PUBLIC203",
  money: "1.00",
  paytype: [{ id: "700203", name: "alipay", showname: "支付宝203" }],
  sitename: "公开测试203",
  captcha_open: 0,
};
const site = {
  sitename: "公开测试203",
  title: "公开测试203",
  keywords: "",
  description: "",
  orgname: "",
  email: "",
  kfqq: "",
  kfqq_url: "",
  wework_url: "",
  qqqun: "",
  login_subtitle: "",
  homepage: "0",
  homepage_url: "",
};
const ok = (data: unknown, status = 200, message = "") => ({
  status,
  contentType: "application/json",
  body: JSON.stringify({ code: status === 200 ? 0 : -1, data, message }),
});
async function fixture(
  page: Page,
  context: BrowserContext,
  baseURL: string | undefined,
  options: { captcha?: boolean; paid?: boolean; path?: string } = {},
) {
  const calls: { path: string; method: string; body: unknown }[] = [],
    external: string[] = [],
    errors: string[] = [];
  const origin = new URL(baseURL ?? "http://127.0.0.1:5173").origin;
  await context.route("**/*", (route) => {
    if (new URL(route.request().url()).origin !== origin) {
      external.push(route.request().url());
      return route.abort("blockedbyclient");
    }
    return route.continue();
  });
  page.on("pageerror", (error) => errors.push(error.message));
  await page.route("**/checkout/**", (route) =>
    route.fulfill({
      contentType: "text/html",
      body: "<h1>Controlled original checkout203</h1>",
    }),
  );
  await page.route("**/console/v1/**", async (route) => {
    const req = route.request(),
      u = new URL(req.url()),
      path = u.pathname;
    calls.push({
      path: path + u.search,
      method: req.method(),
      body: req.method() === "GET" ? undefined : req.postDataJSON(),
    });
    if (path.endsWith("/testpay/init"))
      return route.fulfill(
        ok({ ...initial, captcha_open: options.captcha ? 1 : 0 }),
      );
    if (path.endsWith("/testpay/submit"))
      return route.fulfill(
        ok({
          url:
            "/checkout/CONTROLLED203?expires=1999999999&token=" +
            "t".repeat(43),
        }),
      );
    if (path.endsWith("/testpay/success"))
      return route.fulfill(
        options.paid
          ? ok({ money: "1.23", sitename: "公开测试203" })
          : ok(null, 409, "原测试订单尚未核实支付"),
      );
    if (path.endsWith("/public/captcha"))
      return route.fulfill(ok(null, 503, "验证码服务不可用203"));
    if (path.endsWith("/public/site")) return route.fulfill(ok(site));
    if (path.endsWith("/public/layout"))
      return route.fulfill(ok({ zhuce: "", footer: "", format: "html" }));
    return route.fulfill(ok(null, 401, "console credentials are unrelated203"));
  });
  await page.goto(options.path ?? "/test");
  const panel = page.getByRole("main", { name: "测试支付", exact: true });
  return { panel, calls, external, errors };
}
for (const width of [1440, 375])
  test(`public TestPay203 has original readonly fields and keyboard-only explicit payment at ${width}px`, async ({
    page,
    context,
    baseURL,
  }, info) => {
    await page.setViewportSize({ width, height: 1000 });
    const f = await fixture(page, context, baseURL, {
      path: width === 375 ? "/test/" : "/test",
    });
    await expect(f.panel.getByLabel("商户订单号", { exact: true })).toHaveValue(
      "TP-PUBLIC203",
    );
    await expect(
      f.panel.getByLabel("商户订单号", { exact: true }),
    ).toHaveAttribute("readonly", "");
    await expect(f.panel.getByLabel("商品名称", { exact: true })).toHaveValue(
      "支付测试",
    );
    await expect(f.panel.locator('[name="money"]')).toHaveValue("1.00");
    expect(
      f.calls.some((call) =>
        /\/(admin|merchant)\/(session|login)$/.test(call.path),
      ),
    ).toBe(false);
    expect(f.calls.filter((call) => call.method !== "GET")).toEqual([]);
    await f.panel.locator('[name="money"]').fill("1.23");
    await f.panel.locator('[name="money"]').press("Tab");
    const pay = f.panel.getByRole("button", { name: "支付宝203", exact: true });
    await expect(pay).toBeFocused();
    expect(
      await page.evaluate(
        () => document.documentElement.scrollWidth <= innerWidth,
      ),
    ).toBe(true);
    await f.panel.screenshot({
      path: info.outputPath(`public-testpay203-${width}.png`),
    });
    await pay.press("Enter");
    await expect(
      page.getByRole("heading", { name: "Controlled original checkout203" }),
    ).toBeVisible();
    expect(f.calls.filter((call) => call.method === "POST")).toEqual([
      {
        path: "/console/v1/public/testpay/submit",
        method: "POST",
        body: { money: "1.23", typeid: "700203", csrf_token: csrf },
      },
    ]);
    expect(
      await page.evaluate(() => localStorage.length + sessionStorage.length),
    ).toBe(0);
    expect(f.external).toEqual([]);
    expect(f.errors).toEqual([]);
  });
test("public TestPay203 uses the trusted success read instead of trusting the ok query", async ({
  page,
  context,
  baseURL,
}) => {
  const f = await fixture(page, context, baseURL, {
    path: "/test?ok=1&trade_no=CALLER203",
  });
  await expect(f.panel.locator('[name="money"]')).toHaveValue("1.00");
  await expect(f.panel).not.toContainText("订单已支付成功");
  expect(
    f.calls.some(
      (call) =>
        call.path === "/console/v1/public/testpay/success?trade_no=CALLER203",
    ),
  ).toBe(true);
  expect(f.calls.filter((call) => call.method !== "GET")).toEqual([]);
  expect(f.external).toEqual([]);
});
test("public TestPay203 displays only a server-confirmed paid amount and returns to a fresh test", async ({
  page,
  context,
  baseURL,
}) => {
  const f = await fixture(page, context, baseURL, {
    paid: true,
    path: "/test?ok=1&trade_no=CONTROLLED-PAID203",
  });
  await expect(
    f.panel.getByRole("heading", { name: "订单已支付成功！" }),
  ).toBeVisible();
  await expect(f.panel).toContainText("支付金额：¥ 1.23");
  expect(
    f.calls.filter((call) => call.path.endsWith("/testpay/init")),
  ).toHaveLength(0);
  await f.panel.getByRole("button", { name: "返回测试", exact: true }).click();
  await expect(f.panel.locator('[name="money"]')).toHaveValue("1.00");
  expect(
    f.calls.filter((call) => call.path.endsWith("/testpay/init")),
  ).toHaveLength(1);
  expect(f.calls.filter((call) => call.method !== "GET")).toEqual([]);
});
test("public TestPay203 cannot proceed when CAPTCHA is required but unavailable", async ({
  page,
  context,
  baseURL,
}) => {
  const f = await fixture(page, context, baseURL, { captcha: true });
  await expect(f.panel.getByRole("alert")).toContainText("验证码");
  await expect(
    f.panel.getByRole("button", { name: "支付宝203", exact: true }),
  ).toBeDisabled();
  expect(
    f.calls
      .filter((call) => call.path.includes("/public/captcha"))
      .map((call) => call.path),
  ).toEqual(["/console/v1/public/captcha?purpose=testpay"]);
  expect(f.calls.filter((call) => call.method !== "GET")).toEqual([]);
  expect(f.external).toEqual([]);
});
