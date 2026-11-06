import { test, expect } from "@playwright/test";
import { localCrudFixture } from "./local-crud-fixture";
import { localRedBackgroundPNG } from "./local-test-images";
import {
  newCollectionStyle,
  collectionStylePayload,
} from "../src/collection-style";

test.describe("real local persisted operational edits", () => {
  test.skip(
    process.env.GOEPAY_LIVE_E2E !== "1" ||
      process.env.GOEPAY_LIVE_NODE_WORKFLOWS_E2E !== "1",
    "Owned loopback simulation only",
  );
  let local: Awaited<ReturnType<typeof localCrudFixture>>;
  test.beforeAll(async ({ playwright }) => {
    local = await localCrudFixture(playwright, import.meta.url);
  });
  test.beforeEach(async ({ page }) => {
    await local.attach(page, "admin");
  });
  test.afterAll(async () => {
    await local?.dispose();
  });

  test("domain list returns to a populated page after deleting its last row", async ({
    page,
  }) => {
    for (let i = 0; i < 21; i++)
      await local.call("admin", "/resources/domains", "POST", {
        merchant_id: "1000",
        domain: `${local.tag}-${i}.example.test`,
        approved: false,
        enabled: true,
      });
    await page.goto("/admin/merchant/domain");
    await expect(page.locator("tbody tr")).toHaveCount(20);
    await page.getByRole("button", { name: "下一页", exact: true }).click();
    await expect(page.locator("tbody tr")).toHaveCount(1);
    await page
      .locator("tbody tr")
      .getByRole("button", { name: "删除", exact: true })
      .click();
    await page
      .getByRole("dialog")
      .getByRole("button", { name: "确认删除", exact: true })
      .click();
    await expect(page.locator("tbody tr")).toHaveCount(20);
    await expect(
      page.getByRole("button", { name: "上一页", exact: true }),
    ).toBeDisabled();
    expect(
      (await local.call("admin", "/resources/domains?limit=100")).total,
    ).toBe(20);
  });

  test("announcement deletion leaves a valid page and edited color and order remain persisted", async ({
    page,
  }) => {
    for (let i = 0; i < 21; i++)
      await local.call("admin", "/resources/announcements", "POST", {
        content: `${local.tag}-notice-${i}`,
        content_format: "text",
        sort: i + 1,
        color: "#123456",
        enabled: false,
        audience: "merchant",
      });
    await page.goto("/admin/system/anounce");
    await expect(page.locator(".announcement-row")).toHaveCount(20);
    await page.getByRole("button", { name: "下一页", exact: true }).click();
    await expect(page.locator(".announcement-row")).toHaveCount(1);
    await page
      .locator(".announcement-row")
      .getByRole("button", { name: "删除", exact: true })
      .click();
    await page
      .getByRole("dialog", { name: "删除公告" })
      .getByRole("button", { name: "确定删除", exact: true })
      .click();
    await expect(page.locator(".announcement-row")).toHaveCount(20);
    await expect(
      page.getByRole("button", { name: "上一页", exact: true }),
    ).toHaveCount(0);
    await page
      .locator(".announcement-row")
      .first()
      .getByRole("button", { name: "编辑", exact: true })
      .click();
    const editor = page.getByRole("region", { name: "编辑公告", exact: true });
    await editor.getByLabel("排序", { exact: true }).fill("99");
    await editor.getByLabel("公告文字颜色", { exact: true }).fill("#654321");
    const saving = page.waitForResponse(
      (r) =>
        r.request().method() === "PATCH" &&
        new URL(r.url()).pathname.includes("/resources/announcements/"),
    );
    await editor.getByRole("button", { name: /保存|修改/ }).click();
    const saved = await (await saving).json();
    expect(saved.code).toBe(0);
    await page.reload();
    const detail = await local.call(
      "admin",
      "/resources/announcements/" + saved.data.id,
    );
    expect(detail).toMatchObject({ sort: "99", color: "#654321" });
  });

  test("style-card deletion returns from the last page without hiding the other layouts", async ({
    page,
  }) => {
    const { api, csrf } = await local.session("admin");
    const uploaded = await api.post(
      "/console/v1/admin/collection-style-images",
      {
        headers: { "X-CSRF-Token": csrf },
        multipart: {
          file: {
            name: "synthetic.png",
            mimeType: "image/png",
            buffer: localRedBackgroundPNG(),
          },
        },
      },
    );
    expect(uploaded.status()).toBe(201);
    const asset = (await uploaded.json()).data;
    for (let i = 0; i < 13; i++)
      await local.call(
        "admin",
        "/collection-styles",
        "POST",
        collectionStylePayload({
          ...newCollectionStyle(),
          name: `style_${local.tag.replaceAll("-", "_")}_${i}`,
          label: `${local.tag}-style-${i}`,
          url: "/collection-style-assets/" + asset.id,
        }),
      );
    await page.goto("/admin/other/onecode");
    await page
      .getByRole("textbox", { name: "搜索样式", exact: true })
      .fill(local.tag + "-style-");
    await expect(page.locator(".style-card")).toHaveCount(12);
    await page.getByRole("button", { name: "下一页", exact: true }).click();
    await expect(page.locator(".style-card")).toHaveCount(1);
    await page
      .locator(".style-card")
      .getByRole("button", { name: "删除", exact: true })
      .click();
    await page
      .getByRole("dialog")
      .getByRole("button", { name: "确认删除", exact: true })
      .click();
    await expect(page.locator(".style-card")).toHaveCount(12);
    await expect(
      page.getByRole("button", { name: "上一页", exact: true }),
    ).toBeDisabled();
    expect(
      (await local.call("admin", "/collection-styles")).items.filter((r: any) =>
        String(r.fields.name).startsWith(
          "style_" + local.tag.replaceAll("-", "_") + "_",
        ),
      ),
    ).toHaveLength(12);
  });

  test("style upload and creation retain the saved edit identity on refresh", async ({
    page,
  }) => {
    await page.goto("/admin/other/onecode");
    await page.getByRole("link", { name: "新增样式", exact: true }).click();
    const name = "created_" + local.tag.replaceAll("-", "_");
    await page.getByLabel("风格标识", { exact: true }).fill(name);
    await page
      .getByLabel("显示名称", { exact: true })
      .fill(local.tag + "-uploaded-style");
    await page.getByLabel("上传背景图", { exact: true }).setInputFiles({
      name: "local.png",
      mimeType: "image/png",
      buffer: localRedBackgroundPNG(),
    });
    await expect(
      page.getByRole("status").filter({ hasText: /背景已上传/ }),
    ).toBeVisible();
    const saving = page.waitForResponse(
      (r) =>
        r.request().method() === "POST" &&
        new URL(r.url()).pathname === "/console/v1/admin/collection-styles",
    );
    await page.getByRole("button", { name: "保存样式", exact: true }).click();
    const saved = await (await saving).json();
    expect(saved.code).toBe(0);
    await expect
      .poll(() => new URL(page.url()).searchParams.get("id"))
      .toBe(saved.data.id);
    await expect(
      page.getByRole("status").filter({ hasText: /已保存/ }),
    ).toBeVisible();
    await expect(page.getByLabel("风格标识", { exact: true })).toHaveValue(
      name,
    );
    await page.reload();
    await expect(page.getByLabel("风格标识", { exact: true })).toHaveValue(
      name,
    );
    await page
      .getByLabel("显示名称", { exact: true })
      .fill(local.tag + "-style-edited");
    const update = page.waitForResponse(
      (r) =>
        r.request().method() === "PUT" &&
        new URL(r.url()).pathname.endsWith(
          "/collection-styles/" + saved.data.id,
        ),
    );
    await page.getByRole("button", { name: "保存样式", exact: true }).click();
    expect((await update).ok()).toBe(true);
    await page.reload();
    await expect(page.getByLabel("显示名称", { exact: true })).toHaveValue(
      local.tag + "-style-edited",
    );
  });

  test("routing draft, channel weights, limits and status survive independent edits", async ({
    page,
  }) => {
    await page.goto("/admin/payapi/pay-roll");
    await page
      .getByRole("button", { name: "新增轮询配置", exact: true })
      .click();
    const dialog = page.getByRole("dialog"),
      name = local.tag + "-roll";
    await dialog.getByLabel("显示名称", { exact: true }).fill(name);
    await dialog.getByLabel("支付方式", { exact: true }).selectOption("alipay");
    await dialog
      .getByLabel("轮询方式", { exact: true })
      .selectOption("weighted");
    const creating = page.waitForResponse(
      (r) =>
        r.request().method() === "POST" &&
        new URL(r.url()).pathname.endsWith("/pay-rolls/drafts"),
    );
    await dialog
      .getByRole("button", { name: "创建轮询组", exact: true })
      .click();
    const created = await (await creating).json();
    expect(created.code).toBe(0);
    const id = created.data.id;
    await expect(dialog).toHaveCount(0);
    let row = page.getByRole("row").filter({ hasText: name });
    await row.getByRole("button", { name: "配置通道", exact: true }).click();
    await dialog.getByLabel("选择通道 1", { exact: true }).check();
    await dialog.getByLabel("通道 1 权重", { exact: true }).fill("3");
    await dialog
      .getByRole("button", { name: "保存通道配置", exact: true })
      .click();
    await expect
      .poll(
        async () =>
          (await local.call("admin", "/resources/pay-rolls/" + id)).channel_ids,
      )
      .toBe("1:3");
    await expect(dialog).toHaveCount(0);
    await row.getByRole("button", { name: "编辑", exact: true }).click();
    await dialog.locator("#field-name").fill(name + "-edited");
    await dialog
      .locator("#field-limits")
      .fill('{"min_amount":"0.01","max_amount":"1000.00"}');
    await dialog.locator("#field-enabled").check();
    await dialog.getByRole("button", { name: "保存", exact: true }).click();
    await expect(dialog).toHaveCount(0);
    await page.reload();
    row = page.getByRole("row").filter({ hasText: name + "-edited" });
    await expect(row).toBeVisible();
    const persisted = await local.call("admin", "/resources/pay-rolls/" + id);
    expect(persisted.channel_ids).toBe("1:3");
    expect(persisted.enabled).toBe(true);
    expect(JSON.parse(persisted.limits)).toEqual({
      min_amount: "0.01",
      max_amount: "1000.00",
    });
    await row.getByRole("button", { name: "编辑", exact: true }).click();
    await dialog.locator("#field-enabled").uncheck();
    await dialog.getByRole("button", { name: "保存", exact: true }).click();
    await expect(dialog).toHaveCount(0);
    expect(
      (await local.call("admin", "/resources/pay-rolls/" + id)).enabled,
    ).toBe(false);
  });
});
