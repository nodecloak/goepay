// @vitest-environment jsdom
import { it, expect, vi } from "vitest";
import { mount, flushPromises } from "@vue/test-utils";
import { reactive } from "vue";
import MerchantCreateFields from "../src/components/MerchantCreateFields.vue";
import { ApiClient } from "../src/api";
for (const allowed of [["wxpay", "bank"], []])
  it(
    "new merchant honors actual enabled settlement types: " + allowed.join(","),
    async () => {
      const client = new ApiClient("admin");
      client.csrfToken = "synthetic-options";
      const request = vi
        .spyOn(client, "request")
        .mockResolvedValue({ settlement: allowed, can_write: true } as never);
      const values = reactive<Record<string, unknown>>({
        creation_mode: "account",
        settlement_type: "alipay",
      });
      const w = mount(MerchantCreateFields, {
        props: {
          client,
          fields: [
            {
              name: "settlement_type",
              label: "结算方式",
              type: "select",
              options: [
                { value: "alipay", label: "支付宝" },
                { value: "wxpay", label: "微信" },
                { value: "bank", label: "银行卡" },
              ],
            },
            { name: "settlement_account", label: "结算账号", type: "string" },
            { name: "settlement_name", label: "结算账号姓名", type: "string" },
          ],
          values,
          errors: {},
          disabled: false,
          onChange: (name, value) => (values[name] = value),
        },
      });
      await flushPromises();
      expect(request).toHaveBeenCalledWith(
        "/workflows/settlement-recipient/allowed-types",
        expect.anything(),
      );
      if (allowed.length) {
        expect(
          w
            .get("#field-settlement_type")
            .findAll("option")
            .map((o) => o.attributes("value"))
            .filter(Boolean),
        ).toEqual(allowed);
        expect(values.settlement_type).toBe("wxpay");
      } else {
        expect(w.find("#field-settlement_type").exists()).toBe(false);
        expect(w.text()).toContain("暂未开启结算方式");
      }
      w.unmount();
    },
  );

it("reloads the selected group's rails, keeps incompatible recipient drafts repairable and ignores old responses", async () => {
  const client = new ApiClient("admin");
  client.csrfToken = "synthetic-group-options";
  let resolveOld!: (value: unknown) => void;
  vi.spyOn(client, "request").mockImplementation(async (path) => {
    if (path.startsWith("/resources/groups"))
      return {
        items: [
          { id: "0", name: "Default" },
          { id: "3", name: "Old" },
          { id: "4", name: "Closed" },
        ],
        total: 3,
      } as never;
    if (path.endsWith("group_id=3"))
      return (await new Promise<unknown>((resolve) => {
        resolveOld = resolve;
      })) as never;
    if (path.endsWith("group_id=4"))
      return { settlement: [], can_write: true } as never;
    if (path.endsWith("group_id=0"))
      return { settlement: ["alipay"], can_write: true } as never;
    throw Error("unexpected availability request");
  });
  const values = reactive<Record<string, unknown>>({
    group_id: "0",
    creation_mode: "account",
    settlement_type: "alipay",
  });
  const w = mount(MerchantCreateFields, {
    props: {
      client,
      fields: [
        { name: "group_id", type: "string", label: "用户组" },
        {
          name: "settlement_type",
          type: "select",
          label: "结算方式",
          options: ["alipay", "bank"],
        },
        { name: "settlement_account", type: "string", label: "结算账号" },
        { name: "settlement_name", type: "string", label: "结算账号姓名" },
      ],
      values,
      errors: {},
      disabled: false,
      onChange: (name, value) => (values[name] = value),
    },
  });
  await flushPromises();
  expect(
    w
      .get("#field-settlement_type")
      .findAll("option")
      .map((o) => o.attributes("value")),
  ).toContain("alipay");
  values.settlement_account = "synthetic-recipient@example.invalid";
  values.settlement_name = "Synthetic";
  values.group_id = "3";
  await flushPromises();
  expect(w.emitted("settlement-validity")?.at(-1)?.[0]).toContain("正在读取");
  values.group_id = "4";
  await flushPromises();
  resolveOld({ settlement: ["bank"], can_write: true });
  await flushPromises();
  expect(
    w.get<HTMLInputElement>("#field-settlement_account").element.value,
  ).toBe("synthetic-recipient@example.invalid");
  expect(w.emitted("settlement-validity")?.at(-1)?.[0]).toContain("不允许");
  const clear = w
    .findAll("button")
    .find((b) => b.text() === "清空初始结算资料")!;
  expect(clear).toBeDefined();
  await clear.trigger("click");
  await flushPromises();
  expect(values.settlement_account).toBe("");
  expect(values.settlement_name).toBe("");
  expect(w.emitted("settlement-validity")?.at(-1)?.[0]).toBe("");
  w.unmount();
});
