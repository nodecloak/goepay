import { pageReload248 } from "./fixtures/page-reload248";
import { expect, test, type Page } from "@playwright/test";

const fields = [
  { name: "name", label: "名称", type: "string", required: true },
  { name: "merchant_id", label: "商户ID", type: "string", required: true },
  {
    name: "type",
    label: "支付方式",
    type: "select",
    options: ["alipay", "wxpay"],
  },
  {
    name: "strategy",
    label: "轮询方式",
    type: "select",
    options: ["round_robin", "weighted", "first", "priority"],
  },
  {
    name: "channel_ids",
    label: "通道ID:权重，逗号分隔",
    type: "string",
    required: true,
  },
  { name: "limits", label: "组限制 JSON", type: "text" },
  { name: "rules", label: "各通道限制 JSON", type: "text" },
  { name: "default_route", label: "默认用途", type: "boolean" },
  { name: "enabled", label: "执行路由", type: "boolean" },
];
async function fixture(page: Page, unknownSave = false) {
  const records: Record<string, unknown>[] = [
    {
      id: "41",
      name: "运行轮询",
      merchant_id: "101",
      type: "alipay",
      strategy: "weighted",
      channel_ids: "7:2",
      limits: '{"daily_count":7}',
      rules: '{"7":{"daily_count":2}}',
      enabled: true,
      default_route: false,
    },
  ];
  const writes: { path: string; body: Record<string, unknown> }[] = [],
    errors: string[] = [];
  let revision = "a".repeat(64),
    failedRead = false;
  page.on("pageerror", (e) => errors.push(e.message));
  await page.route("**/console/v1/**", async (route) => {
    const req = route.request(),
      url = new URL(req.url()),
      path = url.pathname;
    let data: unknown = { items: [], total: 0 };
    if (path.endsWith("/session"))
      data = {
        user: {
          id: "1",
          role: "admin",
          admin_class: "operator",
          permissions: ["PayRoll"],
          name: "Synthetic operator",
        },
        csrf_token: "synthetic-roll-csrf",
      };
    else if (path.endsWith("/pay-rolls/schema"))
      data = {
        title: "通道轮询",
        fields,
        permissions: { create: true, update: true, delete: false },
      };
    else if (path.endsWith("/pay-rolls/routing-options"))
      data = {
        available: true,
        items: [
          { id: "7", type: "alipay", methods: ["page"] },
          { id: "8", type: "alipay", methods: ["page"] },
          { id: "9", type: "wxpay", methods: ["native"] },
        ],
      };
    else if (path.endsWith("/pay-rolls/drafts")) {
      expect(req.method()).toBe("POST");
      expect(req.headers()["x-csrf-token"]).toBe("synthetic-roll-csrf");
      const body = req.postDataJSON();
      writes.push({ path, body });
      expect(Object.keys(body).sort()).toEqual([
        "creation_mode",
        "name",
        "strategy",
        "type",
      ]);
      data = {
        id: "42",
        name: body.name,
        type: body.type,
        strategy: body.strategy,
        merchant_id: "0",
        enabled: false,
        default_route: false,
        channel_ids: "",
        limits: "{}",
        rules: "{}",
      };
      records.unshift(data as Record<string, unknown>);
    } else if (path.endsWith("/pay-rolls/41/channels")) {
      if (req.method() === "POST") {
        const body = req.postDataJSON();
        writes.push({ path, body });
        expect(req.headers()["x-csrf-token"]).toBe("synthetic-roll-csrf");
        expect(body).toEqual({
          expected_revision: "a".repeat(64),
          channel_ids: "7:2,8:3",
          confirm_enabled: true,
        });
        records.find((r) => r.id === "41")!.channel_ids = body.channel_ids;
        revision = "b".repeat(64);
        if (unknownSave) {
          failedRead = true;
          return route.abort();
        }
      } else if (failedRead)
        return route.fulfill({
          status: 503,
          json: { code: -1, message: "Synthetic readback unavailable" },
        });
      data = { id: "41", revision, fields: records.find((r) => r.id === "41") };
    } else if (path.endsWith("/pay-rolls/42")) {
      const draft = records.find((r) => r.id === "42")!;
      if (req.method() === "PATCH") {
        const body = req.postDataJSON();
        writes.push({ path, body });
        expect(req.headers()["x-csrf-token"]).toBe("synthetic-roll-csrf");
        expect(body).toMatchObject({
          name: "仅改名称的安全草稿",
          type: "alipay",
          strategy: "weighted",
          merchant_id: "0",
          enabled: false,
          default_route: false,
          channel_ids: "",
          limits: "{}",
          rules: "{}",
        });
        Object.assign(draft, body);
      }
      data = draft;
    } else if (path.endsWith("/pay-rolls/41"))
      data = records.find((r) => r.id === "41");
    else if (path.endsWith("/pay-rolls"))
      data = { items: records, total: records.length };
    if (
      !["GET", "POST"].includes(req.method()) &&
      !(path.endsWith("/pay-rolls/42") && req.method() === "PATCH")
    )
      throw Error("unexpected generic write: " + path);
    return route.fulfill({ json: { code: 0, data } });
  });
  await page.goto("/admin/payapi/pay-roll");
  await expect(
    page.getByRole("cell", { name: "运行轮询", exact: true }),
  ).toBeVisible();
  return {
    writes,
    errors,
    records,
    allowReadback: () => {
      failedRead = false;
    },
  };
}

for (const width of [1280, 375])
  test(`three-field inert draft and independent active channel configuration at ${width}`, async ({
    page,
  }) => {
    await page.setViewportSize({ width, height: 1000 });
    const f = await fixture(page);
    const create = page.getByRole("button", {
      name: "新增轮询配置",
      exact: true,
    });
    await create.click();
    const dialog = page.getByRole("dialog", {
      name: "新增轮询组",
      exact: true,
    });
    await expect(dialog.getByLabel("显示名称", { exact: true })).toBeFocused();
    await expect(dialog.locator("input")).toHaveCount(1);
    await expect(dialog.locator("select")).toHaveCount(2);
    await dialog.getByLabel("显示名称", { exact: true }).fill("本地安全草稿");
    await dialog.getByLabel("支付方式", { exact: true }).selectOption("alipay");
    await dialog
      .getByLabel("轮询方式", { exact: true })
      .selectOption({ label: "按权重随机轮询" });
    expect(f.writes).toHaveLength(0);
    await dialog
      .getByRole("button", { name: "创建轮询组", exact: true })
      .click();
    await expect(dialog).toHaveCount(0);
    await expect(create).toBeFocused();
    expect(f.writes).toHaveLength(1);
    expect(f.records[0]).toMatchObject({
      enabled: false,
      default_route: false,
      channel_ids: "",
      merchant_id: "0",
    });
    const draftRow = page.getByRole("row").filter({
      has: page.getByRole("cell", { name: "本地安全草稿", exact: true }),
    });
    await draftRow.getByRole("button", { name: "编辑", exact: true }).click();
    const draftEditor = page.getByRole("dialog");
    await expect(draftEditor.locator("#field-channel_ids")).toHaveValue("");
    await expect(draftEditor.locator("#field-channel_ids")).not.toHaveAttribute(
      "aria-required",
      "true",
    );
    await draftEditor.locator("#field-name").fill("仅改名称的安全草稿");
    await draftEditor
      .getByRole("button", { name: "保存", exact: true })
      .click();
    await expect(draftEditor).toHaveCount(0);
    await expect(
      page.getByRole("cell", { name: "仅改名称的安全草稿", exact: true }),
    ).toBeVisible();
    expect(f.records.find((r) => r.id === "42")).toMatchObject({
      name: "仅改名称的安全草稿",
      channel_ids: "",
      enabled: false,
      default_route: false,
    });
    expect(f.writes).toHaveLength(2);
    const row = page.getByRole("row").filter({
      has: page.getByRole("cell", { name: "运行轮询", exact: true }),
    });
    const configure = row.getByRole("button", {
      name: "配置通道",
      exact: true,
    });
    await configure.click();
    const channels = page.getByRole("dialog", {
      name: "配置通道",
      exact: true,
    });
    await expect(
      channels.getByLabel("选择通道 7", { exact: true }),
    ).toBeChecked();
    await expect(channels.locator('[data-channel-id="9"]')).toHaveCount(0);
    await channels.getByLabel("选择通道 8", { exact: true }).check();
    await channels.getByLabel("通道 8 权重", { exact: true }).fill("3");
    await expect(
      channels.getByRole("button", { name: "保存通道配置", exact: true }),
    ).toBeDisabled();
    await channels.getByLabel("确认修改已启用轮询组", { exact: true }).check();
    const box = await channels.boundingBox();
    expect(box!.x).toBeGreaterThanOrEqual(0);
    expect(box!.x + box!.width).toBeLessThanOrEqual(width + 1);
    await channels
      .getByRole("button", { name: "保存通道配置", exact: true })
      .click();
    await expect(channels).toHaveCount(0);
    await expect(configure).toBeFocused();
    expect(f.writes).toHaveLength(3);
    expect(f.records.find((r) => r.id === "41")).toMatchObject({
      enabled: true,
      default_route: false,
      merchant_id: "101",
      limits: '{"daily_count":7}',
      rules: '{"7":{"daily_count":2}}',
      channel_ids: "7:2,8:3",
    });
    await row.getByRole("button", { name: "编辑", exact: true }).click();
    await expect(
      page.getByRole("dialog").locator("#field-channel_ids"),
    ).toBeVisible();
    await page
      .getByRole("dialog")
      .getByRole("button", { name: "取消", exact: true })
      .click();
    expect(
      await page.evaluate(
        () => document.documentElement.scrollWidth <= innerWidth,
      ),
    ).toBe(true);
    expect(f.errors).toEqual([]);
  });

test("unknown channel save stays locked across failed explicit readback and does not repeat POST", async ({
  page,
}) => {
  await page.setViewportSize({ width: 375, height: 1000 });
  const f = await fixture(page, true);
  await page.getByRole("button", { name: "配置通道", exact: true }).click();
  const dialog = page.getByRole("dialog", { name: "配置通道", exact: true });
  await dialog.getByLabel("选择通道 8", { exact: true }).check();
  await dialog.getByLabel("通道 8 权重", { exact: true }).fill("3");
  await dialog.getByLabel("确认修改已启用轮询组", { exact: true }).check();
  await dialog
    .getByRole("button", { name: "保存通道配置", exact: true })
    .click();
  await expect(dialog).toContainText("保存结果尚未确认");
  await pageReload248(page);
  await page.getByRole("button", { name: "配置通道", exact: true }).click();
  await expect(dialog).toContainText("Synthetic readback unavailable");
  await expect(
    dialog.getByRole("button", { name: "保存通道配置", exact: true }),
  ).toBeDisabled();
  expect(f.writes).toHaveLength(1);
  f.allowReadback();
  await pageReload248(page);
  await page.getByRole("button", { name: "配置通道", exact: true }).click();
  await expect(dialog.getByLabel("选择通道 8", { exact: true })).toBeChecked();
  await expect(dialog).not.toContainText("保存结果尚未确认");
  expect(f.writes).toHaveLength(1);
  expect(f.errors).toEqual([]);
});
