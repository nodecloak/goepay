import { test, expect } from "@playwright/test";
import fixture from "./fixtures/core-risk177-http.json" with { type: "json" };
import { coreRiskFields } from "../src/core-payment-risk";

// Public DTO bytes were exported from actual migrated synthetic HTTP tests.
// Mutation replies here are mocked; backend enforcement has separate PG tests.
for (const width of [1440, 375])
  for (const lost of [false, true]) {
    test(`original risk fields, confirmation and ${lost ? "lost reply recovery" : "exact save"} at ${width}`, async ({
      page,
    }) => {
      await page.setViewportSize({ width, height: 950 });
      let policy = structuredClone(fixture.policy),
        reads = 0;
      const writes: {
        path: string;
        body: unknown;
        csrf: string | undefined;
      }[] = [];
      await page.route("**/console/v1/**", async (route) => {
        const r = route.request(),
          path = new URL(r.url()).pathname;
        let data: unknown = { items: [], total: 0 };
        if (path.endsWith("/session"))
          data = {
            user: { id: "1", role: "admin", admin_class: "super" },
            csrf_token: "synthetic-risk187",
          };
        else if (path.endsWith("/settings/schema"))
          data = {
            title: "系统设置",
            fields: [
              { name: "key", type: "select", options: ["site_name"] },
              { name: "value", type: "string" },
            ],
            permissions: { create: true, update: true, delete: false },
          };
        else if (path.endsWith("/settings/merchant-access"))
          data = {
            configured: false,
            revision: "0",
            fingerprint: "a".repeat(64),
            close_keylogin: 1,
            user_refund: 0,
          };
        else if (path.endsWith("/payment-risk/actions")) data = fixture.actions;
        else if (path.endsWith("/payment-risk")) {
          if (r.method() === "PATCH") {
            const body = r.postDataJSON();
            writes.push({ path, body, csrf: r.headers()["x-csrf-token"] });
            expect(body.expected_fingerprint).toBe(fixture.policy.fingerprint);
            policy = {
              ...policy,
              revision: "2",
              fingerprint: "b".repeat(64),
              settings: body.settings,
            };
            if (lost) return route.abort("failed");
          } else reads++;
          data = policy;
        } else if (r.method() !== "GET")
          throw Error("unexpected mutation " + path);
        await route.fulfill({ json: { code: 0, data } });
      });
      await page.goto("/admin/system/settings");
      await page.getByRole("tab", { name: "风控检测", exact: true }).click();
      const region = page.getByRole("region", {
        name: "核心付款风控",
        exact: true,
      });
      for (const field of coreRiskFields.filter(
        (f) =>
          !f.parent ||
          policy.settings[f.parent as keyof typeof policy.settings] === "1",
      ))
        await expect(
          region.getByLabel(field.label, { exact: true }),
        ).toBeVisible();
      await expect(region).toContainText("9007199254741774");
      await region.getByLabel("IP 每日支付次数", { exact: true }).fill("18");
      const save = region.getByRole("button", {
        name: "核对并保存风控设置",
        exact: true,
      });
      await save.click();
      const dialog = page.getByRole("dialog", {
        name: "确认风控设置",
        exact: true,
      });
      await expect(dialog).toContainText("18");
      await page.keyboard.press("Escape");
      await expect(dialog).toHaveCount(0);
      await expect(save).toBeFocused();
      expect(writes).toHaveLength(0);
      await save.click();
      await dialog
        .getByRole("button", { name: "确认执行", exact: true })
        .click();
      if (lost) {
        await expect(
          region.getByRole("alert").filter({ hasText: "结果无法确认" }),
        ).toBeVisible();
        await expect(save).toBeDisabled();
        expect(writes).toHaveLength(1);
        await region
          .getByRole("button", { name: "刷新风控设置", exact: true })
          .click();
        await expect(region).toContainText("修订 2");
        expect(reads).toBe(2);
      } else
        await expect(region.getByRole("status")).toContainText(
          "风控设置已保存",
        );
      expect(writes).toHaveLength(1);
      expect(writes[0]!.csrf).toBe("synthetic-risk187");
      expect(
        (writes[0]!.body as { settings: Record<string, string> }).settings
          .pay_iplimit,
      ).toBe("18");
      expect(
        await page.evaluate(
          () => document.documentElement.scrollWidth > innerWidth,
        ),
      ).toBe(false);
    });
  }
