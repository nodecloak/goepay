import { it, expect, vi } from "vitest";
import { mount, flushPromises } from "@vue/test-utils";
import { ApiClient } from "../src/api";
import ParentChannelSubject from "../src/components/ParentChannelSubject.vue";
it("shows original parent ordername fields and recovers unknown writes by GET only", async () => {
  const client = new ApiClient("admin");
  client.csrfToken = "original";
  const dto = {
    channel_id: "9007199254740993",
    ordername: "原[name]",
    ordername_rules: "[]",
    fingerprint: "a".repeat(64),
  };
  const spy = vi.spyOn(client, "request").mockImplementation(async (_p, o) => {
    if (o?.method === "PATCH") throw new Error("lost response");
    return dto;
  });
  const w = mount(ParentChannelSubject, {
    props: { client, allowed: true, channelID: dto.channel_id },
  });
  await flushPromises();
  expect(w.text()).toContain("自定义订单名称");
  expect(w.get('input[name="ordername"]').element).toHaveProperty(
    "value",
    "原[name]",
  );
  await w.get("form").trigger("submit");
  await w.get("[data-subject-confirm]").trigger("click");
  await flushPromises();
  expect(w.text()).toContain("结果未知");
  expect(spy.mock.calls.filter(([, o]) => o?.method === "PATCH")).toHaveLength(
    1,
  );
  await w.get("[data-subject-read]").trigger("click");
  await flushPromises();
  expect(spy.mock.calls.filter(([, o]) => o?.method === "PATCH")).toHaveLength(
    1,
  );
  client.csrfToken = "changed";
  window.dispatchEvent(new Event("focus"));
  await flushPromises();
  expect(w.find("input").exists()).toBe(false);
  w.unmount();
});
