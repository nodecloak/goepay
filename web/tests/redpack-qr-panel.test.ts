// @vitest-environment jsdom
import { flushPromises, mount } from "@vue/test-utils";
import { expect, it, vi } from "vitest";
import { ApiClient } from "../src/api";
import RedpackPage from "../src/components/RedpackPage.vue";
import type { Redpack } from "../src/redpacks";

const toDataURL = vi.hoisted(() => vi.fn());
vi.mock("qrcode", () => ({ default: { toDataURL } }));

const redpack = (id: string): Redpack => ({
  id,
  merchant_id: "1234",
  out_biz_no: "202611191234560000" + id,
  channel_id: "1",
  type: "alipay",
  amount: 100,
  money: "1.00",
  remark: "",
  created_by: "merchant",
  status: "claimable",
  expires_at: "2026-11-20T12:34:56Z",
  created_at: "2026-11-19T12:34:56Z",
  claim_url: "https://example.invalid/red/" + id,
});

it("shows only the last requested QR code and clears the claim panel on close", async () => {
  const [a, b] = [redpack("1"), redpack("2")];
  let completeFirst = (_: string) => {};
  toDataURL.mockImplementation((url: string) =>
    url === a.claim_url
      ? new Promise<string>((resolve) => {
          completeFirst = resolve;
        })
      : Promise.resolve("data:image/png;base64,second"),
  );
  const client = new ApiClient("merchant");
  const request = vi
    .spyOn(client, "request")
    .mockResolvedValue({ items: [a, b], open: true });
  const wrapper = mount(RedpackPage, { props: { client } });
  try {
    await flushPromises();
    const buttons = wrapper
      .findAll("button")
      .filter((button) => button.text() === "二维码");
    await buttons[0]!.trigger("click");
    expect(wrapper.text()).toContain("正在生成领取二维码");
    await buttons[1]!.trigger("click");
    await flushPromises();
    completeFirst("data:image/png;base64,first");
    await flushPromises();
    const panel = wrapper.get('section[aria-label="红包领取二维码"]');
    expect(panel.text()).toContain(b.claim_url);
    expect(panel.text()).not.toContain(a.claim_url);
    expect(panel.get("img").attributes("src")).toBe(
      "data:image/png;base64,second",
    );
    expect(panel.find('button[aria-label="复制领取链接"]').exists()).toBe(true);
    await panel
      .findAll("button")
      .find((button) => button.text() === "关闭二维码")!
      .trigger("click");
    expect(wrapper.find(".redpack-qr").exists()).toBe(false);
    expect(request).toHaveBeenCalledExactlyOnceWith("/redpacks");
  } finally {
    wrapper.unmount();
    toDataURL.mockReset();
  }
});
