import { test, expect, type Page } from "@playwright/test";
import { closedLoginCaptchaFixture205 } from "./fixtures/login-captcha205";
import { publicSiteFixture191 } from "./fixtures/site191";

test("administrator editor cancels its name draft and saves a rename against the original username", async ({
  page,
}) => {
  const writes: Record<string, unknown>[] = [];
  let row = {
    id: "2",
    username: "staff245",
    display_name: "测试管理员",
    admin_class: "operator",
    enabled: true,
    auth_version: "2",
  };
  await page.route("**/console/v1/**", async (route) => {
    const req = route.request(),
      p = new URL(req.url()).pathname;
    let data: unknown = {};
    if (p.endsWith("/session"))
      data = {
        user: {
          id: "1",
          name: "模拟管理员",
          role: "admin",
          admin_class: "super",
        },
        csrf_token: "synthetic245",
      };
    else if (p.endsWith("/admin-users/schema"))
      data = {
        title: "管理员",
        permissions: { create: true, update: true, delete: true },
        actions: [],
        fields: [
          {
            name: "username",
            label: "用户名",
            type: "string",
            required: true,
            max_length: 100,
          },
          {
            name: "display_name",
            label: "名称",
            type: "string",
            required: true,
            max_length: 100,
          },
          {
            name: "admin_class",
            label: "权限",
            type: "select",
            options: [
              { value: "operator", label: "业务管理员" },
              { value: "super", label: "超级管理员" },
            ],
          },
          {
            name: "password",
            label: "密码",
            type: "password",
            write_only: true,
            max_length: 1024,
          },
          { name: "enabled", label: "启用", type: "boolean" },
        ],
      };
    else if (p.endsWith("/admin-users/2")) {
      if (req.method() === "PATCH") {
        const body = req.postDataJSON();
        writes.push(body);
        row = { ...row, username: body.username || row.username };
      }
      data = row;
    } else if (p.endsWith("/admin-users")) data = { items: [row], total: 1 };
    return route.fulfill({ json: { code: 0, data } });
  });
  await page.goto("/admin/system/admin-user");
  await page.getByRole("button", { name: "编辑", exact: true }).click();
  const d = page.getByRole("dialog");
  await expect(d).not.toContainText("账号创建后不可更名");
  await expect(d).toContainText("修改用户名后，该管理员需重新登录");
  await expect(d.getByLabel("用户名", { exact: true })).toBeEditable();
  await d.getByLabel("用户名", { exact: true }).fill("discarded245");
  await d.getByRole("button", { name: "取消", exact: true }).click();
  expect(writes).toHaveLength(0);
  await page.getByRole("button", { name: "编辑", exact: true }).click();
  await expect(d.getByLabel("用户名", { exact: true })).toHaveValue("staff245");
  await d.getByLabel("用户名", { exact: true }).fill("updated245");
  await d.getByRole("button", { name: "保存", exact: true }).click();
  await expect(d).toHaveCount(0);
  expect(writes).toHaveLength(1);
  expect(writes[0]).toMatchObject({
    username: "updated245",
    expected_username: "staff245",
  });
  expect(writes[0]).not.toHaveProperty("password");
  await page.getByRole("button", { name: "编辑", exact: true }).click();
  await d.getByRole("button", { name: "保存", exact: true }).click();
  expect(writes).toHaveLength(2);
  expect(writes[1]).not.toHaveProperty("username");
  expect(writes[1]).not.toHaveProperty("expected_username");
});

async function fixture(page: Page, mode: "ok" | "conflict" | "unknown" = "ok") {
  const writes: unknown[] = [];
  let changed = false;
  await page.route("**/*", async (route) => {
    const req = route.request(),
      u = new URL(req.url());
    if (!["127.0.0.1", "localhost"].includes(u.hostname)) return route.abort();
    if (!u.pathname.startsWith("/console/v1/")) return route.continue();
    const captcha = closedLoginCaptchaFixture205(route);
    if (captcha) return captcha;
    const site = publicSiteFixture191(route);
    if (site) return site;
    const path = u.pathname;
    let data: unknown = {};
    if (path.endsWith("/session")) {
      if (changed)
        return route.fulfill({
          status: 401,
          json: { code: -1, message: "请重新登录" },
        });
      data = {
        user: {
          id: "1",
          name: "本地管理员245",
          role: "admin",
          admin_class: "super",
        },
        csrf_token: "synthetic245",
      };
    } else if (path.endsWith("/account/login-name")) {
      if (req.method() === "POST") {
        writes.push(req.postDataJSON());
        if (mode === "unknown") return route.abort("failed");
        if (mode === "conflict")
          return route.fulfill({
            status: 409,
            json: { code: -1, message: "用户名已被使用" },
          });
        changed = true;
        data = { username: "updated245", reauthenticate: true };
      } else data = { username: "original245", auth_version: "3" };
    } else if (path.endsWith("/security/payment-password"))
      data = {
        configured: false,
        enforced: false,
        credential_epoch: 0,
        totp_required: false,
      };
    else if (path.endsWith("/mfa")) data = { enabled: false };
    else if (req.method() !== "GET")
      throw Error("Unexpected local write: " + path);
    return route.fulfill({ json: { code: 0, data } });
  });
  await page.goto("/admin/system/change-password");
  await expect(
    page.getByRole("region", { name: "管理员账户", exact: true }),
  ).toContainText("original245");
  return writes;
}
for (const width of [375, 1280])
  test(`administrator username cancel then explicit save at ${width}`, async ({
    page,
  }, info) => {
    await page.setViewportSize({ width, height: 900 });
    const writes = await fixture(page);
    await page
      .getByRole("button", { name: "修改管理员用户名", exact: true })
      .click();
    let d = page.getByRole("dialog", { name: "修改管理员用户名", exact: true });
    await d.getByLabel("新用户名", { exact: true }).fill("discarded245");
    await d
      .getByLabel("当前登录密码", { exact: true })
      .fill("discarded-password245");
    await d.getByRole("button", { name: "取消", exact: true }).click();
    expect(writes).toHaveLength(0);
    await page
      .getByRole("button", { name: "修改管理员用户名", exact: true })
      .click();
    d = page.getByRole("dialog", { name: "修改管理员用户名", exact: true });
    await expect(d.getByLabel("新用户名", { exact: true })).toHaveValue(
      "original245",
    );
    await expect(d.getByLabel("当前登录密码", { exact: true })).toHaveValue("");
    await d.getByLabel("新用户名", { exact: true }).fill("bad name");
    await d
      .getByLabel("当前登录密码", { exact: true })
      .fill("synthetic-password245");
    await d.getByRole("button", { name: "保存用户名", exact: true }).click();
    expect(writes).toHaveLength(0);
    await expect(d.getByRole("alert")).toContainText("用户名须为");
    await d.getByLabel("新用户名", { exact: true }).fill("updated245");
    expect(await d.evaluate((el) => el.scrollWidth <= el.clientWidth + 1)).toBe(
      true,
    );
    await page.screenshot({
      path: info.outputPath(`username-${width}.png`),
      fullPage: true,
    });
    await d.getByRole("button", { name: "保存用户名", exact: true }).click();
    await expect(
      page.getByRole("button", { name: "登录工作台", exact: true }),
    ).toBeVisible();
    expect(writes).toEqual([
      {
        username: "updated245",
        expected_username: "original245",
        current_password: "synthetic-password245",
      },
    ]);
    expect(
      await page.evaluate(() =>
        JSON.stringify({ ...localStorage, ...sessionStorage }),
      ),
    ).not.toContain("password245");
  });
test("duplicate username remains editable but never retains its password", async ({
  page,
}) => {
  const writes = await fixture(page, "conflict");
  await page
    .getByRole("button", { name: "修改管理员用户名", exact: true })
    .click();
  const d = page.getByRole("dialog", { name: "修改管理员用户名", exact: true });
  await d.getByLabel("新用户名", { exact: true }).fill("updated245");
  await d
    .getByLabel("当前登录密码", { exact: true })
    .fill("synthetic-password245");
  await d.getByRole("button", { name: "保存用户名", exact: true }).click();
  await expect(d.getByRole("alert")).toHaveText("用户名已被使用");
  await expect(d.getByLabel("新用户名", { exact: true })).toHaveValue(
    "updated245",
  );
  await expect(d.getByLabel("当前登录密码", { exact: true })).toHaveValue("");
  await d.getByRole("button", { name: "取消", exact: true }).click();
  expect(writes).toHaveLength(1);
});
test("unknown username write cannot be replayed from the same dialog", async ({
  page,
}) => {
  const writes = await fixture(page, "unknown");
  await page
    .getByRole("button", { name: "修改管理员用户名", exact: true })
    .click();
  const d = page.getByRole("dialog", { name: "修改管理员用户名", exact: true });
  await d.getByLabel("新用户名", { exact: true }).fill("updated245");
  await d
    .getByLabel("当前登录密码", { exact: true })
    .fill("synthetic-password245");
  await d.getByRole("button", { name: "保存用户名", exact: true }).click();
  await expect(d).toHaveCount(0);
  await expect(
    page.getByRole("button", { name: "修改管理员用户名", exact: true }),
  ).toBeDisabled();
  await expect(page.getByRole("alert")).toContainText("未能确认用户名修改结果");
  expect(writes).toHaveLength(1);
});
