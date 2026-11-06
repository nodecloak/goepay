import { test, expect } from "@playwright/test";
for (const width of [1280, 375])
  test(`merchant directory opens original target login logs, pages and returns focus at ${width}`, async ({
    page,
  }) => {
    await page.setViewportSize({ width, height: 1000 });
    const reads: URL[] = [],
      writes: string[] = [];
    await page.route("**/console/v1/**", (route) => {
      const req = route.request(),
        u = new URL(req.url()),
        p = u.pathname;
      if (req.method() !== "GET") writes.push(p);
      let data: unknown = { items: [], total: 0 };
      if (p.endsWith("/session"))
        data = {
          user: {
            id: "1",
            name: "admin",
            role: "admin",
            admin_class: "operator",
          },
          csrf_token: "admin126-log",
        };
      else if (p.endsWith("/resources/merchants/schema"))
        data = {
          title: "商户管理",
          permissions: { create: false, update: false, delete: false },
          fields: [{ name: "name", label: "名称", type: "string" }],
        };
      else if (p.endsWith("/resources/merchants"))
        data = {
          items: [{ id: "9223372036854775807", name: "本地日志商户" }],
          total: 1,
        };
      else if (p.endsWith("/merchants/9223372036854775807/login-log")) {
        reads.push(u);
        data = {
          items: [
            {
              id: u.searchParams.get("page") === "2" ? "1" : "2",
              ip:
                u.searchParams.get("page") === "2" ? "192.0.2.1" : "192.0.2.2",
              type: "",
              city: "",
              created_at: "2026-10-25T00:00:00Z",
            },
          ],
          total: 21,
        };
      }
      return route.fulfill({
        contentType: "application/json",
        body: JSON.stringify({ code: 0, data }),
      });
    });
    await page.goto("/admin/merchant/merchant-list");
    const trigger = page.getByRole("button", {
      name: "商户关联记录 9223372036854775807",
      exact: true,
    });
    await trigger.click();
    await page.getByRole("menuitem", { name: "登录日志", exact: true }).click();
    const dialog = page.getByRole("dialog", {
      name: "商户 9223372036854775807 登录日志",
      exact: true,
    });
    await expect(
      dialog.getByRole("cell", { name: "192.0.2.2", exact: true }),
    ).toBeVisible();
    await expect(dialog.getByRole("columnheader")).toHaveText([
      "登录方式",
      "IP地址",
      "归属地",
      "时间",
    ]);
    await expect(
      dialog.getByRole("cell", { name: "2026-10-25 08:00:00", exact: true }),
    ).toBeVisible();
    await dialog.getByRole("button", { name: "下一页", exact: true }).click();
    await expect(
      dialog.getByRole("cell", { name: "192.0.2.1", exact: true }),
    ).toBeVisible();
    expect(reads.at(-1)!.searchParams.get("page")).toBe("2");
    await expect(
      dialog.getByRole("button", { name: "关闭登录日志", exact: true }),
    ).toBeFocused();
    await dialog.press("Escape");
    await expect(dialog).toHaveCount(0);
    await expect(trigger).toBeFocused();
    expect(writes).toHaveLength(0);
    expect(
      await page.evaluate(
        () => document.documentElement.scrollWidth > innerWidth,
      ),
    ).toBe(false);
  });
