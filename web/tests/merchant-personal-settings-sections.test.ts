import { pageReload248 } from "./helpers/page-reload248";
import { afterEach, expect, it, vi } from "vitest";
import { flushPromises, mount, type VueWrapper } from "@vue/test-utils";
import { ApiClient, ApiError } from "../src/api";
import Panel from "../src/components/MerchantPersonalSettings.vue";

const original = {
  merchant_id: "1000",
  qq: "123456",
  pay_minmoney: "1.00",
  pay_maxmoney: "2.00",
  remain_money: "10%",
  bounds_valid: true,
  fingerprint: "a".repeat(64),
  can_update: true,
};
const mounted: VueWrapper[] = [];
afterEach(() => {
  mounted.splice(0).forEach((w) => w.unmount());
  vi.restoreAllMocks();
});
async function fixture(
  section: "contact" | "settlement",
  conflict = false,
  ownSession = true,
) {
  const client = new ApiClient("merchant");
  client.csrfToken = "synthetic-sections";
  const request = vi
    .spyOn(client, "request")
    .mockImplementation(async (_path, options) => {
      if (options?.method === "PATCH") {
        if (conflict) throw new ApiError("changed", 409, -1);
        const body = options.body as Record<string, unknown>;
        return {
          ...original,
          qq: body.qq,
          pay_minmoney: body.pay_minmoney,
          pay_maxmoney: body.pay_maxmoney,
          remain_money: body.remain_money,
          fingerprint: "b".repeat(64),
        } as never;
      }
      return original as never;
    });
  let w = mount(Panel, {
    props: { client, merchantId: "1000", ownSession, section },
  });
  mounted.push(w);
  await flushPromises();
  return { w, request };
}

it("account contact settings never display or reset the existing settlement reserve", async () => {
  let { w, request } = await fixture("contact");
  expect(w.find('[name="remain_money"]').exists()).toBe(false);
  await w.get('[name="qq"]').setValue("7654321");
  await w.get("form").trigger("submit");
  await flushPromises();
  expect(request.mock.calls[1][1]?.body).toEqual({
    qq: "7654321",
    pay_minmoney: "1.00",
    pay_maxmoney: "2.00",
    remain_money: "10%",
    expected_fingerprint: original.fingerprint,
  });
  expect(w.text()).toContain("保存成功");
});

it("settlement reserve settings save only their visible draft while preserving contact and amount limits from the same CAS snapshot", async () => {
  let { w, request } = await fixture("settlement");
  expect(w.get("h2").text()).toBe("预留余额设置");
  for (const name of ["qq", "pay_minmoney", "pay_maxmoney"])
    expect(w.find(`[name="${name}"]`).exists()).toBe(false);
  await w.get('[name="remain_money"]').setValue("50");
  await w.get('[data-action="cancel"]').trigger("click");
  expect(
    (w.get('[name="remain_money"]').element as HTMLInputElement).value,
  ).toBe("10%");
  expect(request).toHaveBeenCalledTimes(1);
  await w.get('[name="remain_money"]').setValue("50");
  await w.get("form").trigger("submit");
  await flushPromises();
  expect(request.mock.calls[1][1]?.body).toEqual({
    qq: "123456",
    pay_minmoney: "1.00",
    pay_maxmoney: "2.00",
    remain_money: "50",
    expected_fingerprint: original.fingerprint,
  });
  expect(w.text()).toContain("保存成功");
});

it("a reserve conflict locks writes until explicit read instead of overwriting another section", async () => {
  let { w, request } = await fixture("settlement", true);
  await w.get('[name="remain_money"]').setValue("25%");
  await w.get("form").trigger("submit");
  await flushPromises();
  expect(w.text()).toContain("刷新页面");
  expect(w.find("form").exists()).toBe(false);
  expect(request).toHaveBeenCalledTimes(2);
  w = await pageReload248(w);
  await flushPromises();
  expect(request).toHaveBeenCalledTimes(3);
  expect(
    (w.get('[name="remain_money"]').element as HTMLInputElement).value,
  ).toBe("10%");
});

it("delegated reserve settings never read personal values", async () => {
  let { w, request } = await fixture("settlement", false, false);
  expect(request).not.toHaveBeenCalled();
  expect(w.find("form").exists()).toBe(false);
});

it("switching sections aborts and ignores an earlier section read", async () => {
  const client = new ApiClient("merchant");
  client.csrfToken = "synthetic-sections-context";
  let finish!: (value: unknown) => void;
  let count = 0;
  const request = vi.spyOn(client, "request").mockImplementation(async () => {
    if (++count === 1)
      return new Promise((resolve) => {
        finish = resolve;
      }) as never;
    return original as never;
  });
  let w = mount(Panel, {
    props: { client, merchantId: "1000", ownSession: true, section: "contact" },
  });
  mounted.push(w);
  await flushPromises();
  await w.setProps({ section: "settlement" });
  await flushPromises();
  expect(request).toHaveBeenCalledTimes(2);
  expect(request.mock.calls[0][1]?.signal?.aborted).toBe(true);
  finish({ ...original, remain_money: "20%" });
  await flushPromises();
  expect(w.find('[name="qq"]').exists()).toBe(false);
  expect(
    (w.get('[name="remain_money"]').element as HTMLInputElement).value,
  ).toBe("10%");
  expect(w.text()).not.toContain("保存成功");
});
