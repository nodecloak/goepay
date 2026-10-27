import { publicSiteFixture191 } from "./fixtures/site191";
import { test, expect } from "@playwright/test";
const ok = (data: unknown) => ({
  status: 200,
  contentType: "application/json",
  body: JSON.stringify({ code: 0, data }),
});
test("subject policy supplemental entry uses its actual schema and operator direct route never reads it", async ({
  page,
}) => {
  let adminClass = "operator",
    reads = 0;
  await page.route("**/console/v1/**", (route) => {
    const siteReply191 = publicSiteFixture191(route);
    if (siteReply191) return siteReply191;
    const p = new URL(route.request().url()).pathname;
    if (p === "/console/v1/public/layout") {
      expect(route.request().method()).toBe("GET");
      expect(new URL(route.request().url()).search).toBe("");
      return route.fulfill(ok({ zhuce: "", footer: "", format: "html" }));
    }
    if (p.endsWith("/session"))
      return route.fulfill(
        ok({
          user: {
            id: "1",
            name: "Admin",
            role: "admin",
            admin_class: adminClass,
          },
          csrf_token: "csrf",
        }),
      );
    reads++;
    expect(p).toContain("/resources/order-subject-policies");
    if (p.endsWith("/schema"))
      return route.fulfill(
        ok({
          title: "订单名称限制策略",
          description: "仅影响新的上游发送，历史资金签名原名不变。",
          fields: [
            {
              name: "scope",
              label: "范围",
              type: "select",
              required: true,
              options: ["global", "merchant", "channel"],
            },
            {
              name: "scope_id",
              label: "范围ID",
              type: "string",
              required: true,
            },
            { name: "template", label: "模板", type: "text" },
          ],
          permissions: { create: true, update: true, delete: true },
        }),
      );
    return route.fulfill(
      ok({
        items: [
          { id: "1", scope: "global", scope_id: "0", template: "Template" },
        ],
        total: 1,
      }),
    );
  });
  await page.goto("/admin/system/order-subject-policies");
  await expect(
    page.getByText("您没有访问此页面的权限", { exact: true }),
  ).toBeVisible();
  expect(reads).toBe(0);
  adminClass = "super";
  await page.reload();
  await expect(
    page.getByRole("heading", { name: "订单名称限制策略", exact: true }),
  ).toBeVisible();
  await expect(
    page.getByText("仅影响新的上游发送，历史资金签名原名不变。"),
  ).toBeVisible();
  await expect(
    page.getByRole("cell", { name: "Template", exact: true }),
  ).toBeVisible();
});
