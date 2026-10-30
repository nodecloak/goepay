import { test, expect } from "@playwright/test";
import { publicSiteFixture191 } from "./fixtures/site191";
const png = Buffer.from(
  "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAusB9Wl6D1sAAAAASUVORK5CYII=",
  "base64",
);
for (const width of [375, 1280])
  test(`original home and global logo uploads remain separate and refresh console brand ${width}`, async ({
    page,
  }) => {
    await page.setViewportSize({ width, height: 1000 });
    const writes: any[] = [];
    let state: any = {
      revision: "0",
      fingerprint: "a".repeat(64),
      logo: null,
      icon: null,
    };
    await page.route("**/static/img/*.png", async (route) => {
      const slot = route.request().url().endsWith("/logo.png")
        ? "logo"
        : "icon";
      if (!state[slot]) return route.fulfill({ status: 404, body: "" });
      return route.fulfill({ contentType: "image/png", body: png });
    });
    await page.route("**/console/v1/**", async (route) => {
      const site = publicSiteFixture191(route);
      if (site) return site;
      const req = route.request(),
        path = new URL(req.url()).pathname;
      let data: any = {};
      if (path.endsWith("/session"))
        data = {
          user: {
            id: "1",
            role: "admin",
            admin_class: "super",
            name: "合成管理员",
          },
          csrf_token: "brand193-csrf",
        };
      else if (path.endsWith("/resources/settings/schema"))
        data = {
          permissions: { create: true, update: true, delete: true },
          fields: [
            { name: "key", type: "select", options: [] },
            { name: "value", type: "string" },
          ],
        };
      else if (path.endsWith("/resources/settings"))
        data = { items: [], total: 0 };
      else if (path.endsWith("/settings/merchant-access"))
        data = {
          close_keylogin: "0",
          user_refund: "1",
          can_update: true,
          fingerprint: "f".repeat(64),
        };
      else if (path.endsWith("/settings/branding")) data = state;
      else if (
        path.endsWith("/settings/branding/logo") ||
        path.endsWith("/settings/branding/icon")
      ) {
        expect(req.method()).toBe("POST");
        expect(req.headers()["x-csrf-token"]).toBe("brand193-csrf");
        expect(req.headers()["x-expected-fingerprint"]).toBe(state.fingerprint);
        expect(req.headers()["content-type"]).toContain("multipart/form-data");
        const slot = path.split("/").pop()!;
        writes.push({ slot, fingerprint: state.fingerprint });
        state = {
          ...state,
          revision: String(writes.length),
          fingerprint: (slot === "logo" ? "b" : "c").repeat(64),
          [slot]: {
            id: (slot === "logo" ? "1" : "2").repeat(64),
            mime: "image/png",
            width: 1,
            height: 1,
          },
        };
        data = state;
      } else if (req.method() !== "GET")
        throw new Error("unknown image write " + path);
      return route.fulfill({
        contentType: "application/json",
        body: JSON.stringify({ code: 0, data }),
      });
    });
    await page.goto("/admin/system/settings");
    const home = page.getByLabel("首页LOGO", { exact: true }),
      global = page.getByLabel("全局LOGO", { exact: true });
    await expect(home).toBeAttached();
    await expect(global).toBeAttached();
    await home.setInputFiles({
      name: "home.png",
      mimeType: "image/png",
      buffer: png,
    });
    await expect(
      page.getByRole("img", { name: "当前首页LOGO", exact: true }),
    ).toBeVisible();
    expect(writes).toEqual([{ slot: "logo", fingerprint: "a".repeat(64) }]);
    await expect(
      page.getByRole("img", { name: "全局品牌图标", exact: true }),
    ).toHaveCount(0);
    await global.setInputFiles({
      name: "global.png",
      mimeType: "image/png",
      buffer: png,
    });
    await expect(
      page.getByRole("img", { name: "当前全局LOGO", exact: true }),
    ).toBeVisible();
    if (width === 375)
      await page.getByRole("button", { name: "展开菜单", exact: true }).click();
    await expect(
      page.getByRole("img", { name: "全局品牌图标", exact: true }),
    ).toBeVisible();
    if (width === 375)
      // Tap the scrim beside the 200px menu: its center sits under the menu
      // once the slide-in ends, so a center click only won a race.
      await page
        .getByRole("button", { name: "关闭菜单", exact: true })
        .click({ position: { x: 350, y: 400 } });
    expect(writes[1]).toEqual({ slot: "icon", fingerprint: "b".repeat(64) });
    await expect(
      page.getByRole("img", { name: "当前首页LOGO", exact: true }),
    ).toBeVisible();
    expect(
      await page.evaluate(
        () => document.documentElement.scrollWidth <= innerWidth,
      ),
    ).toBe(true);
  });
