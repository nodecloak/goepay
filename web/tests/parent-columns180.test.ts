import { it, expect, vi } from "vitest";
import { mount, flushPromises } from "@vue/test-utils";
import { ApiClient } from "../src/api";
import ParentChannelExtra from "../src/components/ParentChannelExtra.vue";
it("shows only real public cost estimates, fetches limits on demand and clears late session data", async () => {
  const client = new ApiClient("admin");
  client.csrfToken = "a";
  const spy = vi
    .spyOn(client, "request")
    .mockRejectedValue(new Error("not available"));
  const row = {
    id: "9007199254740993",
    configured: true,
    account: {
      fields: [
        { namespace: "config", name: "costrate", secret: false, value: "0.6" },
      ],
    },
  };
  const w = mount(ParentChannelExtra, {
    props: {
      client,
      allowed: true,
      row: row as never,
      cost: true,
      limits: false,
    },
  });
  await flushPromises();
  expect(w.text()).toContain("0.6%（估算）");
  expect(spy).not.toHaveBeenCalled();
  await w.setProps({ limits: true });
  await flushPromises();
  expect(spy).toHaveBeenCalledWith("/channel-policies/" + row.id + "/limits");
  expect(w.text()).toContain("未知");
  client.csrfToken = "b";
  window.dispatchEvent(new Event("focus"));
  await flushPromises();
  expect(w.text()).not.toContain("0.6");
  w.unmount();
});
