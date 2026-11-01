import { test, expect } from "@playwright/test";
import type { Page, TestInfo } from "@playwright/test";

async function typedWeixinFlow(page: Page, testInfo: TestInfo, width: number) {
  const writes: string[] = [],
    reads: string[] = [],
    baseID = "9007199254740993";
  const items = Array.from({ length: 60 }, (_, index) => ({
    id: String(BigInt(baseID) + BigInt(index)),
    revision: "2",
    name:
      index === 0
        ? "测试公众号"
        : `${index < 30 ? "测试公众号" : "测试小程序"} ${index + 1}`,
    kind: index < 30 ? "official_account" : "mini_program",
    app_id:
      index < 30
        ? `wx-public-synthetic-${index + 1}`
        : `wx-mini-synthetic-${index + 1}`,
    enabled: true,
    archived: false,
    source_channel_id: "0",
    merchant_ids: [],
    path: "",
    fingerprint: "a".repeat(64),
    secret_configured: true,
    draft: true,
    execution_ready: false,
    origin: "managed",
    read_only: false,
  }));
  await page.route("**/console/v1/**", async (route) => {
    const request = route.request(),
      url = new URL(request.url()),
      path = url.pathname;
    const ok = (data: unknown) =>
      route.fulfill({ status: 200, json: { code: 0, data } });
    if (request.method() !== "GET") {
      writes.push(path);
      return route.abort();
    }
    if (path.endsWith("/session"))
      return ok({
        user: { id: "1", name: "Test", role: "admin", admin_class: "super" },
        csrf_token: "synthetic",
      });
    if (path.includes("/resources/weixin-apps"))
      throw Error("dedicated Weixin page must not read the generic resource");
    if (path.endsWith("/weixin-applications/capabilities")) {
      expect(url.search).toBe("");
      reads.push(path);
      return ok({ can_write: true, sources: [] });
    }
    if (path.endsWith("/weixin-applications")) {
      expect(url.search).toBe("");
      reads.push(path);
      return ok({
        items,
        total: items.length,
        can_write: true,
        configuration_errors: [],
      });
    }
    return ok({ items: [], total: 0 });
  });
  await page.goto("/admin/payapi/pay-weixin");
  await expect(page.getByRole("columnheader")).toHaveText([
    "ID",
    "类别",
    "名称",
    "APPID",
    "操作",
    "凭证",
    "状态",
    "来源",
  ]);
  await expect
    .poll(
      async () =>
        (await page.locator("tbody tr").first().boundingBox())?.height ?? 999,
    )
    .toBeLessThan(100);
  await expect(
    page.locator("tbody tr").first().locator("td").first(),
  ).toHaveText(baseID);
  await page
    .getByRole("textbox", { name: "搜索应用", exact: true })
    .fill("wx-public");
  await page
    .getByRole("combobox", { name: "筛选类别", exact: true })
    .selectOption("official_account");
  expect(reads).toHaveLength(2);
  await page.getByRole("button", { name: "查询", exact: true }).click();
  await expect(page.locator(".pa-pager-total")).toHaveText("共 30 条");
  await page.getByRole("button", { name: "2", exact: true }).click();
  await expect(page.locator("tbody tr[data-app-id]")).toHaveCount(10);
  await expect(
    page.locator("tbody tr").first().locator("td").first(),
  ).toHaveText("9007199254741013");
  const before = reads.length;
  await page.getByRole("button", { name: "重置", exact: true }).click();
  await expect(
    page.getByRole("textbox", { name: "搜索应用", exact: true }),
  ).toHaveValue("");
  await expect(
    page.getByRole("combobox", { name: "筛选类别", exact: true }),
  ).toHaveValue("");
  await expect(page.locator('.pa-pager [aria-current="page"]')).toHaveText("1");
  await expect(page.locator("tbody tr[data-app-id]")).toHaveCount(20);
  expect(reads).toHaveLength(before);
  await expect(
    page.getByRole("checkbox", { name: "选择记录 " + baseID, exact: true }),
  ).toHaveCount(0);
  await expect(page.getByRole("button", { name: /批量删除/ })).toHaveCount(0);
  const row = page.locator(
    `[data-app-origin="managed"][data-app-id="${baseID}"]`,
  );
  await row.getByRole("button", { name: "归档", exact: true }).click();
  const archive = page.getByRole("dialog", { name: "归档应用", exact: true });
  await expect(archive).toContainText("历史记录继续保留");
  await archive.getByRole("button", { name: "取消", exact: true }).click();
  await expect(archive).toHaveCount(0);
  await row.getByRole("button", { name: "编辑", exact: true }).click();
  const dialog = page.getByRole("dialog", { name: "编辑应用", exact: true });
  await expect(
    dialog.getByRole("checkbox", { name: "启用资料", exact: true }),
  ).toBeChecked();
  await dialog
    .getByRole("checkbox", { name: "启用资料", exact: true })
    .uncheck();
  await expect(
    dialog.getByRole("checkbox", { name: "启用资料", exact: true }),
  ).not.toBeChecked();
  await expect(dialog.getByLabel("APPSECRET", { exact: true })).toHaveValue("");
  await dialog.getByRole("button", { name: "取消", exact: true }).click();
  await expect(dialog).toHaveCount(0);
  await row.getByRole("button", { name: "编辑", exact: true }).click();
  await expect(
    dialog.getByRole("checkbox", { name: "启用资料", exact: true }),
  ).toBeChecked();
  await dialog.getByRole("button", { name: "取消", exact: true }).click();
  expect(writes).toEqual([]);
  expect(reads).toHaveLength(before);
  expect(
    await page.evaluate(
      () => document.documentElement.scrollWidth <= innerWidth,
    ),
  ).toBe(true);
  await page.screenshot({
    path: testInfo.outputPath(`weixin-apps-${width}.png`),
    fullPage: true,
  });
}

for (const resource of ["domains", "weixin-apps"] as const)
  for (const width of [375, 1280])
    test(`${resource} filters and local editor at ${width}`, async ({
      page,
    }, testInfo) => {
      await page.setViewportSize({ width, height: 900 });
      const domain = resource === "domains";
      if (!domain) {
        await typedWeixinFlow(page, testInfo, width);
        return;
      }
      const writes: string[] = [],
        queries: URLSearchParams[] = [];
      const item = {
        id: "9007199254740993",
        merchant_id: "9007199254740995",
        domain: "shop.example.invalid",
        approved: true,
        enabled: false,
        created_at: "2026-11-01T01:02:03Z",
        name: "测试公众号",
        app_id: "wx-public-synthetic",
        kind: "official_account",
      };
      const fields = domain
        ? [
            { name: "merchant_id", label: "商户 ID", type: "string" },
            { name: "domain", label: "域名", type: "string" },
            { name: "approved", label: "审核通过", type: "boolean" },
            { name: "enabled", label: "启用", type: "boolean" },
            {
              name: "created_at",
              label: "添加时间",
              type: "string",
              read_only: true,
            },
          ]
        : [
            { name: "name", label: "名称", type: "string" },
            { name: "app_id", label: "App ID", type: "string" },
            {
              name: "kind",
              label: "类别",
              type: "select",
              options: [{ value: "official_account", label: "公众号" }],
            },
            { name: "enabled", label: "启用资料", type: "boolean" },
          ];
      const filters = domain
        ? [
            {
              param: "merchant_id",
              label: "商户号",
              type: "positive_id",
              options: [],
            },
            {
              param: "approved",
              label: "状态",
              options: [
                { value: "", label: "全部状态" },
                { value: "true", label: "已通过" },
              ],
            },
            {
              param: "enabled",
              label: "启用",
              options: [{ value: "", label: "全部" }],
            },
          ]
        : [
            {
              param: "kind",
              label: "类别",
              options: [
                { value: "", label: "全部类别" },
                { value: "official_account", label: "公众号" },
              ],
            },
          ];
      await page.route("**/console/v1/**", async (route) => {
        const request = route.request(),
          url = new URL(request.url()),
          path = url.pathname;
        const ok = (data: unknown) =>
          route.fulfill({ status: 200, json: { code: 0, data } });
        if (request.method() !== "GET") {
          writes.push(path);
          return route.abort();
        }
        if (path.endsWith("/session"))
          return ok({
            user: {
              id: "1",
              name: "Test",
              role: "admin",
              admin_class: "super",
            },
            csrf_token: "synthetic",
          });
        if (path.endsWith("/schema"))
          return ok({
            title: domain ? "授权域名" : "公众号小程序",
            fields,
            filters,
            permissions: { create: true, update: true, delete: true },
            batch_delete: true,
          });
        if (path.endsWith("/" + item.id)) return ok(item);
        if (path.endsWith("/resources/" + resource)) {
          queries.push(url.searchParams);
          return ok({ items: [item], total: 60 });
        }
        return ok({ items: [], total: 0 });
      });
      await page.goto(
        domain ? "/admin/merchant/domain" : "/admin/payapi/pay-weixin",
      );
      await expect(page.getByRole("columnheader")).toHaveText(
        domain
          ? ["", "ID", "商户号", "域名", "状态", "添加时间", "操作"]
          : ["", "ID", "类别", "名称", "APPID", "操作"],
      );
      // Long decimal IDs must stay on one line rather than making a row
      // hundreds of pixels tall on a narrow screen.
      await expect
        .poll(
          async () =>
            (await page.locator("tbody tr").first().boundingBox())?.height ??
            999,
        )
        .toBeLessThan(100);
      await page
        .getByPlaceholder(domain ? "要搜索的域名" : "名称/APPID", {
          exact: true,
        })
        .fill(domain ? "shop.example.invalid" : "wx-public");
      if (domain)
        await page
          .getByRole("textbox", { name: "商户号", exact: true })
          .fill("9007199254740995");
      await page
        .getByRole("button", { name: domain ? "状态" : "类别", exact: true })
        .click();
      await page
        .getByRole("option", {
          name: domain ? "已通过" : "公众号",
          exact: true,
        })
        .click();
      await expect
        .poll(() => queries.at(-1)?.get(domain ? "approved" : "kind"))
        .toBe(domain ? "true" : "official_account");
      if (domain)
        expect(queries.at(-1)?.get("merchant_id")).toBe("9007199254740995");
      await page.getByRole("button", { name: "2", exact: true }).click();
      await expect.poll(() => queries.at(-1)?.get("page")).toBe("2");
      await page
        .getByRole("checkbox", { name: "选择记录 " + item.id, exact: true })
        .check();
      const before = queries.length;
      await page.getByRole("button", { name: "重置", exact: true }).click();
      await expect.poll(() => queries.length).toBe(before + 1);
      expect(queries.at(-1)?.get("page")).toBe("1");
      expect(queries.at(-1)?.get("q")).toBe("");
      expect(queries.at(-1)?.has(domain ? "merchant_id" : "kind")).toBe(false);
      await expect(
        page.getByRole("checkbox", {
          name: "选择记录 " + item.id,
          exact: true,
        }),
      ).not.toBeChecked();
      await page.getByRole("button", { name: "编辑", exact: true }).click();
      const dialog = page.getByRole("dialog");
      await expect(dialog).toBeVisible();
      await expect(dialog.locator("#field-enabled")).toBeVisible();
      await dialog.getByRole("button", { name: "取消", exact: true }).click();
      await expect(dialog).not.toBeVisible();
      expect(writes).toEqual([]);
      await page.screenshot({
        path: testInfo.outputPath(`${resource}-${width}.png`),
        fullPage: true,
      });
    });
