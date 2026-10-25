import { it, expect, vi } from "vitest";
import { mount, flushPromises } from "@vue/test-utils";
import IdentityProfileLookup from "../src/components/IdentityProfileLookup.vue";
import { ApiClient } from "../src/api";
it("independent profile capability is lazy, owner-only and default disabled remains honest", async () => {
  const request = vi.fn(async () => ({
    merchant_id: "1",
    available: false,
    reason: "disabled",
    profiles: [],
    url_link_available: false,
  }));
  for (const ownSession of [false, true]) {
    request.mockClear();
    const w = mount(IdentityProfileLookup, {
      props: {
        client: Object.assign(new ApiClient("merchant"), { request }),
        area: "merchant",
        ownSession,
      },
    });
    await flushPromises();
    expect(request).toHaveBeenCalledTimes(ownSession ? 1 : 0);
    if (ownSession) expect(w.text()).toContain("disabled");
    w.unmount();
  }
});

import IdentityLookupPage from "../src/components/IdentityLookupPage.vue";
it("original lookup page exposes a separate lazy profile tab while retaining legacy behavior", async () => {
  const request = vi.fn(async () => ({
    merchant_id: "1",
    available: false,
    reason: "disabled",
    profiles: [],
    channels: [],
    url_link_available: false,
  }));
  const w = mount(IdentityLookupPage, {
    props: {
      client: Object.assign(new ApiClient("merchant"), { request }),
      area: "merchant",
      ownSession: true,
    },
  });
  await flushPromises();
  expect(w.findAll("button").some((b) => b.text() === "独立应用与小程序")).toBe(
    true,
  );
  w.unmount();
});

it("explicit capability reload after selecting a profile does not cancel its own read", async () => {
  const p = {
    id: "11",
    revision: "1",
    channel_id: "7",
    provider: "wxpay",
    kind: "mini",
    app_id: "independent-app",
    path: "pages/id",
  };
  const request = vi.fn(async () => ({
    merchant_id: "1",
    available: true,
    reason: "",
    profiles: [p],
    url_link_available: false,
  }));
  const w = mount(IdentityProfileLookup, {
    props: {
      client: Object.assign(new ApiClient("merchant"), { request }),
      area: "merchant",
      ownSession: true,
    },
  });
  await flushPromises();
  await w.get("select").setValue("11");
  await flushPromises();
  await w
    .findAll("button")
    .find((b) => b.text() === "读取独立应用")!
    .trigger("click");
  await flushPromises();
  expect(w.get("select").findAll("option").length).toBe(2);
  w.unmount();
});
