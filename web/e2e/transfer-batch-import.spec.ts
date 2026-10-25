import { test, expect } from "@playwright/test";
const ok = (data: unknown) => ({
  status: 200,
  contentType: "application/json",
  body: JSON.stringify({ code: 0, data }),
});
test("file preview is masked and requires explicit apply and existing reservation confirmation", async ({
  page,
}) => {
  let imports = 0,
    prepares = 0,
    executes = 0;
  const draft = {
    source_row: 2,
    serial: "1",
    account: "000123456789012345678",
    name: "Private Name",
    amount: "0.10",
    remark: "memo",
  };
  await page.route("**/console/v1/**", (route) => {
    const p = new URL(route.request().url()).pathname;
    if (p.endsWith("/session"))
      return route.fulfill(
        ok({
          user: { id: "1", role: "merchant", name: "local" },
          csrf_token: "csrf",
        }),
      );
    if (p.endsWith("/transfers/capabilities"))
      return route.fulfill(
        ok({
          writes_enabled: true,
          channels: [{ id: "7", types: ["alipay"] }],
        }),
      );
    if (p.endsWith("/transfer-batch-imports/preview")) {
      imports++;
      expect(route.request().headers()["x-csrf-token"]).toBe("csrf");
      expect(route.request().postDataJSON().format).toBe("csv");
      return route.fulfill(
        ok({
          merchant_id: "1",
          format: "csv",
          count: 1,
          total_cents: "10",
          rows: [
            {
              source_row: 2,
              serial: "1",
              account_masked: "0***78",
              name_masked: "P***me",
              amount: "0.10",
              has_remark: true,
            },
          ],
          draft_items: [draft],
        }),
      );
    }
    if (p.endsWith("/execute")) executes++;
    if (
      p.endsWith("/transfer-batches") &&
      route.request().method() === "POST"
    ) {
      prepares++;
      expect(route.request().postDataJSON().items[0].account).toBe(
        draft.account,
      );
      return route.abort("failed");
    }
    return route.fulfill(ok({ items: [], total: 0, next_before_id: "0" }));
  });
  await page.goto("/merchant/transfer/batches");
  await page.getByRole("button", { name: "新建批量付款", exact: true }).click();
  await page.getByLabel("导入文件").setInputFiles({
    name: "synthetic.csv",
    mimeType: "text/csv",
    buffer: Buffer.from(
      "序号,收款账号,收款人姓名,转账金额（元）,转账备注\n1,000123456789012345678,Private Name,0.10,memo\n",
    ),
  });
  const preview = page.getByRole("region", { name: "文件校验预览" });
  await expect(preview).toContainText("0***78");
  await expect(preview).not.toContainText(draft.account);
  await expect(preview).not.toContainText(draft.name);
  expect(imports).toBe(1);
  expect(prepares).toBe(0);
  expect(executes).toBe(0);
  await page.setViewportSize({ width: 375, height: 812 });
  expect(
    await page.evaluate(
      () => document.documentElement.scrollWidth <= innerWidth,
    ),
  ).toBe(true);
  await page.setViewportSize({ width: 1280, height: 900 });
  await page.getByLabel("导入通道").selectOption("7");
  await page
    .getByRole("button", { name: "应用到当前草稿", exact: true })
    .click();
  await expect(page.getByLabel("收款账号", { exact: true })).toHaveValue(
    draft.account,
  );
  expect(prepares).toBe(0);
  expect(executes).toBe(0);
  await page
    .getByRole("button", { name: "核对并预留整批资金", exact: true })
    .click();
  await page
    .getByRole("button", { name: "确认预留，不发送付款", exact: true })
    .click();
  expect(prepares).toBe(1);
  expect(executes).toBe(0);
  await expect(page.getByText(/预留结果未确认/)).toBeVisible();
  expect(
    await page.evaluate(
      () => JSON.stringify(localStorage) + JSON.stringify(sessionStorage),
    ),
  ).not.toContain(draft.account);
});

test("unsupported XLS and rejected CSV leave existing draft untouched and show safe source row", async ({
  page,
}) => {
  let imports = 0,
    writes = 0;
  await page.route("**/console/v1/**", (route) => {
    const p = new URL(route.request().url()).pathname;
    if (p.endsWith("/session"))
      return route.fulfill(
        ok({
          user: { id: "1", role: "merchant", name: "local" },
          csrf_token: "csrf",
        }),
      );
    if (p.endsWith("/transfers/capabilities"))
      return route.fulfill(
        ok({
          writes_enabled: true,
          channels: [{ id: "7", types: ["alipay"] }],
        }),
      );
    if (p.endsWith("/transfer-batch-imports/preview")) {
      imports++;
      return route.fulfill({
        status: 422,
        contentType: "application/json",
        body: JSON.stringify({
          code: -1,
          message: "row 3: positive_decimal_amount_required",
          data: { error: "positive_decimal_amount_required", source_row: 3 },
        }),
      });
    }
    if (route.request().method() === "POST") writes++;
    return route.fulfill(ok({ items: [], total: 0, next_before_id: "0" }));
  });
  await page.goto("/merchant/transfer/batches");
  await page.getByRole("button", { name: "新建批量付款", exact: true }).click();
  await page
    .getByLabel("收款账号", { exact: true })
    .fill("existing-private-draft");
  await page.getByLabel("导入文件").setInputFiles({
    name: "legacy.xls",
    mimeType: "application/vnd.ms-excel",
    buffer: Buffer.from("synthetic"),
  });
  await expect(page.getByRole("alert")).toContainText("旧版 XLS 暂不支持");
  expect(imports).toBe(0);
  await page.getByLabel("导入文件").setInputFiles({
    name: "invalid.csv",
    mimeType: "text/csv",
    buffer: Buffer.from("synthetic"),
  });
  await expect(page.getByRole("alert")).toContainText("第 3 行");
  await expect(page.getByLabel("收款账号", { exact: true })).toHaveValue(
    "existing-private-draft",
  );
  await expect(page.getByRole("region", { name: "文件校验预览" })).toHaveCount(
    0,
  );
  expect(imports).toBe(1);
  expect(writes).toBe(0);
  await page
    .getByRole("button", { name: "取消编辑并清除收款资料", exact: true })
    .click();
  await expect(page.getByRole("region", { name: "五列文件导入" })).toHaveCount(
    0,
  );
});
