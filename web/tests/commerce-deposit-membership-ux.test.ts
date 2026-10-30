import { it, expect, vi, beforeEach } from "vitest";
import { mount, flushPromises } from "@vue/test-utils";
import { ApiClient } from "../src/api";
import CommercePage from "../src/components/CommercePage.vue";

beforeEach(() => {
  // Stable external_no so opening the dialog is deterministic.
  vi.stubGlobal("crypto", {
    ...globalThis.crypto,
    randomUUID: () => "11111111-1111-1111-1111-111111111111",
  });
});

function depositClient(overrides: Record<string, unknown> = {}) {
  const client = new ApiClient("merchant");
  vi.spyOn(client, "request").mockImplementation(
    (path) =>
      Promise.resolve(
        path.startsWith("/workflows/deposits")
          ? {
              items: [],
              total: 0,
              deposit_money: "50.00",
              pending_release_money: "0.00",
              deposit_min_money: "100.00",
              deposit_shortfall_money: "50.00",
              deposit_day: 7,
              ...overrides,
            }
          : { items: [], total: 0 },
      ) as never,
  );
  return client;
}

it("B15 #24: deposit page shows the minimum-required warning and the withdraw day notice", async () => {
  const w = mount(CommercePage, {
    props: { client: depositClient(), kind: "deposits", area: "merchant" },
  });
  await flushPromises();
  const text = w.text();
  expect(w.find('section[aria-label="保证金管理"]').exists()).toBe(true);
  expect(w.find("h2").exists()).toBe(false);
  expect(text).toContain("当前平台要求最低缴纳保证金");
  expect(text).toContain("100.00");
  expect(text).toContain("提取保证金要求最近");
  expect(text).toContain("7");
  w.unmount();
});

it("B15 #24: no warning/notice when deposit_min and deposit_day are zero", async () => {
  const w = mount(CommercePage, {
    props: {
      client: depositClient({
        deposit_min_money: "0.00",
        deposit_shortfall_money: "0.00",
        deposit_day: 0,
      }),
      kind: "deposits",
      area: "merchant",
    },
  });
  await flushPromises();
  const text = w.text();
  expect(text).not.toContain("当前平台要求最低缴纳保证金");
  expect(text).not.toContain("提取保证金要求最近");
  w.unmount();
});

it("B15 #24: opening the deposit dialog pre-fills the shortfall into the amount", async () => {
  const w = mount(CommercePage, {
    props: { client: depositClient(), kind: "deposits", area: "merchant" },
    attachTo: document.body,
  });
  await flushPromises();
  await w.find("button.primary").trigger("click");
  await flushPromises();
  // KitModal teleports to body; query the amount input there (avoid w.html()).
  const amount = document.body.querySelector<HTMLInputElement>(
    'input[aria-label="金额（元）"]',
  );
  expect(amount?.value).toBe("50.00");
  w.unmount();
  document.body.innerHTML = "";
});

function membershipClient(current: Record<string, unknown> | null) {
  const client = new ApiClient("merchant");
  vi.spyOn(client, "request").mockImplementation(
    (path) =>
      Promise.resolve(
        path.startsWith("/membership-sales")
          ? {
              items: [
                {
                  id: "5",
                  name: "黄金会员",
                  price_cents: "19900",
                  duration_months: 1,
                  description: "尊享",
                },
              ],
              enabled: true,
              current,
            }
          : { items: [], total: 0 },
      ) as never,
  );
  return client;
}

it("B15 #20: membership header shows current level, expiry and a 续期 button", async () => {
  const w = mount(CommercePage, {
    props: {
      client: membershipClient({
        my_group: "黄金会员",
        my_gid: "5",
        endtime: "2026-11-09 12:00:00",
        can_renew: true,
      }),
      kind: "memberships",
      area: "merchant",
    },
  });
  await flushPromises();
  const text = w.text();
  expect(text).toContain("当前会员等级");
  expect(text).toContain("黄金会员");
  expect(text).toContain("到期时间");
  expect(text).toContain("2026-11-09 12:00:00");
  expect(w.findAll("button").some((b) => b.text() === "续期")).toBe(true);
  w.unmount();
});

it("B15 #20: no current-level header for a merchant with no active membership", async () => {
  const w = mount(CommercePage, {
    props: {
      client: membershipClient({
        my_group: "",
        my_gid: "",
        endtime: "",
        can_renew: false,
      }),
      kind: "memberships",
      area: "merchant",
    },
  });
  await flushPromises();
  expect(w.text()).not.toContain("当前会员等级");
  w.unmount();
});

it("B15 #20: 续期 opens the buy dialog preselected to the current group", async () => {
  const w = mount(CommercePage, {
    props: {
      client: membershipClient({
        my_group: "黄金会员",
        my_gid: "5",
        endtime: "2026-11-09 12:00:00",
        can_renew: true,
      }),
      kind: "memberships",
      area: "merchant",
    },
    attachTo: document.body,
  });
  await flushPromises();
  const renew = w.findAll("button").find((b) => b.text() === "续期");
  await renew!.trigger("click");
  await flushPromises();
  // KitSelect is a custom listbox whose trigger button carries the aria-label
  // and renders the selected option's label text.
  const trigger = document.body.querySelector<HTMLButtonElement>(
    'button[aria-label="购买用户组"]',
  );
  expect(trigger?.textContent).toContain("黄金会员");
  w.unmount();
  document.body.innerHTML = "";
});
