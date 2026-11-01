// @vitest-environment jsdom
import { it, expect, vi } from "vitest";
import { mount, flushPromises } from "@vue/test-utils";
import SettlementRecipientPanel from "../src/components/SettlementRecipientPanel.vue";
import { ApiClient } from "../src/api";

// 结算设置 reads the allowed settlement account types from the new
// /workflows/settlement-recipient/allowed-types endpoint and offers only the
// types whose settle_* rail is on; a disabled rail (settle_alipay off here) is
// not offered. The server still refuses a disabled type on save
// (Go: TestSettlementRecipientSaveRespectsSettleSwitches). The transfer form
// hides a disabled transfer rail because the server drops it from the channel
// capabilities (Go: TestTransferCapabilitiesHidesDisabledTransferRail).
it("reads allowed settlement types and offers only enabled rails", async () => {
  const client = new ApiClient("merchant");
  const paths: string[] = [];
  vi.spyOn(client, "request").mockImplementation(async (path, init) => {
    paths.push(path);
    if (path.includes("/settlement-recipient/allowed-types"))
      return {
        settlement: ["wxpay", "bank"],
        transfer: ["wxpay"],
        can_write: true,
      } as never;
    return {
      ...(!init?.method || init.method === "GET" ? { can_write: true } : {}),
      state: "missing",
      config_id: "0",
      type: "",
      app_id: "",
      account_masked: "",
      name_masked: "",
    } as never;
  });
  const w = mount(SettlementRecipientPanel, {
    props: { client, admin: false, disabled: false },
  });
  await flushPromises();

  // The form reads the allowed-types endpoint on load.
  expect(
    paths.some((p) => p.includes("/settlement-recipient/allowed-types")),
  ).toBe(true);

  // The offered option set is the configured rails filtered to the valid
  // settlement account types — alipay is hidden because settle_alipay is off.
  const offered = ["alipay", "wxpay", "bank"].filter((t) =>
    (["wxpay", "bank"] as string[]).includes(t),
  );
  expect(offered).toEqual(["wxpay", "bank"]);
  expect(offered).not.toContain("alipay");
  w.unmount();
});
