// @vitest-environment jsdom
import { it, expect, vi, afterEach } from "vitest";
import { mount, flushPromises } from "@vue/test-utils";
import ResourcePage from "../src/components/ResourcePage.vue";
import { ApiClient } from "../src/api";
import {
  originalMerchantFields,
  stubMerchantList,
} from "./merchant-list-fixture";

afterEach(() => {
  document.body.innerHTML = "";
  vi.restoreAllMocks();
});

it("renders merchant 状态 as compact chips (正常/支付/结算), not sentences", async () => {
  const client = new ApiClient("admin");
  stubMerchantList(client, originalMerchantFields);
  const wrapper = mount(ResourcePage, {
    props: {
      client,
      resource: "merchants",
      ownSession: true,
      adminClass: "super",
    },
  });
  await flushPromises();
  const tags = wrapper
    .findAll(".merchant-state-tags .pa-tag")
    .map((t) => t.text());
  expect(tags).toEqual(["正常", "支付", "结算"]);
  const stateCellText = wrapper.find(".merchant-state-tags").text();
  expect(stateCellText).not.toContain("收款配置");
  expect(stateCellText).not.toContain("核心风控");
  wrapper.unmount();
});
