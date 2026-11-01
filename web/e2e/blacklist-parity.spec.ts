import { test, expect } from "@playwright/test";

test("blacklist search/reset and original table columns keep the existing editor", async ({
  page,
}, testInfo) => {
  const listQueries: URLSearchParams[] = [];
  const writes: string[] = [];
  const item = {
    id: "9007199254740993",
    kind: "domain",
    value: "merchant.example",
    reason: "synthetic",
    enabled: true,
    expires_at: null,
    created_at: "2026-11-01T01:02:03Z",
  };
  const fields = [
    {
      name: "kind",
      label: "黑名单类型",
      type: "select",
      options: [{ label: "域名", value: "domain" }],
    },
    { name: "value", label: "黑名单内容", type: "string" },
    { name: "reason", label: "备注", type: "string" },
    { name: "enabled", label: "启用", type: "boolean" },
    {
      name: "expires_at",
      label: "到期时间（空为永久）",
      type: "string",
      read_only: true,
    },
    { name: "created_at", label: "添加时间", type: "string", read_only: true },
  ];
  await page.route("**/console/v1/**", async (route) => {
    const r = route.request(),
      u = new URL(r.url()),
      p = u.pathname;
    const ok = (data: unknown) =>
      route.fulfill({ status: 200, json: { code: 0, data } });
    if (r.method() !== "GET") {
      writes.push(p);
      throw Error("Unexpected production-shaped write in read-only UI test");
    }
    if (p.endsWith("/session"))
      return ok({
        user: { id: "1", name: "Admin", role: "admin", admin_class: "super" },
        csrf_token: "synthetic-csrf",
      });
    if (p.endsWith("/security/payment-password"))
      return ok({
        configured: false,
        enforced: false,
        credential_epoch: 0,
        totp_required: false,
      });
    if (p.endsWith("/resources/blacklist/schema"))
      return ok({
        title: "黑名单管理",
        fields,
        permissions: { create: true, update: true, delete: true },
        exportable: true,
        batch_delete: true,
        filters: [
          {
            param: "kind",
            label: "黑名单类型",
            options: [
              { value: "", label: "全部类型" },
              { value: "domain", label: "域名" },
            ],
          },
        ],
      });
    if (p.endsWith("/resources/blacklist/" + item.id)) return ok(item);
    if (p.endsWith("/resources/blacklist")) {
      listQueries.push(u.searchParams);
      return ok({ items: [item], total: 60 });
    }
    return ok({ items: [], total: 0 });
  });
  await page.goto("/admin/trade/blacklist");
  await expect(
    page.getByPlaceholder("黑名单内容", { exact: true }),
  ).toBeVisible();
  await expect(page.getByRole("columnheader")).toHaveText([
    "",
    "ID",
    "黑名单类型",
    "黑名单内容",
    "添加时间",
    "过期时间",
    "备注",
    "操作",
  ]);
  await expect(
    page.getByRole("cell", { name: "2026-11-01 09:02:03", exact: true }),
  ).toBeVisible();
  await expect(
    page.getByRole("cell", { name: "永久", exact: true }),
  ).toBeVisible();
  await page
    .getByPlaceholder("黑名单内容", { exact: true })
    .fill("merchant.example");
  await page.getByRole("button", { name: "黑名单类型", exact: true }).click();
  await page.getByRole("option", { name: "域名", exact: true }).click();
  await expect.poll(() => listQueries.at(-1)?.get("kind")).toBe("domain");
  await page.getByRole("button", { name: "2", exact: true }).click();
  await expect.poll(() => listQueries.at(-1)?.get("page")).toBe("2");
  await page
    .getByRole("checkbox", { name: "选择记录 " + item.id, exact: true })
    .check();
  const before = listQueries.length;
  await page.getByRole("button", { name: "重置", exact: true }).click();
  await expect.poll(() => listQueries.length).toBe(before + 1);
  expect(listQueries.at(-1)!.get("q")).toBe("");
  expect(listQueries.at(-1)!.get("kind")).toBeNull();
  expect(listQueries.at(-1)!.get("page")).toBe("1");
  await expect(
    page.getByRole("checkbox", { name: "选择记录 " + item.id, exact: true }),
  ).not.toBeChecked();
  await page.screenshot({
    path: testInfo.outputPath("blacklist-parity.png"),
    fullPage: true,
  });
  await page.getByRole("button", { name: "编辑", exact: true }).click();
  await expect(
    page.getByRole("checkbox", { name: "启用", exact: true }),
  ).toBeChecked();
  expect(writes).toEqual([]);
});
