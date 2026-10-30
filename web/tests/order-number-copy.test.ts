import { mount, flushPromises } from "@vue/test-utils";
import { afterEach, describe, expect, it, vi } from "vitest";
import CopyButton from "../src/components/CopyButton.vue";
import DataTable from "../src/components/DataTable.vue";
import OrderDetails from "../src/components/OrderDetails.vue";
import { ApiClient } from "../src/api";
import { copyText, isOrderNumberKey } from "../src/order-number";

const TRADE = "2026103011522438127";
const OUT = "M-20261030-0001";
const API = "4200001234202610301234567890";

function stubClipboard(writeText: (text: string) => Promise<void>) {
  Object.defineProperty(navigator, "clipboard", {
    configurable: true,
    value: { writeText: vi.fn(writeText) },
  });
  return navigator.clipboard.writeText as ReturnType<typeof vi.fn>;
}
afterEach(() => {
  vi.restoreAllMocks();
  Object.defineProperty(navigator, "clipboard", {
    configurable: true,
    value: undefined,
  });
});

describe("order numbers", () => {
  it("knows which fields are order or refund numbers", () => {
    for (const key of [
      "trade_no",
      "out_trade_no",
      "api_trade_no",
      "refund_no",
      "out_refund_no",
    ])
      expect(isOrderNumberKey(key), key).toBe(true);
    for (const key of ["merchant_id", "amount", "name", "client_ip"])
      expect(isOrderNumberKey(key), key).toBe(false);
  });

  it("shows order and refund numbers in the code font in the lists", () => {
    const orders = mount(DataTable, {
      props: {
        orders: true,
        items: [
          { id: "1", trade_no: TRADE, out_trade_no: OUT, status: "paid" },
        ],
        columns: [{ key: "trade_no", label: "系统订单号" }],
      },
    });
    expect(orders.get("button.trade-link").classes()).toContain("pa-order-no");
    expect(orders.get("button.trade-link").text()).toBe(TRADE);
    const merchantNo = orders.findAll("small").find((s) => s.text() === OUT);
    expect(merchantNo?.classes()).toContain("pa-order-no");

    const refunds = mount(DataTable, {
      props: {
        refunds: true,
        items: [
          {
            id: "2",
            refund_no: "R202610300001",
            out_refund_no: "MR-1",
            trade_no: TRADE,
            status: "succeeded",
          },
        ],
        columns: [{ key: "refund_no", label: "退款单号" }],
      },
    });
    const cell = refunds.get("td.col-refund_no");
    expect(cell.get("span.cell").classes()).toContain("pa-order-no");
    expect(cell.get("small").classes()).toContain("pa-order-no");

    const generic = mount(DataTable, {
      props: {
        items: [{ id: "3", trade_no: TRADE, merchant_id: "1000" }],
        columns: [
          { key: "trade_no", label: "关联订单号" },
          { key: "merchant_id", label: "商户 ID" },
        ],
      },
    });
    expect(generic.get("td.col-trade_no span.cell").classes()).toContain(
      "pa-order-no",
    );
    expect(generic.get("td.col-merchant_id span.cell").classes()).not.toContain(
      "pa-order-no",
    );
  });

  it("copies the exact value and says so", async () => {
    const write = stubClipboard(async () => {});
    const w = mount(CopyButton, {
      props: { text: TRADE, label: "系统订单号" },
      attachTo: document.body,
    });
    const button = w.get("button");
    expect(button.attributes("aria-label")).toBe("复制系统订单号");
    await button.trigger("click");
    await flushPromises();
    expect(write).toHaveBeenCalledTimes(1);
    expect(write).toHaveBeenCalledWith(TRADE);
    expect(w.get("[role=status]").text()).toBe("系统订单号已复制");
    expect(button.attributes("title")).toBe("已复制");
    w.unmount();
  });

  it("reports a failed copy instead of claiming success", async () => {
    stubClipboard(async () => {
      throw new Error("denied");
    });
    const exec = vi.fn(() => false);
    Object.defineProperty(document, "execCommand", {
      configurable: true,
      value: exec,
    });
    const w = mount(CopyButton, {
      props: { text: TRADE, label: "系统订单号" },
      attachTo: document.body,
    });
    await w.get("button").trigger("click");
    await flushPromises();
    expect(exec).toHaveBeenCalledWith("copy");
    expect(w.get("[role=status]").text()).toBe("系统订单号复制失败");
    w.unmount();
  });

  it("falls back to execCommand where the Clipboard API is missing", async () => {
    const exec = vi.fn(() => true);
    Object.defineProperty(document, "execCommand", {
      configurable: true,
      value: exec,
    });
    const host = document.createElement("div");
    document.body.appendChild(host);
    expect(await copyText(OUT, host)).toBe(true);
    expect(exec).toHaveBeenCalledWith("copy");
    expect(host.querySelector("textarea")).toBeNull();
    host.remove();
    expect(await copyText("", host)).toBe(false);
  });

  it("puts a copy button after each order number in the detail sidebar", async () => {
    const write = stubClipboard(async () => {});
    const client = new ApiClient("admin");
    vi.spyOn(client, "request").mockImplementation(
      async () =>
        ({
          order: {
            trade_no: TRADE,
            out_trade_no: OUT,
            merchant_id: "1000",
            amount: "100",
            status: "paid",
            name: "商品",
          },
          reporting: { api_trade_no: API, client_ip: "192.0.2.10" },
        }) as never,
    );
    const w = mount(OrderDetails, {
      props: { client, tradeNo: TRADE, ownSession: true },
      attachTo: document.body,
      global: {
        stubs: {
          OrderIncomePanel: true,
          OrderPrintingPanel: true,
          NotificationDeliveryPanel: true,
          NotificationReturnAction200: true,
          PreauthorizationObservation: true,
          ScanRecoveryPanel: true,
        },
      },
    });
    await flushPromises();
    const numbers = w.findAll("dd .pa-order-no").map((n) => n.text());
    expect(numbers).toEqual([TRADE, OUT, API]);
    const labels = w
      .findAll("button.pa-copy-btn")
      .map((b) => b.attributes("aria-label"));
    expect(labels).toEqual([
      "复制系统订单号",
      "复制商户订单号",
      "复制接口订单号",
    ]);
    // A value that is not an order number gets no copy button.
    expect(
      w
        .findAll("dd")
        .some(
          (d) =>
            d.text().includes("192.0.2.10") &&
            d.find("button.pa-copy-btn").exists(),
        ),
    ).toBe(false);
    const buttons = w.findAll("button.pa-copy-btn");
    await buttons[1]!.trigger("click");
    await flushPromises();
    expect(write).toHaveBeenLastCalledWith(OUT);
    await buttons[2]!.trigger("click");
    await flushPromises();
    expect(write).toHaveBeenLastCalledWith(API);
    w.unmount();
  });
});
