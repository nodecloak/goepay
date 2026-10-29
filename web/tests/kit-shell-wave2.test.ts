import { afterEach, expect, it } from "vitest";
import { mount } from "@vue/test-utils";
import ToastStack from "../src/components/ToastStack.vue";
import PaymentAmountTrend from "../src/components/PaymentAmountTrend.vue";
import { pushToast, toastSaved, dismissToast, useToasts } from "../src/toast";

afterEach(() => {
  // Clear any toasts left between cases.
  const { toasts } = useToasts();
  toasts.value.splice(0).forEach((t) => dismissToast(t.id));
});

it("pushes success/info toasts and renders them with the kit tone class", async () => {
  const w = mount(ToastStack);
  const id = toastSaved();
  await w.vm.$nextTick();
  expect(w.findAll(".toast")).toHaveLength(1);
  expect(w.get(".toast").classes()).toContain("toast--success");
  expect(w.get(".toast").text()).toContain("保存成功");
  pushToast("已导出", "info", 0);
  await w.vm.$nextTick();
  expect(w.findAll(".toast")).toHaveLength(2);
  expect(w.get(".toast--info").text()).toContain("已导出");
  // Click-to-dismiss removes a toast.
  await w.get(".toast--success").trigger("click");
  await w.vm.$nextTick();
  expect(w.findAll(".toast--success")).toHaveLength(0);
  dismissToast(id);
  w.unmount();
});

it("marks only the last point of the amount trend; earlier points are transparent hit targets", () => {
  const days = [
    {
      date: "2026-10-22",
      items: [
        { type: "alipay", money: "10.00" },
        { type: "wxpay", money: "5.00" },
      ],
    },
    {
      date: "2026-10-23",
      items: [
        { type: "alipay", money: "20.00" },
        { type: "wxpay", money: "6.00" },
      ],
    },
    {
      date: "2026-10-24",
      items: [
        { type: "alipay", money: "30.00" },
        { type: "wxpay", money: "7.00" },
      ],
    },
  ] as unknown as never;
  const w = mount(PaymentAmountTrend, { props: { days } });
  const marked = w.findAll(".trend-last-point");
  const hits = w.findAll(".trend-hit-point");
  // One marked (last) point per visible series; the rest are hit targets.
  expect(marked.length).toBeGreaterThanOrEqual(1);
  expect(hits.length).toBeGreaterThanOrEqual(marked.length);
  // 2px line stroke per kit.
  expect(w.get("polyline").attributes("stroke-width")).toBe("2");
  // Dashed grid.
  expect(w.get("line").attributes("stroke-dasharray")).toBe("3 3");
  w.unmount();
});
