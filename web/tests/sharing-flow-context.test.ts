import { expect, it, vi } from "vitest";
import { mount, flushPromises } from "@vue/test-utils";
import { ApiClient } from "../src/api";
import ChannelSharingPage from "../src/components/ChannelSharingPage.vue";
it("opens the operation panel on the exact row merchant without requiring a second manual owner selection", async () => {
  const client = new ApiClient("admin");
  client.csrfToken = "synthetic";
  const request = vi
    .spyOn(client, "request")
    .mockImplementation(async (path) =>
      path.includes("/channels")
        ? ({
            merchant_id: "9007199254740993",
            available: false,
            reason: "本地没有实时通道",
            channels: [],
          } as never)
        : ({ items: [], next_cursor: "0" } as never),
    );
  const view = mount(ChannelSharingPage, {
    props: {
      client,
      area: "admin",
      page: "order",
      initialMerchantId: "9007199254740993",
      initialExternalNo: "original-sharing",
    },
  });
  try {
    await flushPromises();
    expect(view.text()).toContain("商户 ID 9007199254740993");
    expect(view.find('input[aria-label="商户 ID"]').exists()).toBe(false);
    expect(view.find("form.sharing-toolbar").exists()).toBe(false);
    const targets = request.mock.calls.map(([path]) => path).sort();
    expect(targets).toEqual([
      "/channel-sharing/channels?merchant_id=9007199254740993",
      "/channel-sharing/finishes?merchant_id=9007199254740993&limit=100",
      "/channel-sharing/orders?merchant_id=9007199254740993&limit=20",
      "/channel-sharing/returns?merchant_id=9007199254740993&limit=20",
    ]);
    expect(view.text()).toContain("original-sharing");
    expect(
      request.mock.calls.every(([, o]) => !o?.method || o.method === "GET"),
    ).toBe(true);
  } finally {
    view.unmount();
  }
});
