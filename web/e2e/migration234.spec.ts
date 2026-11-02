import { test, expect, type Page } from "@playwright/test";
import site from "./fixtures/site191-http.json" with { type: "json" };
import {
  migrationCapabilities234,
  migrationReport234,
  migrationView234,
} from "../tests/migration234-fixtures";

async function fixture(
  page: Page,
  options: {
    target?: boolean;
    unknownApply?: boolean;
    unknownPrepare?: boolean;
    operator?: boolean;
  } = {},
) {
  const writes: { path: string; body: string | null }[] = [];
  const reads: string[] = [];
  let task = migrationView234({
      target_ready: options.target ?? true,
      ...(options.target === false
        ? {
            status: "blocked",
            issues: [{ path: "target", code: "target_not_empty" }],
          }
        : {}),
    }),
    created = false;
  await page.route("**/console/v1/**", async (route) => {
    const req = route.request(),
      path = new URL(req.url()).pathname,
      method = req.method();
    let data: unknown = {};
    if (path.endsWith("/session"))
      data = {
        user: {
          id: "1",
          role: "admin",
          admin_class: options.operator ? "operator" : "super",
          name: "本地迁移验收234",
        },
        csrf_token: "synthetic234",
      };
    else if (path.endsWith("/public/site")) data = site.default;
    else if (path.endsWith("/public/layout"))
      data = { zhuce: "", footer: "", format: "html" };
    else if (path.endsWith("/public/merchant-access"))
      data = { close_keylogin: 1, user_refund: 1 };
    else if (path.endsWith("/public/connect/options"))
      data = {
        providers: { qq: false, wx: false, alipay: false, telegram: false },
      };
    else if (path.endsWith("/migration/epay") && method === "GET") {
      reads.push(path);
      data = migrationCapabilities234({
        target_ready: options.target ?? true,
        runs: created ? [task] : [],
      });
    } else if (path.endsWith("/migration/epay/prepare") && method === "POST") {
      writes.push({ path, body: req.postData() });
      created = true;
      if (options.unknownPrepare) return route.abort("failed");
      data = task;
    } else if (path.endsWith("/apply") && method === "POST") {
      writes.push({ path, body: req.postData() });
      task = migrationView234({
        status: "completed",
        progress: 100,
        report: migrationReport234(),
      });
      if (options.unknownApply) return route.abort("failed");
      data = task;
    } else if (path.endsWith("a".repeat(32)) && method === "GET") {
      reads.push(path);
      data = task;
    } else
      throw Error(`Unexpected synthetic migration request ${method} ${path}`);
    return route.fulfill({
      contentType: "application/json",
      body: JSON.stringify({ code: 0, message: "", data }),
    });
  });
  await page.goto("/admin/system/migration");
  return { writes, reads };
}
async function upload(page: Page) {
  await page.getByLabel("易支付SQL备份", { exact: true }).setInputFiles({
    name: "private-source234.sql",
    mimeType: "text/plain",
    buffer: Buffer.from(
      "-- synthetic SQL fixture; no real credentials or identity data\n",
    ),
  });
  await page.getByRole("button", { name: "上传并预检查", exact: true }).click();
  await expect(page.getByRole("table", { name: "源表迁移范围" })).toBeVisible();
}
async function acknowledge(page: Page) {
  await page.getByLabel("确认源站停止写入且备份一致", { exact: true }).check();
  await page.getByLabel("确认仅归档数据不会自动启用", { exact: true }).check();
  await page.getByLabel("输入MIGRATE确认导入", { exact: true }).fill("MIGRATE");
}
for (const width of [375, 1440]) {
  test(`migration234 complete explicit upload/preview/import at ${width}`, async ({
    page,
  }) => {
    await page.setViewportSize({ width, height: 900 });
    const f = await fixture(page);
    await upload(page);
    await expect(
      page.getByText("private-source234.sql", { exact: true }),
    ).toHaveCount(0);
    await expect(
      page.getByRole("table", { name: "源表迁移范围" }),
    ).toContainText("仅归档，不自动启用");
    await expect(
      page.getByRole("button", { name: "开始导入", exact: true }),
    ).toBeDisabled();
    expect(f.writes).toHaveLength(1);
    await acknowledge(page);
    await page.screenshot({
      path: test.info().outputPath(`preview-${width}.png`),
      fullPage: false,
    });
    await page.getByRole("button", { name: "开始导入", exact: true }).click();
    await expect(
      page.getByRole("heading", {
        name: "导入完成，待配置和启用",
        exact: true,
      }),
    ).toBeVisible();
    expect(f.writes).toHaveLength(2);
    expect(f.writes[0].body).toContain('name="backup"');
    expect(JSON.parse(f.writes[1].body!)).toEqual({
      expected_sha256: "b".repeat(64),
      source_frozen: true,
      archive_acknowledged: true,
      confirmation: "MIGRATE",
    });
    expect(
      await page.evaluate(
        () => document.documentElement.scrollWidth <= innerWidth,
      ),
    ).toBe(true);
    const persisted = await page.evaluate(() =>
      JSON.stringify({
        local: { ...localStorage },
        session: { ...sessionStorage },
      }),
    );
    expect(persisted).not.toContain("private-source234");
    expect(persisted).not.toContain("synthetic SQL fixture");
    await page.screenshot({
      path: test.info().outputPath(`complete-${width}.png`),
      fullPage: false,
    });
  });
  test(`migration234 existing target can inspect but never import at ${width}`, async ({
    page,
  }) => {
    await page.setViewportSize({ width, height: 900 });
    const f = await fixture(page, { target: false });
    await upload(page);
    await expect(page.getByText(/当前实例已有业务数据/)).toBeVisible();
    await expect(
      page.getByRole("heading", { name: "先解决以下阻塞项" }),
    ).toBeVisible();
    await expect(
      page.getByRole("button", { name: "开始导入", exact: true }),
    ).toHaveCount(0);
    expect(f.writes).toHaveLength(1);
    await expect(page.getByRole("button", { name: /清空|覆盖/ })).toHaveCount(
      0,
    );
  });
}
test("migration234 uncertain apply reads the existing task without any retry", async ({
  page,
}) => {
  const f = await fixture(page, { unknownApply: true });
  await upload(page);
  await acknowledge(page);
  await page.getByRole("button", { name: "开始导入", exact: true }).click();
  await expect(page.getByRole("alert")).toContainText("导入结果尚未确认");
  expect(f.reads.filter((x) => x.endsWith("a".repeat(32)))).toHaveLength(0);
  await page
    .getByRole("button", { name: "读取当前任务状态", exact: true })
    .click();
  await expect(
    page.getByRole("heading", { name: "导入完成，待配置和启用", exact: true }),
  ).toBeVisible();
  expect(f.writes.filter((x) => x.path.endsWith("/apply"))).toHaveLength(1);
});
test("migration234 lost prepare recovers only from an explicit owner record read", async ({
  page,
}) => {
  const f = await fixture(page, { unknownPrepare: true });
  await page.getByLabel("易支付SQL备份", { exact: true }).setInputFiles({
    name: "synthetic.sql",
    mimeType: "text/plain",
    buffer: Buffer.from("local synthetic backup"),
  });
  await page.getByRole("button", { name: "上传并预检查", exact: true }).click();
  await expect(page.getByRole("alert")).toContainText("预检查结果尚未确认");
  await expect(
    page.getByRole("button", { name: "上传并预检查", exact: true }),
  ).toBeDisabled();
  await page.getByRole("button", { name: "读取迁移记录", exact: true }).click();
  await page
    .getByRole("button", { name: "查看任务 aaaaaaaa", exact: true })
    .click();
  await expect(page.getByRole("table", { name: "源表迁移范围" })).toBeVisible();
  expect(f.writes.filter((x) => x.path.endsWith("/prepare"))).toHaveLength(1);
  expect(f.writes.filter((x) => x.path.endsWith("/apply"))).toHaveLength(0);
});
test("migration234 an operator cannot discover or open the wizard", async ({
  page,
}) => {
  const f = await fixture(page, { operator: true });
  await expect(
    page.getByRole("heading", { name: "易支付迁移", exact: true }),
  ).toHaveCount(0);
  await expect(
    page.getByRole("link", { name: "易支付迁移", exact: true }),
  ).toHaveCount(0);
  expect(f.writes).toHaveLength(0);
  expect(f.reads).toHaveLength(0);
});
