import { expect, test, type Page } from "@playwright/test";
const managed = (changes: Record<string, unknown> = {}) => ({
  id: "7",
  revision: "2",
  name: "原公众号",
  kind: "official_account",
  app_id: "wxsynthetic",
  enabled: false,
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
  ...changes,
});
async function fixture(
  page: Page,
  options: {
    write?: boolean;
    unknown?: boolean;
    configurationErrors?: boolean;
    rejectRotation?: boolean;
  } = {},
) {
  const writes: {
      path: string;
      method: string;
      body: Record<string, unknown>;
    }[] = [],
    receipts: string[] = [],
    reads: string[] = [],
    errors: string[] = [];
  const records = [
    managed(),
    managed({
      revision: "0",
      name: "旧资料",
      fingerprint: "",
      origin: "legacy_metadata",
      read_only: true,
      secret_configured: false,
    }),
    managed({
      id: "8",
      name: "进程导入资料",
      fingerprint: "",
      origin: "imported",
      read_only: true,
    }),
  ];
  let original: ReturnType<typeof managed> | undefined,
    receiptAvailable = false;
  page.on("pageerror", (e) => errors.push(e.message));
  await page.route("**/console/v1/**", async (route) => {
    const req = route.request(),
      url = new URL(req.url()),
      path = url.pathname;
    if (req.method() === "GET" && path.includes("/weixin-applications"))
      reads.push(path);
    let data: unknown = { items: [], total: 0 };
    if (path.endsWith("/session"))
      data = {
        user: {
          id: "1",
          name: "Synthetic Weixin Admin",
          role: "admin",
          admin_class: options.write === false ? "operator" : "super",
          permissions: ["PayWeixin"],
        },
        csrf_token: "synthetic-weixin-csrf",
      };
    else if (path.includes("/resources/weixin-apps"))
      throw Error("generic app resource must not be used");
    else if (path.endsWith("/weixin-applications/capabilities"))
      data = { can_write: options.write !== false, sources: [] };
    else if (path.includes("/weixin-applications/requests/")) {
      receipts.push(path);
      if (!receiptAvailable)
        return route.fulfill({
          status: 404,
          json: { code: -1, message: "原请求尚不存在" },
        });
      data = original;
    } else if (
      (path.endsWith("/weixin-applications") && req.method() === "POST") ||
      (path.endsWith("/weixin-applications/7") && req.method() === "PATCH")
    ) {
      expect(options.write).not.toBe(false);
      expect(req.headers()["x-csrf-token"]).toBe("synthetic-weixin-csrf");
      const body = req.postDataJSON();
      writes.push({ path, method: req.method(), body });
      expect(body.request_id).toMatch(/^[a-f0-9]{64}$/);
      const isNew = req.method() === "POST";
      if (isNew)
        expect(body).toMatchObject({
          enabled: false,
          source_channel_id: "0",
          merchant_ids: [],
          path: "",
          expected_source_fingerprint: "",
        });
      else
        expect(body).toMatchObject({
          expected_revision: "2",
          expected_fingerprint: "a".repeat(64),
          appsecret: options.rejectRotation
            ? "synthetic-requested-rotation-secret"
            : "",
        });
      if (options.rejectRotation && !isNew && writes.length === 1)
        return route.fulfill({
          status: 422,
          json: { code: -1, message: "资料需修正" },
        });
      const view = managed({
        id: isNew ? "9" : "7",
        revision: isNew ? "1" : "3",
        name: body.name,
        kind: body.kind,
        app_id: body.app_id,
        enabled: body.enabled,
        source_channel_id: body.source_channel_id,
        merchant_ids: body.merchant_ids,
        path: body.path,
        fingerprint: "c".repeat(64),
      });
      if (isNew) records.unshift(view);
      else
        Object.assign(
          records.find((r) => r.id === "7" && r.origin === "managed")!,
          view,
        );
      if (options.unknown) {
        original = structuredClone(view);
        return route.abort();
      }
      data = view;
    } else if (path.endsWith("/weixin-applications"))
      data = {
        items: records,
        total: records.length,
        can_write: options.write !== false,
        configuration_errors: options.configurationErrors
          ? [{ id: "8", message: "应用配置损坏，请检查原记录" }]
          : [],
      };
    return route.fulfill({ json: { code: 0, data } });
  });
  await page.goto("/admin/payapi/pay-weixin");
  await expect(
    page.locator('[data-app-origin="managed"][data-app-id="7"]'),
  ).toContainText("原公众号");
  return {
    writes,
    receipts,
    reads,
    errors,
    records,
    allowReceipt: () => {
      receiptAvailable = true;
      Object.assign(
        records.find((r) => r.id === "9")!,
        {
          name: "原请求之后的新版本",
          revision: "2",
          fingerprint: "d".repeat(64),
        },
      );
    },
  };
}

for (const width of [1280, 375])
  test(`managed Weixin draft and blank-secret edit preserve origin and scope at ${width}`, async ({
    page,
  }) => {
    await page.setViewportSize({ width, height: 1000 });
    const f = await fixture(page);
    expect(await page.locator("thead th").allTextContents()).toEqual([
      "ID",
      "类别",
      "名称",
      "APPID",
      "操作",
      "凭证",
      "状态",
      "来源",
    ]);
    await page.getByLabel("搜索应用", { exact: true }).fill("原公众号");
    await page.getByRole("button", { name: "查询", exact: true }).click();
    await expect(page.locator("tbody tr[data-app-id]")).toHaveCount(1);
    await page.getByRole("button", { name: "重置", exact: true }).click();
    await expect(page.locator("tbody tr[data-app-id]")).toHaveCount(3);
    expect(f.reads).toHaveLength(2);
    await page.getByRole("button", { name: "新增应用", exact: true }).click();
    const dialog = page.getByRole("dialog", { name: "新增应用", exact: true });
    await expect(dialog.getByLabel("类别", { exact: true })).toBeFocused();
    await expect(
      dialog.getByLabel("关联来源通道", { exact: true }),
    ).not.toBeVisible();
    await dialog.getByLabel("名称", { exact: true }).fill("本地加密草稿");
    await dialog.getByLabel("APPID", { exact: true }).fill("wxnewlocal");
    await dialog
      .getByLabel("APPSECRET", { exact: true })
      .fill("synthetic-secret-never-return");
    const box = await dialog.boundingBox();
    expect(box!.x).toBeGreaterThanOrEqual(0);
    expect(box!.x + box!.width).toBeLessThanOrEqual(width + 1);
    await dialog.getByRole("button", { name: "保存应用", exact: true }).click();
    await expect(dialog).toHaveCount(0);
    expect(f.writes).toHaveLength(1);
    expect(f.writes[0]!.body.appsecret).toBe("synthetic-secret-never-return");
    await expect(
      page.locator('[data-app-origin="managed"][data-app-id="9"]'),
    ).toContainText("待关联草稿");
    expect(await page.locator("body").innerHTML()).not.toContain(
      "synthetic-secret-never-return",
    );
    const row = page.locator('[data-app-origin="managed"][data-app-id="7"]');
    await row.getByRole("button", { name: "编辑", exact: true }).click();
    const edit = page.getByRole("dialog", { name: "编辑应用", exact: true });
    await expect(edit.getByLabel("APPSECRET", { exact: true })).toHaveValue("");
    await edit.getByLabel("名称", { exact: true }).fill("只改名称保留凭证");
    await edit.getByRole("button", { name: "保存应用", exact: true }).click();
    await expect(edit).toHaveCount(0);
    expect(f.writes).toHaveLength(2);
    expect(f.writes[1]!.body).toMatchObject({
      appsecret: "",
      source_channel_id: "0",
      merchant_ids: [],
      enabled: false,
    });
    const legacy = page.locator(
      '[data-app-origin="legacy_metadata"][data-app-id="7"]',
    );
    await legacy
      .getByRole("button", { name: "配置为新应用", exact: true })
      .click();
    const prefill = page.getByRole("dialog", { name: "新增应用", exact: true });
    await expect(prefill.getByLabel("APPID", { exact: true })).toHaveValue(
      "wxsynthetic",
    );
    await expect(prefill.getByLabel("APPSECRET", { exact: true })).toHaveValue(
      "",
    );
    await prefill.getByRole("button", { name: "取消", exact: true }).click();
    expect(f.writes).toHaveLength(2);
    expect(
      await page.evaluate(
        () => document.documentElement.scrollWidth <= innerWidth,
      ),
    ).toBe(true);
    expect(f.errors).toEqual([]);
  });

test("operator can read colliding origins while actual can_write false prevents every mutation", async ({
  page,
}) => {
  await page.setViewportSize({ width: 375, height: 1000 });
  const f = await fixture(page, { write: false, configurationErrors: true });
  await expect(page.locator("tbody tr")).toHaveCount(3);
  await expect(page.getByRole("alert")).toContainText(
    "应用 #8：应用配置损坏，请检查原记录",
  );
  await expect(
    page.getByRole("button", { name: "新增应用", exact: true }),
  ).toHaveCount(0);
  await expect(
    page.getByRole("button", { name: "编辑", exact: true }),
  ).toHaveCount(0);
  await expect(
    page.getByRole("button", { name: "配置为新应用", exact: true }),
  ).toHaveCount(0);
  await expect(
    page.locator('[data-app-origin="legacy_metadata"][data-app-id="7"]'),
  ).toContainText("旧资料");
  await page.getByLabel("搜索应用", { exact: true }).fill("旧资料");
  await page.getByRole("button", { name: "查询", exact: true }).click();
  await expect(page.locator("tbody tr[data-app-id]")).toHaveCount(1);
  await page
    .getByLabel("筛选类别", { exact: true })
    .selectOption("mini_program");
  await page.getByRole("button", { name: "查询", exact: true }).click();
  await expect(page.locator("tbody")).toContainText("没有符合条件的应用");
  await expect(page.getByRole("alert")).toContainText("应用 #8");
  await page.getByRole("button", { name: "重置", exact: true }).click();
  await expect(page.locator("tbody tr[data-app-id]")).toHaveCount(3);
  await expect(page.getByLabel("搜索应用", { exact: true })).toHaveValue("");
  await expect(page.getByLabel("筛选类别", { exact: true })).toHaveValue("");
  expect(f.reads).toHaveLength(2);
  expect(f.writes).toHaveLength(0);
  expect(f.errors).toEqual([]);
});

test("unknown app creation clears its password and uses only the original receipt until found", async ({
  page,
}) => {
  await page.setViewportSize({ width: 375, height: 1000 });
  const f = await fixture(page, { unknown: true });
  await page.getByRole("button", { name: "新增应用", exact: true }).click();
  const dialog = page.getByRole("dialog", { name: "新增应用", exact: true });
  await dialog.getByLabel("名称", { exact: true }).fill("未知请求草稿");
  await dialog.getByLabel("APPID", { exact: true }).fill("wxunknownlocal");
  await dialog
    .getByLabel("APPSECRET", { exact: true })
    .fill("synthetic-unknown-secret");
  await dialog.getByRole("button", { name: "保存应用", exact: true }).click();
  await expect(dialog).toContainText("结果尚未确认");
  await expect(dialog.getByLabel("APPSECRET", { exact: true })).toHaveValue("");
  await expect(
    dialog.getByRole("button", { name: "保存应用", exact: true }),
  ).toBeDisabled();
  await dialog.getByRole("button", { name: "查询原请求", exact: true }).click();
  await expect(dialog).toContainText("结果仍未确认");
  expect(f.writes).toHaveLength(1);
  const request = f.writes[0]!.body.request_id;
  expect(f.receipts).toEqual([
    `/console/v1/admin/weixin-applications/requests/${request}`,
  ]);
  f.allowReceipt();
  await dialog.getByRole("button", { name: "查询原请求", exact: true }).click();
  await expect(dialog).toHaveCount(0);
  await expect(
    page.locator('[data-app-origin="managed"][data-app-id="9"]'),
  ).toContainText("原请求之后的新版本");
  expect(f.writes).toHaveLength(1);
  expect(f.receipts).toHaveLength(2);
  expect(await page.locator("body").innerHTML()).not.toContain(
    "synthetic-unknown-secret",
  );
  expect(f.errors).toEqual([]);
});

test("a rejected credential replacement is cleared and cannot silently become a keep-old edit", async ({
  page,
}) => {
  await page.setViewportSize({ width: 375, height: 1000 });
  const f = await fixture(page, { rejectRotation: true });
  await page
    .locator('[data-app-origin="managed"][data-app-id="7"]')
    .getByRole("button", { name: "编辑", exact: true })
    .click();
  const dialog = page.getByRole("dialog", { name: "编辑应用", exact: true });
  await dialog
    .getByLabel("APPSECRET", { exact: true })
    .fill("synthetic-requested-rotation-secret");
  await dialog.getByLabel("名称", { exact: true }).fill("需修正名称");
  await dialog.getByRole("button", { name: "保存应用", exact: true }).click();
  await expect(dialog.getByLabel("APPSECRET", { exact: true })).toHaveValue("");
  await expect(dialog).toContainText("本次凭证尚未保存");
  expect(f.writes).toHaveLength(1);
  await dialog.getByLabel("名称", { exact: true }).fill("已修正名称");
  await dialog.getByRole("button", { name: "保存应用", exact: true }).click();
  await expect(dialog.getByRole("alert")).toContainText("重新输入APPSECRET");
  expect(f.writes).toHaveLength(1);
  await dialog
    .getByLabel("APPSECRET", { exact: true })
    .fill("synthetic-requested-rotation-secret");
  await dialog.getByRole("button", { name: "保存应用", exact: true }).click();
  await expect(dialog).toHaveCount(0);
  expect(f.writes).toHaveLength(2);
  expect(f.writes[1]!.body).toMatchObject({
    name: "已修正名称",
    appsecret: "synthetic-requested-rotation-secret",
  });
  expect(f.errors).toEqual([]);
});
