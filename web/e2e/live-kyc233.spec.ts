import { test, expect, type Page } from "@playwright/test";
import { localCrudFixture } from "./local-crud-fixture";

test.use({ trace: "off" });
test("local real API persists, masks, edits and clears certification/OCR configuration without identity calls", async ({
  page,
  playwright,
}) => {
  test.skip(
    process.env.GOEPAY_LIVE_E2E !== "1" ||
      process.env.GOEPAY_LIVE_NODE_WORKFLOWS_E2E !== "1",
    "Owned loopback simulation only",
  );
  const local = await localCrudFixture(playwright, import.meta.url);
  const choose = async (label: string, option: string) => {
    await page.getByRole("button", { name: label, exact: true }).click();
    await page.getByRole("option", { name: option, exact: true }).click();
  };
  const edit = async (label: string, value: string) => {
    await choose(label + "操作", "填写或替换");
    await page.getByLabel(label, { exact: true }).fill(value);
  };
  const save = async (kind: string) => {
    await page
      .getByRole("button", { name: `保存${kind}配置`, exact: true })
      .click();
    await page
      .getByRole("dialog")
      .getByRole("button", { name: "确认保存", exact: true })
      .click();
    await expect(page.getByText("配置已保存。", { exact: true })).toBeVisible();
  };
  try {
    await local.attach(page, "admin");
    await page.goto("/admin/system/settings?tab=certificate");
    await expect(
      page.getByRole("region", { name: "实名认证配置", exact: true }),
    ).toBeVisible();
    await choose("是否开启实名认证", "手机号三要素实名认证");
    await edit("手机号三要素AppCode", "synthetic-kyc233-no-provider-call");
    await save("实名认证");
    let data = await local.call("admin", "/settings/certification");
    expect(data).toMatchObject({
      revision: "1",
      cert_open: 2,
      cert_force: false,
      cert_money: "0.00",
    });
    expect(data.credentials.cert_appcode.present).toBe(true);
    expect(JSON.stringify(data)).not.toContain(
      "synthetic-kyc233-no-provider-call",
    );
    await page.reload();
    await expect(
      page.getByLabel("手机号三要素AppCode", { exact: true }),
    ).toHaveValue("");

    await page.getByRole("tab", { name: "OCR配置", exact: true }).click();
    await choose("文字识别接口", "阿里云OCR");
    await edit("OCR阿里云AccessKey ID", "synthetic-ocr233-id");
    await edit("OCR阿里云AccessKey Secret", "synthetic-ocr233-secret");
    await save("OCR");
    data = await local.call("admin", "/settings/ocr");
    expect(data).toMatchObject({
      revision: "2",
      cert_open: 2,
      ocr_type: "aliyun",
    });
    expect(data.credentials.ocr_aliyunkey.present).toBe(true);
    expect(JSON.stringify(data)).not.toContain("synthetic-ocr233-secret");

    await page.getByRole("tab", { name: "实名认证", exact: true }).click();
    await choose("是否开启实名认证", "关闭");
    await page
      .getByRole("button", { name: "管理其它服务凭据项", exact: true })
      .click();
    await choose("手机号三要素AppCode操作", "清除原值");
    await save("实名认证");
    data = await local.call("admin", "/settings/certification");
    expect(data).toMatchObject({
      revision: "3",
      cert_open: 0,
      ocr_type: "aliyun",
    });
    expect(data.credentials.cert_appcode.present).toBe(false);

    await page.getByRole("tab", { name: "OCR配置", exact: true }).click();
    await choose("文字识别接口", "关闭");
    await page
      .getByRole("button", { name: "管理其它服务凭据项", exact: true })
      .click();
    await choose("OCR阿里云AccessKey ID操作", "清除原值");
    await choose("OCR阿里云AccessKey Secret操作", "清除原值");
    await save("OCR");
    data = await local.call("admin", "/settings/ocr");
    expect(data).toMatchObject({ revision: "4", cert_open: 0, ocr_type: "" });
    expect(data.credentials.ocr_aliyunid.present).toBe(false);
    expect(data.credentials.ocr_aliyunkey.present).toBe(false);
    const merchant: Page = await page.context().newPage();
    await local.attach(merchant, "merchant");
    await merchant.goto("/merchant/account/certificate");
    await expect(
      merchant.getByRole("heading", { name: "实名认证", exact: true }),
    ).toBeVisible();
    await expect(
      merchant.getByRole("button", { name: "提交认证资料", exact: true }),
    ).toHaveCount(0);
    expect(
      await local.call("merchant", "/account/certificate/info"),
    ).toMatchObject({ cert: 0, cert_open: 0, status: "none", ocr_type: "" });
    await merchant.close();
  } finally {
    await local.dispose();
  }
});
