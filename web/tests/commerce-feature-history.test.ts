// @vitest-environment jsdom
import { it, expect, vi } from "vitest";
import { mount, flushPromises } from "@vue/test-utils";
import CommercePage from "../src/components/CommercePage.vue";
import { ApiClient } from "../src/api";
import { flattenMenu, navigationFor, pageAuthorizedFor } from "../src/menu";

for (const [kind, path, flag, menu] of [
  ["deposits", "/account/deposit", "user_deposit", "Deposit"],
  ["recharges", "/account/recharge", "recharge", "Recharge"],
] as const) {
  it(`keeps ${kind} history reachable when its new-use menu is closed`, () => {
    const features = { [flag]: false };
    expect(
      flattenMenu(
        navigationFor("merchant", "merchant", undefined, undefined, features),
      ).some((n) => n.id === menu),
    ).toBe(false);
    expect(
      pageAuthorizedFor(
        "merchant",
        path,
        "merchant",
        undefined,
        undefined,
        features,
      ),
    ).toBe(true);
  });
  it(`reads ${kind} history without enabling a new money action`, async () => {
    const client = new ApiClient("merchant");
    const request = vi.spyOn(client, "request").mockResolvedValue({
      items: [{ id: "12288", amount: "1.23", status: "held" }],
      total: 1,
    });
    const w = mount(CommercePage, {
      props: { client, kind, area: "merchant", newUseAllowed: false },
    });
    await flushPromises();
    expect(w.text()).toContain("12288");
    expect(w.text()).toContain("仍可查看原有记录");
    const create = w
      .findAll("button")
      .find(
        (b) => b.text() === (kind === "deposits" ? "保证金操作" : "余额充值"),
      )!;
    expect(create.attributes("disabled")).toBeDefined();
    await create.trigger("click");
    expect(w.find('[role="dialog"]').exists()).toBe(false);
    expect(request.mock.calls.every(([, options]) => !options?.method)).toBe(
      true,
    );
    w.unmount();
  });
}
it("disables new deposit drafts when the effective policy cannot be read while keeping real balances", async () => {
  const client = new ApiClient("merchant");
  vi.spyOn(client, "request").mockResolvedValue({
    items: [],
    total: 0,
    deposit_money: "12.34",
    pending_release_money: "1.23",
    deposit_policy_available: false,
    deposit_enabled: false,
  });
  const w = mount(CommercePage, {
    props: { client, kind: "deposits", area: "merchant" },
  });
  await flushPromises();
  expect(w.text()).toContain("12.34");
  expect(w.text()).toContain("保证金规则暂不可用");
  expect(
    w
      .findAll("button")
      .find((b) => b.text() === "保证金操作")!
      .attributes("disabled"),
  ).toBeDefined();
  w.unmount();
});
