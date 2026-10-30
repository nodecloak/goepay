import { test, expect, type Page } from "@playwright/test";
async function fixture(page: Page, area: "admin" | "merchant") {
  const writes: { path: string; body: unknown }[] = [];
  let saved = false;
  const record = {
    id: "42",
    business_name: "本地资料",
    contact_name: "张三",
    contact_phone: "13000000000",
    channel_id: "3",
    status: "draft",
  };
  await page.route("**/console/v1/**", async (route) => {
    const req = route.request(),
      p = new URL(req.url()).pathname;
    let data: unknown = { items: [], total: 0 };
    if (p.endsWith("/session"))
      data = {
        user: { id: "1", name: area, role: area, admin_class: "super" },
        csrf_token: "draft-csrf",
      };
    else if (p.endsWith("/security/payment-password"))
      data = {
        configured: false,
        enforced: false,
        credential_epoch: 0,
        totp_required: false,
      };
    else if (p.endsWith("/schema"))
      data = {
        title: "进件商户资料",
        description: "本地进件草稿和人工审核，不代表已获得上游商户资格。",
        permissions: { create: true, update: true, delete: true },
        fields: [
          {
            name: "business_name",
            label: "经营名称",
            type: "string",
            required: true,
          },
          {
            name: "contact_name",
            label: "联系人",
            type: "string",
            required: true,
          },
          {
            name: "contact_phone",
            label: "联系电话",
            type: "string",
            required: true,
          },
          {
            name: "channel_id",
            label: "进件渠道 ID",
            type: "string",
            required: true,
          },
        ],
      };
    else if (p.endsWith("/42")) data = record;
    else if (p.endsWith("/onboarding-applications"))
      data = { items: saved ? [record] : [], total: saved ? 1 : 0 };
    if (req.method() !== "GET") {
      writes.push({ path: p, body: req.postDataJSON() });
      expect(req.headers()["x-csrf-token"]).toBe("draft-csrf");
      Object.assign(record, req.postDataJSON());
      saved = true;
      data = record;
    }
    return route.fulfill({
      contentType: "application/json",
      body: JSON.stringify({ code: 0, data }),
    });
  });
  return { writes };
}
for (const width of [375, 1280]) {
  test(`merchant local application has a direct form and returns to its records at ${width}px`, async ({
    page,
  }) => {
    await page.setViewportSize({ width, height: 960 });
    const { writes } = await fixture(page, "merchant");
    await page.goto("/merchant/applyments/create");
    await expect(
      page.getByRole("region", { name: "新增进件商户资料", exact: true }),
    ).toBeVisible();
    await expect(page.getByRole("dialog")).toHaveCount(0);
    await expect(page.getByRole("table")).toHaveCount(0);
    expect(writes).toHaveLength(0);
    for (const [key, value] of Object.entries({
      business_name: "本地资料",
      contact_name: "张三",
      contact_phone: "13000000000",
      channel_id: "3",
    }))
      await page.locator(`#field-${key}`).fill(value);
    await page.getByRole("button", { name: "保存", exact: true }).click();
    await expect(page.getByRole("status")).toHaveText("新增已保存");
    expect(writes).toHaveLength(1);
    await expect(page.locator("#field-business_name")).toHaveCount(0);
    await page
      .getByRole("link", { name: "返回进件资料列表", exact: true })
      .click();
    await expect(page.getByRole("table")).toBeVisible();
    await page.getByRole("link", { name: "编辑", exact: true }).click();
    await expect(page.locator("#field-business_name")).toHaveValue("本地资料");
    expect(writes).toHaveLength(1);
    expect(
      await page.evaluate(
        () => document.documentElement.scrollWidth <= innerWidth,
      ),
    ).toBe(true);
  });
  test(`admin hidden form loads only the requested local record at ${width}px`, async ({
    page,
  }) => {
    await page.setViewportSize({ width, height: 960 });
    const { writes } = await fixture(page, "admin");
    await page.goto("/admin/applyments/form?id=42");
    await expect(
      page.getByRole("heading", { name: "编辑进件商户资料", exact: true }),
    ).toBeVisible();
    await expect(page.locator("#field-business_name")).toHaveValue("本地资料");
    await expect(page.getByRole("table")).toHaveCount(0);
    await expect(page.getByRole("dialog")).toHaveCount(0);
    expect(writes).toHaveLength(0);
    expect(
      await page.evaluate(
        () => document.documentElement.scrollWidth <= innerWidth,
      ),
    ).toBe(true);
  });
}
