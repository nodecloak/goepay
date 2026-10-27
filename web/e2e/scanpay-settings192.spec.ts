import { test, expect } from "@playwright/test";
import { publicSiteFixture191 } from "./fixtures/site191";
test("original merchant settings scan switch uses protected CAS confirmation", async ({
  page,
}) => {
  const writes: any[] = [];
  let state = {
    scanpay_open: "1",
    configured: true,
    revision: "1",
    fingerprint: "b".repeat(64),
    can_update: true,
  };
  await page.route("**/console/v1/**", async (route) => {
    const publicSite = publicSiteFixture191(route);
    if (publicSite) return publicSite;
    const r = route.request(),
      p = new URL(r.url()).pathname;
    let data: any = {};
    if (p.endsWith("/session"))
      data = {
        user: {
          id: "1",
          role: "admin",
          admin_class: "super",
          name: "本地管理员",
        },
        csrf_token: "scan192-admin-csrf",
      };
    else if (p.endsWith("/settings/schema"))
      data = {
        title: "系统设置",
        permissions: { create: true, update: true, delete: true },
        fields: [
          { name: "key", type: "select", options: [] },
          { name: "value", type: "string" },
        ],
      };
    else if (p.endsWith("/resources/settings")) data = { items: [], total: 0 };
    else if (p.endsWith("/settings/scanpay")) {
      if (r.method() === "PATCH") {
        expect(r.headers()["x-csrf-token"]).toBe("scan192-admin-csrf");
        writes.push(r.postDataJSON());
        state = {
          ...state,
          ...r.postDataJSON(),
          revision: "2",
          fingerprint: "c".repeat(64),
        };
      }
      data = state;
    } else if (p.endsWith("/settings/merchant-access"))
      data = {
        close_keylogin: "0",
        user_refund: "1",
        can_update: true,
        fingerprint: "a".repeat(64),
      };
    else if (r.method() !== "GET")
      throw new Error("unexpected settings mutation " + p);
    return route.fulfill({
      contentType: "application/json",
      body: JSON.stringify({ code: 0, data }),
    });
  });
  await page.goto("/admin/system/settings");
  await page.getByRole("tab", { name: "商户相关", exact: true }).click();
  const toggle = page.getByRole("checkbox", {
    name: "开启付款码支付",
    exact: true,
  });
  await expect(toggle).toBeChecked();
  await toggle.uncheck();
  await page
    .getByRole("button", { name: "保存付款码设置", exact: true })
    .click();
  expect(writes).toHaveLength(0);
  await page
    .getByRole("button", { name: "确认关闭付款码支付", exact: true })
    .click();
  expect(writes).toEqual([
    { scanpay_open: "0", expected_fingerprint: "b".repeat(64) },
  ]);
  await expect(toggle).not.toBeChecked();
  await expect(
    page.getByRole("button", { name: "保存付款码设置", exact: true }),
  ).toBeDisabled();
});
