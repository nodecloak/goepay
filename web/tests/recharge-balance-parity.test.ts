// @vitest-environment jsdom
import { afterEach, expect, it, vi } from "vitest";
import { mount, flushPromises, type VueWrapper } from "@vue/test-utils";
import { ApiClient } from "../src/api";
import CommercePage from "../src/components/CommercePage.vue";

const views: VueWrapper[] = [];
afterEach(() => {
  views.splice(0).forEach((view) => view.unmount());
  vi.restoreAllMocks();
});
function fixture(
  balance: unknown = "99.00",
  kind: "recharges" | "deposits" | "memberships" = "recharges",
) {
  const client = new ApiClient("merchant");
  client.csrfToken = "synthetic-recharge-224";
  const request = vi
    .spyOn(client, "request")
    .mockImplementation(async (path, options) => {
      expect(options?.method || "GET").toBe("GET");
      if (path === "/dashboard" && balance instanceof Error) throw balance;
      return (
        path === "/dashboard"
          ? { available_money: balance }
          : { items: [], total: 0 }
      ) as never;
    });
  const view = mount(CommercePage, {
    props: { client, kind, area: "merchant" },
  });
  views.push(view);
  return { view, client, request };
}
it.each(["99.00", "0.00", "90071992547409.93", "-1.25"])(
  "shows exact current balance %s on the recharge page using only reads",
  async (amount) => {
    const { view, request } = fixture(amount);
    await flushPromises();
    const balance = view.get('[aria-label="充值账户余额"]');
    expect(balance.text()).toContain("当前余额（元）");
    expect(balance.get("dd").text()).toBe(amount);
    expect(
      request.mock.calls.filter(([path]) => path === "/dashboard"),
    ).toHaveLength(1);
    expect(view.find("table").exists()).toBe(true);
  },
);
it.each([undefined, 99, "1e2", "99", "92233720368547758.08"])(
  "does not invent a zero balance for an incomplete response (%s)",
  async (value) => {
    const { view } = fixture(null);
    await view.setProps({
      client: (() => {
        const next = new ApiClient("merchant");
        vi.spyOn(next, "request").mockResolvedValue({
          available_money: value,
        } as never);
        return next;
      })(),
    });
    await flushPromises();
    const balance = view.get('[aria-label="充值账户余额"]');
    expect(balance.get("dd").text()).toBe("暂不可用");
    expect(balance.get('[role="alert"]').text()).toContain("余额");
  },
);
it("shows a recoverable read failure while preserving the recharge records", async () => {
  const { view } = fixture(Error("synthetic read failure"));
  await flushPromises();
  const balance = view.get('[aria-label="充值账户余额"]');
  expect(balance.get("dd").text()).toBe("暂不可用");
  expect(balance.get('[role="alert"]').text()).toContain("余额读取失败");
  expect(view.find("table").exists()).toBe(true);
});
it("discards a late balance response after the merchant client changes", async () => {
  const first = new ApiClient("merchant"),
    second = new ApiClient("merchant");
  let resolveFirst!: (data: unknown) => void;
  vi.spyOn(first, "request").mockImplementation(
    (path) =>
      (path === "/dashboard"
        ? new Promise((resolve) => {
            resolveFirst = resolve;
          })
        : Promise.resolve({ items: [], total: 0 })) as never,
  );
  vi.spyOn(second, "request").mockImplementation(
    async (path) =>
      (path === "/dashboard"
        ? { available_money: "22.34" }
        : { items: [], total: 0 }) as never,
  );
  const view = mount(CommercePage, {
    props: { client: first, kind: "recharges", area: "merchant" },
  });
  views.push(view);
  await flushPromises();
  await view.setProps({ client: second });
  await flushPromises();
  expect(view.get('[aria-label="充值账户余额"] dd').text()).toBe("22.34");
  resolveFirst({ available_money: "999.99" });
  await flushPromises();
  expect(view.get('[aria-label="充值账户余额"] dd').text()).toBe("22.34");
});
it("stops the balance loader if the active client no longer belongs to the merchant area", async () => {
  const first = new ApiClient("merchant"),
    second = new ApiClient("admin");
  vi.spyOn(first, "request").mockImplementation(
    (path) =>
      (path === "/dashboard"
        ? new Promise(() => {})
        : Promise.resolve({ items: [], total: 0 })) as never,
  );
  const request = vi.spyOn(second, "request");
  const view = mount(CommercePage, {
    props: { client: first, kind: "recharges", area: "merchant" },
  });
  views.push(view);
  await flushPromises();
  await view.setProps({ client: second });
  await flushPromises();
  expect(view.get('[aria-label="充值账户余额"] dd').text()).toBe("暂不可用");
  expect(request).not.toHaveBeenCalled();
});
it.each(["deposits", "memberships"] as const)(
  "does not read a recharge balance on %s",
  async (kind) => {
    const { view, request } = fixture("99.00", kind);
    await flushPromises();
    expect(view.find('[aria-label="充值账户余额"]').exists()).toBe(false);
    expect(request.mock.calls.some(([path]) => path === "/dashboard")).toBe(
      false,
    );
  },
);
it("clears the old displayed balance while an explicit refresh is pending", async () => {
  const { view, request } = fixture();
  await flushPromises();
  let resolve!: (value: unknown) => void;
  request.mockImplementation(
    (path) =>
      (path === "/dashboard"
        ? new Promise((done) => {
            resolve = done;
          })
        : Promise.resolve({ items: [], total: 0 })) as never,
  );
  const card = view.get('[aria-label="充值账户余额"]');
  await card.get("button").trigger("click");
  expect(card.get("dd").text()).toBe("读取中…");
  expect(card.text()).not.toContain("99.00");
  resolve({ available_money: "22.34" });
  await flushPromises();
  expect(card.get("dd").text()).toBe("22.34");
});
