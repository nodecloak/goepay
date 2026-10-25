import { afterEach, describe, it, expect } from "vitest";
import { mount, type VueWrapper } from "@vue/test-utils";
import { nextTick } from "vue";
import OrderActionMenu from "../src/components/OrderActionMenu.vue";
import DataTable from "../src/components/DataTable.vue";
const mounted: VueWrapper[] = [];
afterEach(() => {
  mounted.splice(0).forEach((w) => w.unmount());
  document.body.innerHTML = "";
});
function setup(status = "paid") {
  const w = mount(OrderActionMenu, {
    props: { row: { trade_no: "T123", status } },
    attachTo: document.body,
  });
  mounted.push(w);
  return w;
}
describe("order action popup interactions", () => {
  it("preserves details and replaces only the old standalone admin refund button", () => {
    const w = mount(DataTable, {
      props: {
        orders: true,
        adminOrders: true,
        columns: [{ key: "trade_no", label: "系统订单号" }],
        items: [{ trade_no: "T123", status: "paid" }],
      },
      attachTo: document.body,
    });
    mounted.push(w);
    const labels = w.findAll("button").map((b) => b.text());
    expect(labels).toContain("详情");
    expect(labels).not.toContain("申请退款");
    expect(w.get("button[aria-haspopup=menu]").attributes("aria-label")).toBe(
      "订单操作 T123",
    );
  });
  it("opens outside the scrolling table and emits only the chosen action", async () => {
    const w = setup();
    expect(w.emitted("action")).toBeUndefined();
    await w.get("button").trigger("click");
    await nextTick();
    const menu = document.querySelector("[role=menu]")!;
    expect(menu.parentElement).toBe(document.body);
    expect(
      Array.from(menu.querySelectorAll("button")).map((b) =>
        b.textContent?.trim(),
      ),
    ).toEqual([
      "改未完成",
      "API退款",
      "手动退款",
      "冻结订单",
      "重新通知",
      "删除订单",
    ]);
    (menu.querySelectorAll("button")[3] as HTMLButtonElement).click();
    await nextTick();
    expect(w.emitted("action")).toEqual([["freeze"]]);
    expect(document.querySelector("[role=menu]")).toBeNull();
  });
  it("supports arrow navigation, Escape, and returning focus to the trigger", async () => {
    const w = setup("pending");
    await w.get("button").trigger("keydown", { key: "ArrowDown" });
    await nextTick();
    let buttons = Array.from(
      document.querySelectorAll<HTMLButtonElement>("[role=menuitem]"),
    );
    expect(document.activeElement).toBe(buttons[0]);
    buttons[0].dispatchEvent(
      new KeyboardEvent("keydown", { key: "ArrowDown", bubbles: true }),
    );
    expect(document.activeElement).toBe(buttons[1]);
    buttons[1].dispatchEvent(
      new KeyboardEvent("keydown", { key: "Escape", bubbles: true }),
    );
    await nextTick();
    expect(document.querySelector("[role=menu]")).toBeNull();
    expect(document.activeElement).toBe(w.get("button").element);
    expect(w.emitted("action")).toBeUndefined();
  });
  it("closes on outside click and on a new order state", async () => {
    const w = setup();
    await w.get("button").trigger("click");
    await nextTick();
    document.body.dispatchEvent(new Event("pointerdown", { bubbles: true }));
    await nextTick();
    expect(document.querySelector("[role=menu]")).toBeNull();
    await w.get("button").trigger("click");
    await nextTick();
    await w.setProps({ row: { trade_no: "OTHER", status: "pending" } });
    expect(document.querySelector("[role=menu]")).toBeNull();
  });
});
