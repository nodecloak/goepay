import { test, expect, type Page } from "@playwright/test";
import { publicSiteFixture191 } from "./fixtures/site191";

const scenarios = [
  {
    resource: "merchants",
    url: "/admin/merchant/merchant-list",
    create: "新增商户",
    title: "添加商户",
  },
  {
    resource: "groups",
    url: "/admin/merchant/group-list",
    create: "新增用户组",
    title: "新增用户组",
  },
  {
    resource: "pay-rolls",
    url: "/admin/payapi/pay-roll",
    create: "新增轮询配置",
    title: "新增轮询组",
  },
  {
    resource: "domains",
    url: "/admin/merchant/domain",
    create: "新增授权域名",
    title: "新增授权域名",
  },
] as const;
async function fixture(page: Page, resource: string, deferSave = false) {
  const writes: unknown[] = [],
    errors: string[] = [];
  let finish: () => void = () => {};
  const pending = new Promise<void>((resolve) => (finish = resolve));
  page.on("pageerror", (error) => errors.push(error.message));
  const fields: any[] =
    resource === "merchants"
      ? [
          {
            name: "creation_mode",
            label: "新增方式",
            type: "select",
            options: [
              { value: "account", label: "商户账号" },
              { value: "api", label: "仅API" },
            ],
          },
          { name: "name", label: "名称", type: "string", required: true },
          {
            name: "login_username",
            label: "后台登录账号（可选）",
            type: "string",
          },
          {
            name: "login_password",
            label: "后台登录密码（首次设置或重置）",
            type: "password",
            write_only: true,
          },
          { name: "phone", label: "手机号码", type: "string" },
          { name: "email", label: "电子邮箱", type: "email" },
          { name: "group_id", label: "基础用户组 ID", type: "string" },
          { name: "enabled", label: "启用", type: "boolean", default: true },
        ]
      : resource === "groups"
        ? [
            { name: "name", label: "名称", type: "string", required: true },
            { name: "description", label: "说明", type: "string" },
            { name: "enabled", label: "启用", type: "boolean", default: true },
          ]
        : resource === "domains"
          ? [
              {
                name: "merchant_id",
                label: "商户 ID",
                type: "string",
                required: true,
              },
              { name: "domain", label: "域名", type: "string", required: true },
              {
                name: "enabled",
                label: "启用",
                type: "boolean",
                default: true,
              },
            ]
          : [
              { name: "name", label: "名称", type: "string", required: true },
              { name: "enabled", label: "启用", type: "boolean" },
            ];
  const record = {
    id: "25001",
    name: "本地抽屉商户250",
    phone: "13000025001",
    email: "owner250@example.invalid",
    enabled: true,
    group_id: "0",
  };
  await page.route("**/console/v1/**", async (route) => {
    const req = route.request(),
      p = new URL(req.url()).pathname;
    const site = publicSiteFixture191(route);
    if (site) return site;
    let data: unknown = { items: [], total: 0 };
    if (p.endsWith("/session"))
      data = {
        user: {
          id: "1",
          name: "本地管理员250",
          role: "admin",
          admin_class: "super",
        },
        csrf_token: "synthetic250",
      };
    else if (p === `/console/v1/admin/resources/${resource}/schema`)
      data = {
        title:
          resource === "merchants"
            ? "商户管理"
            : resource === "groups"
              ? "用户组"
              : resource === "domains"
                ? "授权域名"
                : "轮询组",
        permissions: { create: true, update: true, delete: true },
        fields,
      };
    else if (p === `/console/v1/admin/resources/${resource}`)
      data = {
        items: resource === "merchants" ? [record] : [],
        total: resource === "merchants" ? 1 : 0,
      };
    else if (p === `/console/v1/admin/resources/${resource}/25001`) {
      data = record;
      if (req.method() === "PATCH") {
        writes.push(req.postDataJSON());
        if (deferSave) await pending;
        data = { ...record, ...req.postDataJSON() };
      }
    } else if (p === "/console/v1/admin/resources/groups")
      data = {
        items: [{ id: "0", name: "默认用户组", enabled: true }],
        total: 1,
      };
    else if (p.endsWith("/security/payment-password"))
      data = {
        configured: false,
        enforced: false,
        credential_epoch: 0,
        totp_required: false,
      };
    if (req.method() !== "GET" && req.method() !== "PATCH")
      throw Error("Unexpected local mutation " + p);
    await route.fulfill({ json: { code: 0, data } });
  });
  return { writes, errors, finish };
}
async function settledDrawer(page: Page, title: string, width: number) {
  const drawer = page.getByRole("dialog", { name: title, exact: true });
  await expect(drawer).toBeVisible();
  await expect
    .poll(async () =>
      drawer.evaluate((el) => {
        const b = el.getBoundingClientRect();
        return (
          Math.abs(b.right - innerWidth) < 2 &&
          Math.abs(b.top) < 2 &&
          Math.abs(b.height - innerHeight) < 2
        );
      }),
    )
    .toBe(true);
  expect(
    await drawer.evaluate((el) => getComputedStyle(el).animationName),
  ).toBe("pa-drawer-in");
  const box = (await drawer.boundingBox())!;
  expect(box.width).toBeLessThanOrEqual(width + 0.1);
  expect(
    await page.evaluate(
      () => document.documentElement.scrollWidth <= innerWidth + 1,
    ),
  ).toBe(true);
  return drawer;
}
for (const scenario of scenarios)
  for (const width of [375, 1440]) {
    test(`${scenario.resource} create drawer slides in and cancels without writes at ${width}`, async ({
      page,
    }, info) => {
      await page.setViewportSize({ width, height: 950 });
      const f = await fixture(page, scenario.resource);
      await page.goto(scenario.url);
      const opener = page.getByRole("button", {
        name: scenario.create,
        exact: true,
      });
      await opener.click();
      const drawer = await settledDrawer(page, scenario.title, width);
      await drawer
        .getByRole("heading", { name: scenario.title, exact: true })
        .click();
      await expect(drawer).toBeVisible();
      await page.screenshot({
        path: info.outputPath("drawer.png"),
        fullPage: true,
      });
      if (width >= 1000) await page.mouse.click(10, 400);
      else await page.keyboard.press("Escape");
      await expect(drawer).toHaveCount(0);
      await expect(opener).toBeFocused();
      expect(f.writes).toEqual([]);
      expect(f.errors).toEqual([]);
    });
  }
test("merchant edit outside cancellation clears private drafts and does not cancel an in-flight save", async ({
  page,
}) => {
  await page.setViewportSize({ width: 1440, height: 950 });
  const f = await fixture(page, "merchants", true);
  await page.goto("/admin/merchant/merchant-list");
  const edit = page.getByRole("button", { name: "编辑", exact: true });
  await edit.click();
  let drawer = await settledDrawer(page, "编辑商户管理", 1440);
  const password = drawer.getByLabel("后台登录密码（首次设置或重置）", {
    exact: true,
  });
  await password.fill("discarded-controlled250");
  await page.mouse.click(10, 400);
  await expect(drawer).toHaveCount(0);
  expect(f.writes).toEqual([]);
  await edit.click();
  drawer = await settledDrawer(page, "编辑商户管理", 1440);
  await expect(password).toHaveValue("");
  await drawer.getByLabel("名称", { exact: true }).fill("本地保存250");
  await drawer.getByRole("button", { name: "保存", exact: true }).click();
  await expect.poll(() => f.writes.length).toBe(1);
  await page.mouse.click(10, 400);
  await expect(drawer).toBeVisible();
  await expect(
    drawer.getByRole("button", { name: "关闭", exact: true }),
  ).toBeDisabled();
  f.finish();
  await expect(drawer).toHaveCount(0);
  expect(f.writes).toHaveLength(1);
});
test("drawer respects reduced-motion preference and outside dismissal", async ({
  page,
}) => {
  await page.emulateMedia({ reducedMotion: "reduce" });
  await page.setViewportSize({ width: 1440, height: 950 });
  const f = await fixture(page, "merchants");
  await page.goto("/admin/merchant/merchant-list");
  await page.getByRole("button", { name: "新增商户", exact: true }).click();
  const drawer = page.getByRole("dialog", { name: "添加商户", exact: true });
  await expect(drawer).toBeVisible();
  expect(
    await drawer.evaluate((el) => getComputedStyle(el).animationName),
  ).toBe("none");
  await page.mouse.click(10, 400);
  await expect(drawer).toHaveCount(0);
  expect(f.writes).toEqual([]);
});
