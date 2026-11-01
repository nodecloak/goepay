import { test, expect } from "@playwright/test";

for (const resource of ["domains", "weixin-apps"] as const)
  for (const width of [375, 1280])
    test(`${resource} filters and local editor at ${width}`, async ({
      page,
    }, testInfo) => {
      await page.setViewportSize({ width, height: 900 });
      const domain = resource === "domains";
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
