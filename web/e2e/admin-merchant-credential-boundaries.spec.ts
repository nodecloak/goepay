import { pageReload248 } from "./fixtures/page-reload248";
import { test, expect, type Page } from "@playwright/test";
const firstID = "9007199254740993",
  secondID = "9007199254740995",
  secret = "16216216216216216216216216216216";
const secondSecret = "27272727272727272727272727272727";
async function fixture(
  page: Page,
  options: {
    adminClass?: string;
    canUpdate?: boolean;
    deferRead?: boolean;
    deferWrite?: boolean;
    loseWriteResponse?: "abort" | "malformed";
  } = {},
) {
  const writes: { path: string; body: Record<string, unknown> }[] = [],
    reads: string[] = [];
  const records = [
    { id: firstID, name: "本地商户A", signature_algorithm: "MD5" },
    { id: secondID, name: "本地商户B", signature_algorithm: "MD5" },
  ];
  const states = new Map(
    records.map((r, i) => [
      r.id,
      {
        merchant_id: r.id,
        revision: "1",
        algorithm: "MD5",
        public_key: "",
        md5_configured: true,
        fingerprint: (i ? "e" : "a").repeat(64),
      },
    ]),
  );
  let releaseRead = () => {},
    releaseWrite = () => {},
    deferredReadCompleted = false,
    readDeferred = false;
  const readWait = new Promise<void>((resolve) => (releaseRead = resolve)),
    writeWait = new Promise<void>((resolve) => (releaseWrite = resolve));
  await page.route("**/console/v1/**", async (route) => {
    const req = route.request(),
      p = new URL(req.url()).pathname;
    let data: unknown = { items: [], total: 0 },
      status = 200;
    if (req.method() === "GET") reads.push(p);
    else writes.push({ path: p, body: req.postDataJSON() });
    if (p.endsWith("/session"))
      data = {
        user: {
          id: "1",
          role: "admin",
          admin_class: options.adminClass || "super",
        },
        csrf_token: "boundary162-local",
      };
    else if (p.endsWith("/security/payment-password"))
      data = {
        configured: false,
        enforced: false,
        credential_epoch: 0,
        totp_required: false,
      };
    else if (p.endsWith("/merchants/schema"))
      data = {
        title: "商户管理",
        permissions: {
          create: true,
          update: options.canUpdate !== false,
          delete: false,
        },
        fields: [
          { name: "name", label: "名称", type: "string", required: true },
          {
            name: "api_key",
            label: "API 密钥",
            type: "password",
            write_only: true,
          },
          {
            name: "rsa_public_key",
            label: "RSA 公钥",
            type: "textarea",
            write_only: true,
          },
          {
            name: "signature_algorithm",
            label: "签名算法",
            type: "select",
            options: [
              { value: "MD5", label: "MD5" },
              { value: "RSA", label: "RSA" },
            ],
          },
        ],
      };
    else if (p.includes("/credentials")) {
      const match = p.match(
        /\/merchants\/(\d+)\/credentials(?:\/(reset-md5))?$/,
      );
      expect(match).toBeTruthy();
      const id = match![1]!,
        state = states.get(id)!;
      if (req.method() === "GET") {
        data = { ...state };
        if (options.deferRead && id === firstID && !readDeferred) {
          readDeferred = true;
          await readWait;
          deferredReadCompleted = true;
        }
      } else {
        if (options.deferWrite) await writeWait;
        if (req.postDataJSON().expected_fingerprint !== state.fingerprint) {
          status = 409;
          data = null;
        } else {
          state.revision = String(BigInt(state.revision) + 1n);
          state.fingerprint = (
            state.fingerprint[0] === "a"
              ? "b"
              : state.fingerprint[0] === "b"
                ? "c"
                : "f"
          ).repeat(64);
          data = {
            merchant_id: id,
            revision: state.revision,
            algorithm: state.algorithm,
            fingerprint: state.fingerprint,
            api_key: state.revision === "2" ? secret : secondSecret,
          };
        }
      }
    } else if (p.match(/\/resources\/merchants\/\d+$/)) {
      const record = records.find((r) => p.endsWith("/" + r.id));
      if (
        record &&
        req.method() === "PATCH" &&
        typeof req.postDataJSON().name === "string"
      )
        record.name = req.postDataJSON().name;
      data = record;
    } else if (p.endsWith("/resources/merchants"))
      data = { items: records, total: records.length };
    const output = {
      status,
      json: {
        code: status === 200 ? 0 : -1,
        message: status === 409 ? "credentials changed" : "",
        data,
      },
    };
    if (
      options.loseWriteResponse &&
      status === 200 &&
      req.method() === "POST" &&
      p.includes("/credentials/")
    ) {
      if (options.loseWriteResponse === "abort") await route.abort("failed");
      else
        await route.fulfill({
          status: 200,
          contentType: "application/json",
          body: '{"code":0,"data":',
        });
      return;
    }
    if (options.deferRead && p.endsWith(`/merchants/${firstID}/credentials`))
      await route.fulfill(output).catch(() => {});
    else await route.fulfill(output);
  });
  await page.goto("/admin/merchant/merchant-list");
  async function open(id: string) {
    const row = page
      .getByRole("row")
      .filter({ has: page.getByRole("cell", { name: id, exact: true }) });
    await row.getByRole("button", { name: "编辑", exact: true }).click();
    const dialog = page.getByRole("dialog", {
      name: "编辑商户管理",
      exact: true,
    });
    await dialog.getByRole("tab", { name: "密钥信息", exact: true }).click();
    return {
      dialog,
      panel: dialog.getByRole("region", { name: "商户密钥操作", exact: true }),
    };
  }
  return {
    writes,
    reads,
    open,
    releaseRead,
    releaseWrite,
    readCompleted: () => deferredReadCompleted,
    advanceRemote: () => {
      const v = states.get(firstID)!;
      v.revision = "2";
      v.fingerprint = "b".repeat(64);
    },
  };
}
test("operator and read-only merchant resources never load administrator credential controls", async ({
  page,
}) => {
  const f = await fixture(page, { adminClass: "operator" });
  const { dialog } = await f.open(firstID);
  await expect(
    dialog.getByRole("region", { name: "商户密钥操作", exact: true }),
  ).toHaveCount(0);
  expect(f.reads.filter((p) => p.includes("/credentials"))).toHaveLength(0);
  expect(f.writes).toHaveLength(0);
});
test("readonly schema has no editor or credential generation path", async ({
  page,
}) => {
  const f = await fixture(page, { canUpdate: false });
  await expect(
    page.getByRole("button", { name: "编辑", exact: true }),
  ).toHaveCount(0);
  await expect(
    page.getByRole("button", { name: "重置MD5密钥", exact: true }),
  ).toHaveCount(0);
  expect(f.reads.filter((p) => p.includes("/credentials"))).toHaveLength(0);
  expect(f.writes).toHaveLength(0);
});
test("late credential read cannot replace the next merchant or its confirmation fingerprint", async ({
  page,
}) => {
  const f = await fixture(page, { deferRead: true });
  let { dialog } = await f.open(firstID);
  await expect
    .poll(() =>
      f.reads.some((p) => p.endsWith(`/merchants/${firstID}/credentials`)),
    )
    .toBe(true);
  await dialog.getByRole("button", { name: "取消", exact: true }).click();
  const next = await f.open(secondID);
  dialog = next.dialog;
  const panel = next.panel,
    reset = panel.getByRole("button", { name: "重置MD5密钥", exact: true });
  await expect(reset).toBeEnabled();
  f.releaseRead();
  await expect.poll(f.readCompleted).toBe(true);
  await expect(panel).toContainText(secondID);
  await expect(panel).not.toContainText(firstID);
  await reset.click();
  await panel
    .getByRole("group", { name: "确认重置MD5密钥", exact: true })
    .getByRole("button", { name: "确定重置", exact: true })
    .click();
  await expect(
    panel.getByLabel("新MD5密钥（仅本次显示）", { exact: true }),
  ).toHaveValue(secret);
  expect(f.writes).toEqual([
    {
      path: `/console/v1/admin/merchants/${secondID}/credentials/reset-md5`,
      body: { expected_fingerprint: "e".repeat(64) },
    },
  ]);
});
test("pending credential write blocks duplicate submit, editor close and background tab focus", async ({
  page,
}) => {
  const f = await fixture(page, { deferWrite: true }),
    { dialog, panel } = await f.open(firstID),
    reset = panel.getByRole("button", { name: "重置MD5密钥", exact: true });
  await expect(reset).toBeEnabled();
  await reset.click();
  const confirm = panel.getByRole("group", {
    name: "确认重置MD5密钥",
    exact: true,
  });
  await confirm
    .getByRole("button", { name: "确定重置", exact: true })
    .dblclick();
  await expect.poll(() => f.writes.length).toBe(1);
  await expect(
    dialog.getByRole("button", { name: "保存", exact: true }),
  ).toHaveCount(0);
  await expect(
    dialog.getByRole("button", { name: "正在处理…", exact: true }),
  ).toBeDisabled();
  await expect(
    dialog.getByRole("button", { name: "取消", exact: true }).last(),
  ).toBeDisabled();
  await expect(
    dialog.getByRole("tab", { name: "基本信息", exact: true }),
  ).toBeDisabled();
  await page.keyboard.press("Escape");
  await expect(dialog).toBeVisible();
  await page.keyboard.press("Tab");
  expect(
    await dialog.evaluate((el) => el.contains(document.activeElement)),
  ).toBe(true);
  expect(f.writes).toHaveLength(1);
  f.releaseWrite();
  await expect(
    panel.getByLabel("新MD5密钥（仅本次显示）", { exact: true }),
  ).toHaveValue(secret);
  expect(f.writes).toHaveLength(1);
});
test("conflicted credential change requires an explicit refresh and new confirmation", async ({
  page,
}) => {
  const f = await fixture(page),
    { panel } = await f.open(firstID),
    reset = panel.getByRole("button", { name: "重置MD5密钥", exact: true });
  await expect(reset).toBeEnabled();
  f.advanceRemote();
  await reset.click();
  await panel
    .getByRole("group", { name: "确认重置MD5密钥", exact: true })
    .getByRole("button", { name: "确定重置", exact: true })
    .click();
  await expect(panel.getByRole("alert")).toContainText("密钥状态已变更");
  await expect(panel.getByRole("alert")).toBeFocused();
  await expect(reset).toBeDisabled();
  expect(f.writes).toHaveLength(1);
  await expect(
    panel.getByLabel("新MD5密钥（仅本次显示）", { exact: true }),
  ).toHaveCount(0);
  await pageReload248(page);
  await f.open(firstID);
  await expect(reset).toBeEnabled();
  await reset.click();
  await panel
    .getByRole("group", { name: "确认重置MD5密钥", exact: true })
    .getByRole("button", { name: "确定重置", exact: true })
    .click();
  await expect(
    panel.getByLabel("新MD5密钥（仅本次显示）", { exact: true }),
  ).toHaveValue(secondSecret);
  expect(f.writes).toHaveLength(2);
  expect(f.writes[1]!.body).toEqual({ expected_fingerprint: "b".repeat(64) });
});

test("late clipboard completion cannot mark a newer one-time key as copied", async ({
  page,
}) => {
  const f = await fixture(page),
    { panel } = await f.open(firstID),
    reset = panel.getByRole("button", { name: "重置MD5密钥", exact: true });
  await expect(reset).toBeEnabled();
  await page.evaluate(() => {
    let finish = () => {};
    const pending = new Promise<void>((resolve) => (finish = resolve));
    Object.defineProperty(navigator, "clipboard", {
      configurable: true,
      value: { writeText: () => pending },
    });
    (
      window as unknown as { finishCredentialCopy162: () => void }
    ).finishCredentialCopy162 = finish;
  });
  async function rotate() {
    await reset.click();
    await panel
      .getByRole("group", { name: "确认重置MD5密钥", exact: true })
      .getByRole("button", { name: "确定重置", exact: true })
      .click();
  }
  await rotate();
  await expect(
    panel.getByLabel("新MD5密钥（仅本次显示）", { exact: true }),
  ).toHaveValue(secret);
  await panel.getByRole("button", { name: "复制新密钥", exact: true }).click();
  await panel
    .getByRole("button", { name: "关闭密钥显示", exact: true })
    .click();
  await rotate();
  await expect(
    panel.getByLabel("新MD5密钥（仅本次显示）", { exact: true }),
  ).toHaveValue(secondSecret);
  await page.evaluate(async () => {
    (
      window as unknown as { finishCredentialCopy162: () => void }
    ).finishCredentialCopy162();
    await Promise.resolve();
    await Promise.resolve();
  });
  await expect(
    panel.getByText("已复制，请妥善保存", { exact: true }),
  ).toHaveCount(0);
  await expect(
    panel.getByLabel("新MD5密钥（仅本次显示）", { exact: true }),
  ).toHaveValue(secondSecret);
  expect(f.writes).toHaveLength(2);
});

for (const failure of ["abort", "malformed"] as const)
  test(
    "committed credential change with " +
      failure +
      " response discards stale key drafts before a profile save",
    async ({ page }) => {
      const f = await fixture(page, { loseWriteResponse: failure });
      const { dialog, panel } = await f.open(firstID);
      const reset = panel.getByRole("button", {
        name: "重置MD5密钥",
        exact: true,
      });
      await expect(reset).toBeEnabled();
      const md5 = dialog.getByLabel("API 密钥", { exact: true });
      const rsa = dialog.getByLabel("RSA 公钥", { exact: true });
      await md5.fill("old-unsaved-manual-md5-secret162");
      await rsa.fill("old-unsaved-rsa-draft162");
      await reset.click();
      const confirmation = panel.getByRole("group", {
        name: "确认重置MD5密钥",
        exact: true,
      });
      await confirmation
        .getByRole("button", { name: "取消", exact: true })
        .click();
      await expect(md5).toHaveValue("old-unsaved-manual-md5-secret162");
      await expect(rsa).toHaveValue("old-unsaved-rsa-draft162");
      expect(f.writes).toHaveLength(0);
      await reset.click();
      await confirmation
        .getByRole("button", { name: "确定重置", exact: true })
        .click();
      await expect(panel.getByRole("alert")).toContainText(
        "本次密钥变更未能确认",
      );
      await expect(md5).toHaveValue("");
      await expect(rsa).toHaveValue("");
      await dialog.getByRole("tab", { name: "基本信息", exact: true }).click();
      await dialog
        .getByRole("textbox", { name: "名称", exact: false })
        .fill("不覆盖密钥的资料更新");
      await dialog.getByRole("button", { name: "保存", exact: true }).click();
      await expect(page.getByText("修改已保存", { exact: true })).toBeVisible();
      expect(f.writes).toHaveLength(2);
      const saved = f.writes.find((w) =>
        w.path.endsWith("/resources/merchants/" + firstID),
      )!.body;
      expect(saved).toHaveProperty("name", "不覆盖密钥的资料更新");
      expect(saved).not.toHaveProperty("api_key");
      expect(saved).not.toHaveProperty("rsa_public_key");
    },
  );
