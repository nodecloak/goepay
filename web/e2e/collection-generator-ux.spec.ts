import { test, expect, type Page } from "@playwright/test";
async function setup(page: Page, delegated = false) {
  const writes: any[] = [];
  let lose = false;
  await page.route("**/console/v1/**", async (route) => {
    const req = route.request(),
      p = new URL(req.url()).pathname;
    let data: any = { items: [], total: 0 };
    if (p.endsWith("/session"))
      data = {
        user: { id: "1", name: "merchant", role: "merchant" },
        csrf_token: "generator-csrf",
        delegated_by: delegated ? { id: "2", name: "admin" } : null,
      };
    else if (p.endsWith("/collection-generator") && req.method() === "GET")
      data = {
        recipient_name: "本地商户",
        domains: [{ origin: "http://127.0.0.1:8186", label: "默认收款域名" }],
        styles: [
          {
            id: "7",
            style: null,
            fields: { name: "classic", label: "标准风格", enabled: true },
          },
        ],
        latest: null,
      };
    else if (req.method() !== "GET") {
      expect(req.headers()["x-csrf-token"]).toBe("generator-csrf");
      const body = req.postDataJSON();
      writes.push(body);
      if (lose) {
        lose = false;
        return route.abort("failed");
      }
      data = {
        code_id: "42",
        recipient_name: body.recipient_name,
        style_id: body.style_id,
        origin: body.origin,
        fixed_amount_cents: body.amount ? "123" : "0",
        config_hash: "a".repeat(64),
        url:
          "http://127.0.0.1:8186/collect/42?mode=generated&config=" +
          "a".repeat(64) +
          "&token=synthetic",
        enabled: true,
        replayed: writes.length > 1,
        expires_at: null,
      };
    }
    return route.fulfill({
      contentType: "application/json",
      body: JSON.stringify({ code: 0, data }),
    });
  });
  return {
    writes,
    loseNext: () => {
      lose = true;
    },
  };
}
for (const width of [375, 1280])
  test(`merchant direct generator restores original fields and download ${width}`, async ({
    page,
  }, testInfo) => {
    await page.setViewportSize({ width, height: 950 });
    const f = await setup(page);
    await page.goto("/merchant/onecode");
    for (const label of [
      "收款方名称",
      "选择域名",
      "选择收款码风格",
      "自定义收款金额",
    ])
      await expect(page.getByLabel(label, { exact: true })).toBeVisible();
    expect(f.writes).toHaveLength(0);
    await page.getByLabel("收款方名称", { exact: true }).fill("本地收款");
    await page.getByLabel("自定义收款金额", { exact: true }).fill("1.23");
    await page.getByRole("button", { name: "生成收款码", exact: true }).click();
    await expect(
      page.getByRole("img", { name: "聚合收款二维码" }),
    ).toBeVisible();
    await page.screenshot({
      path: testInfo.outputPath("collection-generator-viewport.png"),
    });
    expect(f.writes).toHaveLength(1);
    expect(f.writes[0]).toMatchObject({
      recipient_name: "本地收款",
      origin: "http://127.0.0.1:8186",
      amount: "1.23",
    });
    await expect(
      page.getByText("长期有效，停用后失效", { exact: false }),
    ).toBeVisible();
    const download = page.waitForEvent("download");
    await page.getByRole("button", { name: "下载收款码", exact: true }).click();
    expect((await download).suggestedFilename()).toBe("collection-code-42.png");
    await page.getByLabel("自定义收款金额", { exact: true }).fill("2.00");
    await expect(page.getByRole("img", { name: "聚合收款二维码" })).toHaveCount(
      0,
    );
    expect(f.writes).toHaveLength(1);
    expect(
      await page.evaluate(
        () => document.documentElement.scrollWidth <= innerWidth,
      ),
    ).toBe(true);
  });
test("unknown generator response retains original intent across SPA navigation", async ({
  page,
}) => {
  const f = await setup(page);
  await page.goto("/merchant/onecode");
  await page.getByLabel("自定义收款金额", { exact: true }).fill("1.23");
  f.loseNext();
  await page.getByRole("button", { name: "生成收款码", exact: true }).click();
  await expect(
    page.getByRole("button", { name: "核对上次生成结果", exact: true }),
  ).toBeVisible();
  await page.evaluate(() => {
    history.pushState({}, "", "/merchant/order");
    dispatchEvent(new PopStateEvent("popstate"));
  });
  await expect(page.getByLabel("自定义收款金额", { exact: true })).toHaveCount(
    0,
  );
  await page.evaluate(() => {
    history.pushState({}, "", "/merchant/onecode");
    dispatchEvent(new PopStateEvent("popstate"));
  });
  await expect(
    page.getByRole("button", { name: "核对上次生成结果", exact: true }),
  ).toBeVisible();
  await page
    .getByRole("button", { name: "核对上次生成结果", exact: true })
    .click();
  await expect(page.getByRole("img", { name: "聚合收款二维码" })).toBeVisible();
  expect(f.writes).toHaveLength(2);
  expect(f.writes[1]).toEqual(f.writes[0]);
});
test("delegated session cannot generate a permanent code", async ({ page }) => {
  const f = await setup(page, true);
  await page.goto("/merchant/onecode");
  await expect(
    page.getByRole("button", { name: "生成收款码", exact: true }),
  ).toBeDisabled();
  expect(f.writes).toHaveLength(0);
});
