// @vitest-environment jsdom
import { it, expect, vi } from "vitest";
import { mount, flushPromises } from "@vue/test-utils";
import { ApiClient } from "../src/api";
import FundsAuthorizationModal from "../src/components/FundsAuthorizationModal.vue";

it("labels bound per-merchant maturity and minimum rules in the confirmation summary", async () => {
  const client = new ApiClient("admin");
  vi.spyOn(client, "request").mockResolvedValue({
    challenge_id: "synthetic-group-summary",
    expires_at: new Date(Date.now() + 60000).toISOString(),
    summary: {
      group_maturity_rules: "商户1：D+1; 商户2：D+0",
      group_minimum_rules: "商户1：1.23元; 商户2：4.56元",
    },
  });
  const wrapper = mount(FundsAuthorizationModal, {
    props: {
      client,
      target: "/console/v1/admin/settlement-plans/1/authorizations",
      body: {},
    },
  });
  await flushPromises();
  expect(wrapper.text()).toContain("各商户结算周期");
  expect(wrapper.text()).toContain("各商户最低结算金额");
  expect(wrapper.text()).toContain("商户1：D+1; 商户2：D+0");
  expect(wrapper.text()).toContain("商户1：1.23元; 商户2：4.56元");
  wrapper.unmount();
});
