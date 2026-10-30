// @vitest-environment jsdom
import { it, expect, vi } from "vitest";
import { mount, flushPromises } from "@vue/test-utils";
import { ApiClient } from "../src/api";
import ReportPage from "../src/components/ReportPage.vue";

// B04 #12: the 通道数据统计 (transfer-channels) report gains epay_pro's
// channelStat 付款方式 (type) and 商户号 (uid) search fields. Both are optional
// and only appear on this report; they must flow through to the request query.
it("adds 付款方式 and 商户号 filters to the transfer-channels report query", async () => {
  const admin = new ApiClient("admin");
  const request = vi.spyOn(admin, "request").mockResolvedValue({
    columns: [
      { key: "dimension", label: "通道ID" },
      { key: "paid_money", label: "成功金额（元）" },
    ],
    items: [],
    total: 0,
  } as any);
  const w = mount(ReportPage, {
    props: { client: admin, report: "transfer-channels" },
    attachTo: document.body,
  });
  await flushPromises();
  // Both filter fields render on this report.
  const labels = w.findAll("label").map((l) => l.text());
  expect(labels.some((t) => t.includes("付款方式"))).toBe(true);
  expect(labels.some((t) => t.includes("商户号"))).toBe(true);

  // Choose 微信 on the 付款方式 KitSelect (the one whose aria-label is 付款方式).
  const methodTrigger = w
    .findAll(".pa-select-trigger")
    .find((b) => b.attributes("aria-label") === "付款方式")!;
  (methodTrigger.element as HTMLButtonElement).click();
  await flushPromises();
  const option = w.findAll('[role="option"]').find((o) => o.text() === "微信")!;
  const click = new MouseEvent("click", { bubbles: true, cancelable: true });
  option.element.dispatchEvent(click);
  await flushPromises();

  // Type a merchant id into the native 商户号 input.
  const uid = w.find('input[aria-label="商户号"]');
  await uid.setValue("42");
  await w.find("form").trigger("submit");
  await flushPromises();

  const url = request.mock.calls.at(-1)![0] as string;
  expect(url).toContain("/reports/transfer-channels");
  expect(url).toContain("type=wxpay");
  expect(url).toContain("uid=42");
  w.unmount();
});

// The 商户号 field never leaks onto the per-account report, which has no uid
// dimension; 付款方式 stays on both transfer reports.
it("shows 商户号 only on transfer-channels, 付款方式 on both transfer reports", async () => {
  const admin = new ApiClient("admin");
  vi.spyOn(admin, "request").mockResolvedValue({
    columns: [],
    items: [],
    total: 0,
  } as any);
  const accounts = mount(ReportPage, {
    props: { client: admin, report: "transfer-accounts" },
  });
  await flushPromises();
  const accountLabels = accounts.findAll("label").map((l) => l.text());
  expect(accountLabels.some((t) => t.includes("付款方式"))).toBe(true);
  expect(accountLabels.some((t) => t.includes("商户号"))).toBe(false);
  accounts.unmount();

  // A non-transfer report (daily) carries neither.
  const daily = mount(ReportPage, {
    props: { client: admin, report: "complaints" },
  });
  await flushPromises();
  const dailyLabels = daily.findAll("label").map((l) => l.text());
  expect(dailyLabels.some((t) => t.includes("付款方式"))).toBe(false);
  daily.unmount();
});
