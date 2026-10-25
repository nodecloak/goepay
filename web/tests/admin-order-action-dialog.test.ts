import { afterEach, describe, it, expect, vi } from "vitest";
import { mount, flushPromises, type VueWrapper } from "@vue/test-utils";
import AdminOrderActionDialog from "../src/components/AdminOrderActionDialog.vue";
import { ApiClient } from "../src/api";
const mounted: VueWrapper[] = [];
afterEach(() => mounted.splice(0).forEach((w) => w.unmount()));
const detail = {
  order: { trade_no: "T123", status: "paid", amount: "1000" },
  reporting: { refunded_amount: "200" },
  admin_control: { status: "", archived: false, revision: "7" },
  can_manual_refund: true,
};
function client() {
  return {
    area: "admin",
    request: vi.fn().mockResolvedValue(detail),
    requestResult: vi.fn(),
  } as unknown as ApiClient;
}
function setup(
  c: ApiClient,
  action: "mark-unpaid" | "manual-refund" | "api-refund",
) {
  const w = mount(AdminOrderActionDialog, {
    props: { client: c, tradeNo: "T123", action },
  });
  mounted.push(w);
  return w;
}
describe("direct original order action confirmations", () => {
  it("reads fresh facts and waits for explicit confirmation before changing a marker", async () => {
    const c = client();
    vi.mocked(c.requestResult).mockResolvedValue({ status: 200, data: {} });
    const w = setup(c, "mark-unpaid");
    await flushPromises();
    expect(c.requestResult).not.toHaveBeenCalled();
    expect(w.text()).toContain("不增减商户余额");
    await w.get("input").setValue("人工复核");
    await w.get("form").trigger("submit");
    await flushPromises();
    expect(c.requestResult).toHaveBeenCalledWith(
      "/orders/T123/admin-status",
      expect.objectContaining({
        body: expect.objectContaining({
          status: "pending",
          reason: "人工复核",
          expected_revision: "7",
        }),
      }),
    );
    expect(w.emitted("updated")).toHaveLength(1);
  });
  it("uses the original remaining amount and requires offline acknowledgement", async () => {
    const c = client();
    const w = setup(c, "manual-refund");
    await flushPromises();
    expect(
      (w.get("input[inputmode=decimal]").element as HTMLInputElement).value,
    ).toBe("8.00");
    await w.get("input[maxlength]").setValue("已线下退还");
    await w.get("form").trigger("submit");
    await flushPromises();
    expect(c.requestResult).not.toHaveBeenCalled();
    expect(w.text()).toContain("确认已完成线下退款");
  });
  it("keeps the same request and body when a failed dialog is reopened", async () => {
    const c = client();
    vi.mocked(c.requestResult)
      .mockRejectedValueOnce(new Error("connection lost"))
      .mockResolvedValueOnce({
        status: 200,
        data: { offline: true, provider_refund_sent: false },
      });
    let w = setup(c, "manual-refund");
    await flushPromises();
    await w.get("input[maxlength]").setValue("线下退款已完成");
    await w.get("input[type=checkbox]").setValue(true);
    await w.get("form").trigger("submit");
    await flushPromises();
    const first = vi.mocked(c.requestResult).mock.calls[0][1]?.body;
    w.unmount();
    mounted.splice(mounted.indexOf(w), 1);
    w = setup(c, "manual-refund");
    await flushPromises();
    expect(
      (w.get("input[maxlength]").element as HTMLInputElement).disabled,
    ).toBe(true);
    await w.get("form").trigger("submit");
    await flushPromises();
    expect(vi.mocked(c.requestResult).mock.calls[1][1]?.body).toEqual(first);
    expect(w.text()).toContain("支付通道未发起退款");
  });
  it("opens API refund from fresh order facts without issuing a refund itself", async () => {
    const c = client();
    const w = setup(c, "api-refund");
    await flushPromises();
    expect(c.request).toHaveBeenCalledWith("/orders/T123", expect.anything());
    expect(c.requestResult).not.toHaveBeenCalled();
    expect(w.emitted("refund")?.[0][0]).toMatchObject({
      trade_no: "T123",
      amount: "1000",
      refunded_amount: "200",
    });
  });
});
