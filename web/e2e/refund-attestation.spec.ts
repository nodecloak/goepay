import { test, expect, type Page } from "@playwright/test";
import { publicSiteFixture191 } from "./fixtures/site191";

async function authorize(page: Page) {
  const dialog = page.getByRole("dialog", {
    name: "管理员资金二次授权",
    exact: true,
  });
  await dialog
    .getByRole("button", { name: "已核对摘要，验证资金密码", exact: true })
    .click();
  await dialog
    .getByLabel("本人管理员资金密码", { exact: true })
    .fill("synthetic-funds-password");
  await dialog
    .getByRole("button", { name: "确认资金密码", exact: true })
    .click();
  await dialog
    .getByRole("button", { name: "执行已核对的原操作", exact: true })
    .click();
  await expect(dialog).toHaveCount(0);
}

for (const width of [375, 1440])
  for (const finalReadback of [false, true])
    test(`unknown attestation stays locked through failed and pending readback at ${width}, final=${finalReadback}`, async ({
      page,
    }) => {
      await page.setViewportSize({ width, height: 950 });
      const errors: string[] = [],
        writes: unknown[] = [];
      page.on("pageerror", (error) => errors.push(error.message));
      let reads = 0,
        grants = 0,
        release!: () => void;
      const pending = new Promise<void>((resolve) => (release = resolve));
      const view = {
        refund_id: "12",
        refund_no: "RF-12",
        trade_no: "ORDER-LOCAL",
        merchant_id: "2",
        amount: "1.00",
        status: "unknown",
        sent: true,
        channel_id: "7",
        eligible: true,
        reason: "",
        states: ["succeeded", "failed"],
        attestation: null,
      };
      await page.route("**/console/v1/**", async (route) => {
        const req = route.request(),
          path = new URL(req.url()).pathname;
        const site = publicSiteFixture191(route);
        if (site) return site;
        const ok = (data: unknown) =>
          route.fulfill({ json: { code: 0, data } });
        if (path.endsWith("/session"))
          return ok({
            user: {
              id: "1",
              username: "admin",
              name: "本地核实管理员",
              role: "admin",
              admin_class: "super",
            },
            csrf_token: "synthetic-attestation",
          });
        if (path.endsWith("/security/payment-password"))
          return ok({
            configured: true,
            enforced: false,
            credential_epoch: 1,
            totp_required: false,
          });
        if (path.endsWith("/funds-authorizations/challenges")) {
          expect(req.postDataJSON().target).toBe(
            "/console/v1/admin/refunds/12/attest-result",
          );
          return ok({
            challenge_id: "synthetic-challenge",
            expires_at: new Date(Date.now() + 120000).toISOString(),
            summary: {
              kind: "refund-attestation",
              refund_id: "12",
              refund_no: "RF-12",
              refund_amount: "1.00",
              attested_state: "succeeded",
            },
          });
        }
        if (path.endsWith("/funds-authorizations/confirm")) {
          grants++;
          return ok({
            authorization: "synthetic-once-" + grants,
            expires_at: new Date(Date.now() + 120000).toISOString(),
          });
        }
        if (path.endsWith("/refunds/12/attestation")) {
          expect(req.method()).toBe("GET");
          reads++;
          if (reads === 2) return route.abort("failed");
          if (reads === 3) await pending;
          return ok(
            reads >= 3 && finalReadback
              ? {
                  ...view,
                  status: "succeeded",
                  eligible: false,
                  reason: "final",
                  states: [],
                  attestation: {
                    state: "succeeded",
                    provider_reference: "LOCAL-REFERENCE",
                    note: "local verified",
                    admin_username: "admin",
                    created_at: "2026-11-06T00:00:00Z",
                  },
                }
              : view,
          );
        }
        if (path.endsWith("/refunds/12/attest-result")) {
          expect(req.method()).toBe("POST");
          expect(req.headers()["x-payment-authorization"]).toBe(
            "synthetic-once-" + grants,
          );
          writes.push(req.postDataJSON());
          if (writes.length === 1) return route.abort("failed");
          return ok({
            ...view,
            status: "succeeded",
            eligible: false,
            reason: "final",
            states: [],
          });
        }
        if (req.method() !== "GET")
          throw Error("unexpected local mutation " + path);
        return ok({
          items: [
            {
              id: "12",
              refund_no: "RF-12",
              trade_no: "ORDER-LOCAL",
              merchant_id: "2",
              amount: "100",
              status: "unknown",
            },
          ],
          total: 1,
        });
      });
      await page.goto("/admin/trade/refund");
      await page
        .getByRole("button", { name: "核实退款结果 RF-12", exact: true })
        .click();
      const dialog = page.getByRole("dialog", {
        name: "核实退款结果",
        exact: true,
      });
      await dialog
        .getByRole("radio", { name: "退款成功", exact: true })
        .check();
      await dialog
        .getByLabel("支付机构凭证号", { exact: true })
        .fill("LOCAL-REFERENCE");
      await dialog
        .getByLabel("核实说明", { exact: true })
        .fill("local provider verification");
      const verified = dialog.getByRole("checkbox");
      await verified.check();
      await dialog
        .getByRole("button", { name: "确认并提交", exact: true })
        .click();
      await authorize(page);
      await expect(dialog.getByRole("alert")).toContainText("提交结果未知");
      expect(writes).toHaveLength(1);
      const refresh = dialog.getByRole("button", {
        name: "重新读取状态",
        exact: true,
      });
      await refresh.click();
      await expect(dialog.getByRole("alert")).toBeVisible();
      await expect(
        dialog.getByRole("button", { name: "确认并提交", exact: true }),
      ).toHaveCount(0);
      await expect(refresh).toBeEnabled();
      expect(writes).toHaveLength(1);
      await refresh.click();
      await expect(refresh).toBeDisabled();
      await expect(
        dialog.getByRole("button", { name: "确认并提交", exact: true }),
      ).toHaveCount(0);
      expect(writes).toHaveLength(1);
      release();
      if (finalReadback) {
        await expect(
          dialog.getByRole("status").filter({ hasText: "核实为“退款成功”" }),
        ).toBeVisible();
        await expect(
          dialog.getByRole("button", { name: "确认并提交", exact: true }),
        ).toHaveCount(0);
      } else {
        await expect(verified).not.toBeChecked();
        const submit = dialog.getByRole("button", {
          name: "确认并提交",
          exact: true,
        });
        await expect(submit).toBeDisabled();
        await verified.check();
        await submit.click();
        await authorize(page);
        await expect(
          dialog.getByRole("status").filter({ hasText: "已记录核实结果" }),
        ).toContainText("已记录核实结果：退款成功");
        expect(writes).toHaveLength(2);
      }
      expect(errors).toEqual([]);
      expect(
        await page.evaluate(
          () => document.documentElement.scrollWidth <= innerWidth,
        ),
      ).toBe(true);
      await dialog.getByRole("button", { name: "关闭", exact: true }).click();
      await expect(dialog).toHaveCount(0);
    });
