import { expect, test } from "@playwright/test";
import { inheritedRiskGroup } from "./fixtures/core-risk187";

test("USDT assignment follows saved user groups, including merchant switch and disabled access", async ({
  page,
  context,
  baseURL,
}) => {
  if (
    !baseURL ||
    !["127.0.0.1", "localhost"].includes(new URL(baseURL).hostname)
  )
    throw Error("Local fixture required");
  await context.route("**/*", (route) =>
    ["127.0.0.1", "localhost"].includes(new URL(route.request().url()).hostname)
      ? route.fallback()
      : route.abort(),
  );
  const channels = [
    {
      id: "42",
      name: "TRON synthetic",
      network: "tron",
      fee_bps: 75,
      min_amount: "0",
      max_amount: "0",
      day_limit: "0",
    },
    {
      id: "43",
      name: "Ethereum synthetic",
      network: "ethereum",
      fee_bps: 80,
      min_amount: "0",
      max_amount: "0",
      day_limit: "0",
    },
  ];
  const groups = [
    {
      id: "0",
      name: "默认未分配",
      enabled: true,
      info: {} as Record<string, any>,
    },
    {
      id: "1",
      name: "TRON用户组",
      enabled: true,
      info: { usdt: { channel: "0", type: "", rate: "99.25" } },
    },
    {
      id: "2",
      name: "Ethereum用户组",
      enabled: true,
      info: { usdt: { channel: "43", type: "channel", rate: "99.20" } },
    },
    {
      id: "3",
      name: "USDT关闭组",
      enabled: true,
      info: { usdt: { channel: "0", type: "" } },
    },
  ];
  let merchant = {
    id: "1000",
    name: "Synthetic merchant",
    group_id: "0",
    enabled: true,
  };
  const writes: { path: string; body: any }[] = [],
    errors: string[] = [];
  page.on("pageerror", (e) => errors.push(e.message));
  await page.route("**/console/v1/**", async (route) => {
    const req = route.request(),
      url = new URL(req.url());
    const area = url.pathname.includes("/admin/") ? "admin" : "merchant";
    const path = url.pathname.replace(`/console/v1/${area}`, "");
    const ok = (data: unknown) => route.fulfill({ json: { code: 0, data } });
    if (req.method() !== "GET") {
      const body = req.postDataJSON();
      writes.push({ path, body });
      expect(body).not.toHaveProperty("merchant_ids");
      if (path === "/resources/groups/1") {
        groups[1] = { ...groups[1]!, ...body };
        return ok(groups[1]);
      }
      if (path === "/resources/merchants/1000") {
        expect(body).not.toHaveProperty("channel_ids");
        expect(body.confirm_group_assignment).toBe(true);
        merchant = { ...merchant, ...body };
        return ok(merchant);
      }
      throw Error("Unexpected mocked write: " + path);
    }
    if (path === "/session")
      return ok({
        user: {
          id: "1000",
          name: "Synthetic user",
          role: area,
          ...(area === "admin" ? { admin_class: "super" } : {}),
        },
        csrf_token: "local-only-csrf",
      });
    if (path === "/security/payment-password")
      return ok({
        configured: false,
        enforced: false,
        credential_epoch: 0,
        totp_required: false,
      });
    if (path === "/resources/groups/schema")
      return ok({
        title: "用户组设置",
        fields: [
          { name: "name", label: "名称", type: "string", required: true },
          { name: "info", label: "通道费率", type: "json" },
        ],
        permissions: { create: true, update: true, delete: false },
      });
    if (path === "/resources/groups/routing-options")
      return ok({
        available: true,
        items: channels.map((channel) => ({
          id: channel.id,
          type: "usdt",
          methods: ["native"],
        })),
      });
    if (path === "/resources/groups")
      return ok({ items: groups, total: groups.length });
    if (path.startsWith("/resources/groups/"))
      return ok(groups.find((group) => group.id === path.split("/").at(-1)));
    if (path.startsWith("/payment-risk/groups/")) {
      const id = path.split("/").at(-1)!;
      return ok(
        inheritedRiskGroup(
          id,
          groups.find((group) => group.id === id)?.name || "default",
        ),
      );
    }
    if (path === "/resources/pay-rolls") return ok({ items: [], total: 0 });
    if (path === "/resources/merchants/schema")
      return ok({
        title: "商户管理",
        fields: [
          { name: "name", label: "名称", type: "string", required: true },
          { name: "group_id", label: "用户组", type: "string" },
          {
            name: "confirm_group_assignment",
            label: "确认变更用户组",
            type: "boolean",
            write_only: true,
          },
        ],
        permissions: { create: false, update: true, delete: false },
      });
    if (path === "/resources/merchants")
      return ok({ items: [merchant], total: 1 });
    if (path === "/resources/merchants/1000") return ok(merchant);
    if (path === "/crypto/config")
      return ok({
        configured: true,
        enabled: true,
        simulation: true,
        networks: [
          { network: "tron", enabled: true, fee_bps: 75 },
          { network: "ethereum", enabled: true, fee_bps: 80 },
        ],
      });
    if (path === "/crypto/channels") {
      const group = groups.find((group) => group.id === merchant.group_id)!;
      const policy = group.info.usdt;
      return ok({
        items:
          policy?.type === "channel"
            ? channels.filter((channel) => channel.id === policy.channel)
            : [],
      });
    }
    if (path.startsWith("/crypto/")) return ok([]);
    return ok({ enabled: false });
  });
  const inspectMerchantOptions = async (expected: string[]) => {
    await page.goto("/merchant/trade/order?type=usdt");
    await page.locator("[data-open-form=invoice]").click();
    const form = page.locator("[data-form=invoice]");
    const select = form.locator("select[name=channel_id]");
    await expect
      .poll(() =>
        select
          .locator("option")
          .evaluateAll((options) =>
            options.map((option) => (option as HTMLOptionElement).value),
          ),
      )
      .toEqual(expected);
    if (!expected.length) {
      await expect(form).toContainText("当前没有可用的 USDT 通道");
      await expect(
        form.getByRole("button", { name: "创建收款单", exact: true }),
      ).toBeDisabled();
    } else await expect(select).toHaveValue(expected[0]!);
  };
  // Merely having enabled channels does not assign the unconfigured default group.
  await inspectMerchantOptions([]);
  await page.goto("/admin/merchant/group-list");
  await page
    .locator("tbody tr")
    .filter({ hasText: "TRON用户组" })
    .getByRole("button", { name: "编辑", exact: true })
    .click();
  const groupDialog = page.getByRole("dialog", {
    name: "编辑用户组 GID:1",
    exact: true,
  });
  const groupChannel = groupDialog.getByRole("combobox", {
    name: "USDT通道",
    exact: true,
  });
  await expect(groupChannel).toHaveValue("disabled");
  await expect(groupChannel.locator('option[value="default"]')).toHaveText(
    "继承默认用户组",
  );
  await groupChannel.selectOption("channel:42");
  expect(writes).toHaveLength(0);
  await groupDialog.getByRole("button", { name: "保存", exact: true }).click();
  await expect(groupDialog).toHaveCount(0);
  expect(writes[0]!.body.info.usdt).toEqual({
    channel: "42",
    type: "channel",
    rate: "99.25",
  });
  for (const [groupID, expectedChannel] of [
    ["1", "42"],
    ["2", "43"],
    ["3", ""],
  ]) {
    await page.goto("/admin/merchant/merchant-list");
    await page
      .locator("tbody tr")
      .getByRole("button", { name: "编辑", exact: true })
      .click();
    const dialog = page.getByRole("dialog");
    const picker = dialog.locator(".merchant-group-picker select");
    await picker.selectOption(groupID!);
    await dialog.locator("#field-confirm_group_assignment").check();
    await dialog.getByRole("button", { name: "保存", exact: true }).click();
    await expect(dialog).toHaveCount(0);
    expect(merchant.group_id).toBe(groupID);
    await inspectMerchantOptions(expectedChannel ? [expectedChannel] : []);
  }
  expect(writes.map((write) => write.path)).toEqual([
    "/resources/groups/1",
    ...Array(3).fill("/resources/merchants/1000"),
  ]);
  expect(errors).toEqual([]);
});
