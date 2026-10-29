// @vitest-environment jsdom
import { it, expect, vi } from "vitest";
import { mount, flushPromises } from "@vue/test-utils";
import MerchantScanShortcut192 from "../src/components/MerchantScanShortcut192.vue";
import type { ApiClient } from "../src/api";

vi.mock("qrcode", () => ({
  default: { toDataURL: vi.fn(async () => "data:image/png;base64,local") },
}));

const stubs = { RouterLink: { template: "<a><slot/></a>" } };

function client(caps: { platform_open: boolean; can_prepare: boolean }) {
  return {
    area: "merchant",
    request: vi.fn(async () => caps),
  } as unknown as ApiClient;
}

// 付款码收款 (打开收款页) is present in Pro's place. When scanpay is open for the
// merchant it links to the scan cashier; otherwise it is shown DISABLED with a
// one-line reason rather than hidden.
it("links to the scan cashier when 付款码收款 is enabled", async () => {
  const w = mount(MerchantScanShortcut192, {
    props: {
      client: client({ platform_open: true, can_prepare: true }),
      ownSession: true,
    },
    global: { stubs },
  });
  await flushPromises();
  expect(w.text()).toContain("付款码收款");
  const link = w.findAll("a").find((a) => a.text().includes("付款码收款"));
  const btn = w.findAll("button").find((b) => b.text().includes("付款码收款"));
  expect(link || btn).toBeTruthy();
  // not the disabled variant
  expect(w.find("button[disabled]").exists()).toBe(false);
  w.unmount();
});

it("shows 付款码收款 disabled with a reason when scanpay is not open", async () => {
  const w = mount(MerchantScanShortcut192, {
    props: {
      client: client({ platform_open: false, can_prepare: false }),
      ownSession: true,
    },
    global: { stubs },
  });
  await flushPromises();
  const disabled = w.find("button[disabled]");
  expect(disabled.exists()).toBe(true);
  expect(disabled.text()).toContain("付款码收款");
  expect(disabled.text()).toContain("平台未开启付款码收款");
  w.unmount();
});
