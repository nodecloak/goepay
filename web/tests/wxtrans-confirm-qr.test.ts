import { expect, it, vi } from "vitest";
import { flushPromises, mount } from "@vue/test-utils";
import TransferListTable from "../src/components/TransferListTable.vue";
import { readTransferListRow } from "../src/transfer-list";
import { transferListFixture } from "./fixtures/transfer-list";

const toDataURL = vi.hoisted(() =>
  vi.fn(async () => "data:image/png;base64,local207"),
);
vi.mock("qrcode", () => ({ default: { toDataURL } }));

const link =
  "https://pay.example.invalid/pay/wxtrans?n=9007199254740993&s=" +
  "a".repeat(64) +
  "&t=1793160604";

it("reads the payee link only for a bill still waiting for confirmation", () => {
  expect(
    readTransferListRow(
      transferListFixture({ status: "pending", confirm_url: link }),
    ).confirmURL,
  ).toBe(link);
  expect(readTransferListRow(transferListFixture()).confirmURL).toBe("");
  for (const bad of [
    { status: "succeeded", confirm_url: link },
    { status: "pending", confirm_url: "https://evil.example/collect?x=1" },
    { status: "pending", confirm_url: "javascript:alert(1)//pay/wxtrans?x" },
    { status: "pending", confirm_url: 7 },
  ])
    expect(() => readTransferListRow(transferListFixture(bad))).toThrow();
});

for (const area of ["admin", "merchant"] as const) {
  it(`${area} shows 确认收款 under 正在处理 and opens the original QR dialog`, async () => {
    const view = mount(TransferListTable, {
      props: {
        items: [transferListFixture({ status: "pending", confirm_url: link })],
        area,
        busy: false,
      },
      attachTo: document.body,
    });
    const button = view.get(".transfer-confirm");
    expect(button.text()).toBe("确认收款");
    await button.trigger("click");
    await flushPromises();
    const dialog = view.get('[role="dialog"]');
    expect(dialog.text()).toContain("收款方使用微信扫描以下二维码");
    expect(dialog.get("img").attributes("src")).toBe(
      "data:image/png;base64,local207",
    );
    expect(toDataURL).toHaveBeenLastCalledWith(link, {
      width: 230,
      margin: 2,
    });
    expect(dialog.findAll("button").map((b) => b.text())).toEqual([
      "复制链接",
      "关闭",
    ]);
    await dialog.findAll("button")[1]!.trigger("click");
    expect(view.find('[role="dialog"]').exists()).toBe(false);
    view.unmount();
  });
}

it("a settled transfer offers no QR", () => {
  const view = mount(TransferListTable, {
    props: {
      items: [transferListFixture({ status: "succeeded" })],
      area: "merchant",
      busy: false,
    },
  });
  expect(view.find(".transfer-confirm").exists()).toBe(false);
  view.unmount();
});
