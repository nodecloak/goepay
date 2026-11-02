import { expect, test, type Page } from "@playwright/test";

const merchantSchema = {
  title: "商户管理",
  permissions: { create: true, update: true, delete: false },
  fields: [
    {
      name: "creation_mode",
      label: "新增方式",
      type: "select",
      write_only: true,
      options: ["account", "api"],
    },
    {
      name: "id",
      label: "商户ID",
      type: "string",
      required: true,
      create_only: true,
    },
    { name: "name", label: "名称", type: "string", required: true },
    {
      name: "phone",
      label: "手机号码",
      type: "string",
      max_length: 21,
      pattern: "^\\+?[0-9]{5,20}$",
    },
    { name: "email", label: "邮箱", type: "email", max_length: 254 },
    { name: "login_username", label: "登录账号", type: "string" },
    {
      name: "login_password",
      label: "登录密码",
      type: "password",
      write_only: true,
    },
    { name: "enabled", label: "状态", type: "boolean", default: true },
  ],
};
const paySchema = {
  title: "支付方式",
  permissions: { create: true, update: true, delete: true },
  fields: [
    { name: "name", label: "展示名称", type: "string", required: true },
    {
      name: "type",
      label: "类型",
      type: "select",
      required: true,
      create_only: true,
      options: ["alipay", "wxpay", "qqpay"],
    },
    {
      name: "device",
      label: "支持设备",
      type: "select",
      required: true,
      default: "0",
      options: ["0", "1", "2"],
    },
    { name: "enabled", label: "启用", type: "boolean", default: true },
  ],
};
async function fixture(page: Page) {
  const writes: {
      path: string;
      method: string;
      body?: Record<string, unknown>;
    }[] = [],
    errors: string[] = [];
  const merchants: Record<string, unknown>[] = [];
  const types = [
    { id: "1", type: "alipay", name: "支付宝", enabled: false, device: "0" },
    { id: "2", type: "wxpay", name: "微信支付", enabled: false, device: "0" },
  ];
  page.on("pageerror", (e) => errors.push(e.message));
  await page.route("**/console/v1/**", async (route) => {
    const req = route.request(),
      path = new URL(req.url()).pathname,
      method = req.method();
    let data: unknown = { items: [], total: 0 };
    if (method !== "GET")
      writes.push({
        path,
        method,
        ...(method !== "DELETE" ? { body: req.postDataJSON() } : {}),
      });
    if (path.endsWith("/session"))
      data = {
        user: {
          id: "1",
          name: "本地管理员",
          role: "admin",
          admin_class: "super",
        },
        csrf_token: "synthetic-admin-form-interaction",
      };
    else if (path.endsWith("/resources/merchants/schema"))
      data = merchantSchema;
    else if (path.endsWith("/resources/merchants")) {
      if (method === "POST") {
        const row = { id: "12295", ...req.postDataJSON() };
        merchants.push(row);
        data = row;
      } else data = { items: merchants, total: merchants.length };
    } else if (path.endsWith("/resources/pay-types/schema")) data = paySchema;
    else if (path.includes("/resources/pay-types")) {
      const id = path.split("/").at(-1),
        body =
          method !== "GET" && method !== "DELETE"
            ? req.postDataJSON()
            : undefined;
      if (method === "POST") types.push({ id: "3", ...body });
      else if (method === "PATCH")
        Object.assign(
          types.find((r) => r.id === id)!,
          body,
        );
      else if (method === "DELETE")
        types.splice(
          types.findIndex((r) => r.id === id),
          1,
        );
      data = /\/\d+$/.test(path)
        ? (types.find((r) => r.id === id) ?? {})
        : { items: types, total: types.length };
    }
    await route.fulfill({ json: { code: 0, message: "", data } });
  });
  return { writes, errors };
}

for (const width of [1280, 375])
  test(`lost merchant-create response reconciles without another POST and restores focus after list read at ${width}px`, async ({
    page,
  }) => {
    await page.setViewportSize({ width, height: 1000 });
    const f = await fixture(page);
    let created = false,
      posts = 0,
      releaseList!: () => void;
    const listGate = new Promise<void>((resolve) => (releaseList = resolve));
    await page.route(
      "**/console/v1/admin/resources/merchants**",
      async (route) => {
        const req = route.request(),
          url = new URL(req.url());
        if (url.pathname.endsWith("/schema")) return route.fallback();
        if (req.method() === "POST") {
          posts++;
          created = true;
          return route.abort("failed");
        }
        if (!created)
          return route.fulfill({
            json: { code: 0, data: { items: [], total: 0 } },
          });
        if (url.searchParams.get("limit") === "20") await listGate;
        return route.fulfill({
          json: {
            code: 0,
            data: {
              items: [
                {
                  id: "12299",
                  name: "Synthetic",
                  login_username: "13000012299",
                },
              ],
              total: 1,
            },
          },
        });
      },
    );
    await page.goto("/admin/merchant/merchant-list");
    const opener = page.getByRole("button", { name: "新增商户", exact: true });
    await opener.click();
    const dialog = page.getByRole("dialog", { name: "添加商户", exact: true });
    await dialog.locator("#field-phone").fill("13000012299");
    await dialog
      .locator("#field-login_password")
      .fill("synthetic-recovery-browser-password");
    await dialog.getByRole("button", { name: "确定添加", exact: true }).click();
    await expect(dialog).toContainText("添加结果尚未确认");
    await expect(dialog.locator("#field-login_password")).toHaveValue("");
    await expect(
      dialog.getByRole("button", { name: "确定添加", exact: true }),
    ).toBeDisabled();
    await dialog
      .getByRole("button", { name: "核对商户账号", exact: true })
      .click();
    await expect(dialog).toHaveCount(0);
    await expect(opener).toBeDisabled();
    releaseList();
    await expect(opener).toBeEnabled();
    await expect(opener).toBeFocused();
    await expect(page.locator("main")).toContainText("12299");
    expect(posts).toBe(1);
    expect(f.errors).toHaveLength(0);
  });

for (const width of [1280, 375]) {
  test(`merchant creation starts inside the dialog, traps keyboard focus, locates the first error and returns focus at ${width}px`, async ({
    page,
  }, info) => {
    await page.setViewportSize({ width, height: 1000 });
    const f = await fixture(page);
    await page.goto("/admin/merchant/merchant-list");
    const opener = page.getByRole("button", { name: "新增商户", exact: true });
    await opener.click();
    const dialog = page.getByRole("dialog", { name: "添加商户", exact: true });
    const phone = dialog.locator("#field-phone"),
      close = dialog.getByRole("button", { name: "关闭", exact: true }).first(),
      submit = dialog.getByRole("button", { name: "确定添加", exact: true });
    await expect(phone).toBeFocused();
    await phone.press("Shift+Tab");
    await expect(close).toBeFocused();
    await close.press("Shift+Tab");
    await expect(submit).toBeFocused();
    await submit.press("Tab");
    await expect(close).toBeFocused();
    await close.press("Tab");
    await expect(phone).toBeFocused();
    await dialog
      .locator("#field-login_password")
      .fill("synthetic-keyboard-password");
    await submit.click();
    await expect(dialog).toContainText("请填写手机号或邮箱");
    await expect(phone).toBeFocused();
    const box = await dialog.boundingBox();
    expect(box!.x).toBeGreaterThanOrEqual(0);
    expect(box!.x + box!.width).toBeLessThanOrEqual(width);
    await dialog.screenshot({
      path: info.outputPath("merchant-create-keyboard-" + width + ".png"),
    });
    await phone.press("Escape");
    await expect(dialog).toHaveCount(0);
    await expect(opener).toBeFocused();
    expect(f.writes).toHaveLength(0);
    expect(f.errors).toHaveLength(0);
  });
  test(`API-only merchant mode keeps contact fields repairable across mode changes at ${width}px`, async ({
    page,
  }) => {
    await page.setViewportSize({ width, height: 1000 });
    const f = await fixture(page);
    await page.goto("/admin/merchant/merchant-list");
    await page.getByRole("button", { name: "新增商户", exact: true }).click();
    const dialog = page.getByRole("dialog", { name: "添加商户", exact: true });
    await dialog.locator("#field-phone").fill("13000012295");
    await dialog.locator("#field-email").fill("bad email");
    await expect(dialog.locator("#field-email")).toHaveAttribute(
      "maxlength",
      "100",
    );
    await dialog.locator("summary").click();
    const mode = dialog.getByRole("checkbox", {
      name: "仅API（不创建后台登录账号）",
      exact: true,
    });
    await mode.check();
    await expect(dialog.getByLabel("联系手机", { exact: true })).toHaveValue(
      "13000012295",
    );
    await expect(dialog.getByLabel("联系邮箱", { exact: true })).toHaveValue(
      "bad email",
    );
    await expect(dialog.locator("#field-email")).toHaveAttribute(
      "maxlength",
      "254",
    );
    await mode.uncheck();
    await expect(dialog.locator("#field-email")).toHaveAttribute(
      "maxlength",
      "100",
    );
    await expect(
      dialog.getByLabel("邮箱(登录账号)", { exact: true }),
    ).toHaveValue("bad email");
    await mode.check();
    await dialog.locator("#field-id").fill("12295");
    await dialog.locator("#field-name").fill("本地API商户");
    const email = "a".repeat(110) + "@example.invalid";
    await dialog.getByLabel("联系邮箱", { exact: true }).fill(email);
    await dialog.getByRole("button", { name: "确定添加", exact: true }).click();
    await expect(dialog).toHaveCount(0);
    expect(f.writes).toHaveLength(1);
    expect(f.writes[0]).toMatchObject({
      path: "/console/v1/admin/resources/merchants",
      method: "POST",
      body: {
        creation_mode: "api",
        id: "12295",
        name: "本地API商户",
        phone: "13000012295",
        email,
      },
    });
    expect(f.writes[0].body).not.toHaveProperty("login_password");
    expect(f.errors).toHaveLength(0);
  });
}

test("pay-type operations remain usable across two toggles, create, edit and delete without duplicate writes", async ({
  page,
}) => {
  const f = await fixture(page);
  await page.goto("/admin/payapi/pay-type");
  const row = (type: string) =>
    page.locator("tbody tr").filter({ hasText: type });
  await row("alipay").getByRole("switch").click();
  await expect(row("alipay").getByRole("switch")).toBeChecked();
  await expect(row("wxpay").getByRole("switch")).toBeEnabled();
  await row("wxpay").getByRole("switch").click();
  await expect(row("wxpay").getByRole("switch")).toBeChecked();
  const create = page.getByRole("button", { name: "新增", exact: true });
  await expect(create).toBeEnabled();
  await create.click();
  const dialog = page.getByRole("dialog");
  await dialog.locator("#field-type").selectOption("qqpay");
  await dialog.locator("#field-name").fill("本地QQ支付");
  await dialog.getByRole("button", { name: "保存", exact: true }).click();
  await expect(dialog).toHaveCount(0);
  await expect(create).toBeEnabled();
  await row("qqpay").getByRole("button", { name: "编辑", exact: true }).click();
  await dialog.locator("#field-name").fill("本地QQ支付更新");
  await dialog.getByRole("button", { name: "保存", exact: true }).click();
  await expect(dialog).toHaveCount(0);
  await expect(create).toBeEnabled();
  await expect(row("qqpay")).toContainText("本地QQ支付更新");
  await row("qqpay").getByRole("button", { name: "删除", exact: true }).click();
  await dialog.getByRole("button", { name: "确认删除", exact: true }).click();
  await expect(dialog).toHaveCount(0);
  await expect(row("qqpay")).toHaveCount(0);
  await expect(create).toBeEnabled();
  await expect(
    page.getByRole("button", { name: "刷新", exact: true }),
  ).toBeEnabled();
  expect(f.writes.map((w) => w.method)).toEqual([
    "PATCH",
    "PATCH",
    "POST",
    "PATCH",
    "DELETE",
  ]);
  expect(
    f.writes.every((w) =>
      w.path.startsWith("/console/v1/admin/resources/pay-types"),
    ),
  ).toBe(true);
  expect(f.errors).toHaveLength(0);
});
