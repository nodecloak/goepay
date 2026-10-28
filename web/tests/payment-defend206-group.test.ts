import { it, expect, vi } from "vitest";
import { mount, flushPromises } from "@vue/test-utils";
import { ApiClient } from "../src/api";
import PaymentDefend206GroupPanel from "../src/components/PaymentDefend206GroupPanel.vue";
import { readPaymentDefendGroup } from "../src/payment-defend206-group";

const view = (revision = "0", pay = "", effective = "3") => ({
  group_id: "5",
  group_name: "VIP",
  configured: revision !== "0",
  revision,
  pay_verify: pay,
  effective_pay_verify: effective,
  fingerprint: (revision === "0" ? "a" : "b").repeat(64),
  can_update: true,
});

function json(data: unknown, status = 200) {
  return new Response(JSON.stringify({ code: 0, message: "", data }), {
    status,
    headers: { "Content-Type": "application/json" },
  });
}

it("saves the original group pay_verify override through the exact API", async () => {
  const wire: { url: string; body: unknown }[] = [];
  const fetch = vi
    .spyOn(globalThis, "fetch")
    .mockImplementation(async (input, init) => {
      if (init?.method === "PATCH") {
        wire.push({ url: String(input), body: JSON.parse(String(init.body)) });
        return json(view("1", "0", "0"));
      }
      return json(view());
    });
  const c = new ApiClient("admin");
  c.csrfToken = "synthetic-only";
  const w = mount(PaymentDefend206GroupPanel, {
    props: { client: c, groupID: "5", allowed: true, disabled: false },
  });
  try {
    await flushPromises();
    expect(w.text()).toContain("当前生效：全部商户开启");
    const options = w.findAll("option").map((o) => o.text());
    expect(options).toEqual([
      "缺省（与系统设置一致）",
      "关闭",
      "按规则开启",
      "强制开启",
    ]);
    await w.find("select").setValue("0");
    await w
      .findAll("button")
      .find((b) => b.text() === "核对并保存人机验证配置")!
      .trigger("click");
    await w
      .findAll("button")
      .find((b) => b.text() === "确认保存人机验证配置")!
      .trigger("click");
    await flushPromises();
    expect(wire).toHaveLength(1);
    expect(wire[0]!.url).toContain("/settings/payment-defend/groups/5");
    expect(wire[0]!.body).toEqual({
      expected_fingerprint: "a".repeat(64),
      pay_verify: "0",
    });
    expect(w.text()).toContain("用户组人机验证配置已保存");
    expect(w.text()).toContain("当前生效：关闭");
  } finally {
    w.unmount();
    fetch.mockRestore();
  }
});

it("rejects inconsistent group views", () => {
  expect(() => readPaymentDefendGroup(view("0", "", "3"), "6")).toThrow();
  expect(() => readPaymentDefendGroup(view("1", "2", "2"), "5")).toThrow();
  expect(() => readPaymentDefendGroup(view("1", "0", "3"), "5")).toThrow();
  expect(() =>
    readPaymentDefendGroup({ ...view(), extra: 1 }, "5"),
  ).toThrow();
  expect(readPaymentDefendGroup(view("1", "3", "3"), "5").pay_verify).toBe(
    "3",
  );
});
