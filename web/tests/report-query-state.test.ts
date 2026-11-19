// @vitest-environment jsdom
import { afterEach, expect, it, vi } from "vitest";
import { flushPromises, mount, type VueWrapper } from "@vue/test-utils";
import { ApiClient } from "../src/api";
import ReportPage from "../src/components/ReportPage.vue";
import CsvExportButton from "../src/components/CsvExportButton.vue";

const wrappers: VueWrapper[] = [];
afterEach(() => {
  wrappers.splice(0).forEach((wrapper) => wrapper.unmount());
  vi.restoreAllMocks();
});

const result = {
  columns: [{ key: "dimension", label: "通道" }],
  items: [{ dimension: "one" }],
  total: 45,
};
function report(client: ApiClient, name = "transfer-channels") {
  const wrapper = mount(ReportPage, {
    props: { client, report: name },
    global: { stubs: { CsvExportButton: true } },
  });
  wrappers.push(wrapper);
  return wrapper;
}
const query = (path: unknown) =>
  new URLSearchParams(String(path).split("?")[1]);

it("keeps paging and CSV on the submitted filters while edits remain drafts", async () => {
  const client = new ApiClient("admin");
  const request = vi.spyOn(client, "request").mockResolvedValue(result);
  const wrapper = report(client);
  await flushPromises();
  await wrapper.get('input[aria-label="商户号"]').setValue("42");
  await wrapper.get('input[aria-label="开始日期"]').setValue("2026-11-01");
  await wrapper.get('input[aria-label="结束日期"]').setValue("2026-11-10");
  await wrapper.get("form").trigger("submit");
  await flushPromises();

  await wrapper.get('input[aria-label="商户号"]').setValue("99");
  await wrapper.get('input[aria-label="开始日期"]').setValue("2026-11-05");
  await wrapper.get('button[aria-label="下一页"]').trigger("click");
  await flushPromises();
  expect(query(request.mock.calls.at(-1)![0]).get("page")).toBe("2");
  expect(query(request.mock.calls.at(-1)![0]).get("uid")).toBe("42");
  expect(query(request.mock.calls.at(-1)![0]).get("from")).toBe("2026-11-01");
  const csv = query(wrapper.getComponent(CsvExportButton).props("path"));
  expect(csv.get("uid")).toBe("42");
  expect(csv.get("from")).toBe("2026-11-01");

  expect(wrapper.find('button[aria-label="刷新数据"]').exists()).toBe(false);
  await wrapper.get("form").trigger("submit");
  await flushPromises();
  expect(query(request.mock.calls.at(-1)![0]).get("uid")).toBe("99");
  expect(query(request.mock.calls.at(-1)![0]).get("page")).toBe("1");
});

it("clears counts and complaint summary after a failed page change instead of displaying stale totals", async () => {
  const client = new ApiClient("admin");
  const request = vi.spyOn(client, "request").mockResolvedValue({
    ...result,
    summary: { today_count: "9", today_money: "12.34" },
  });
  const wrapper = report(client, "complaints");
  await flushPromises();
  expect(wrapper.findAll(".stat").length).toBeGreaterThan(0);
  request.mockRejectedValueOnce(new Error("读取统计失败"));
  await wrapper.get('button[aria-label="下一页"]').trigger("click");
  await flushPromises();
  expect(wrapper.get('[role="alert"]').text()).toBe("读取统计失败");
  expect(wrapper.findAll(".stat")).toHaveLength(0);
  expect(wrapper.get(".table-summary").text()).toContain("0");
  expect(wrapper.getComponent(CsvExportButton).props("disabled")).toBe(true);
});

it("rejects an invalid date range without retaining or accepting a pending result", async () => {
  const client = new ApiClient("admin");
  let resolveOld!: (value: typeof result) => void;
  const request = vi.spyOn(client, "request").mockImplementationOnce(
    () =>
      new Promise((resolve) => {
        resolveOld = resolve;
      }),
  );
  const wrapper = report(client);
  await flushPromises();
  await wrapper.get('input[aria-label="开始日期"]').setValue("2026-11-10");
  await wrapper.get('input[aria-label="结束日期"]').setValue("2026-11-01");
  await wrapper.get("form").trigger("submit");
  resolveOld(result);
  await flushPromises();
  expect(request).toHaveBeenCalledTimes(1);
  expect(wrapper.get('[role="alert"]').text()).toContain(
    "开始日期不得晚于结束日期",
  );
  expect(wrapper.get(".table-summary").text()).toContain("0");
  expect(wrapper.find(".loading-row").exists()).toBe(false);
  expect(wrapper.getComponent(CsvExportButton).props("disabled")).toBe(true);
});

it("reloads a changed report context and ignores its preceding delayed reply", async () => {
  const client = new ApiClient("admin");
  let resolveOld!: (value: typeof result) => void;
  const request = vi
    .spyOn(client, "request")
    .mockImplementationOnce(
      () =>
        new Promise((resolve) => {
          resolveOld = resolve;
        }),
    )
    .mockResolvedValue({
      ...result,
      items: [{ dimension: "current" }],
      total: 1,
    });
  const wrapper = report(client);
  await flushPromises();
  await wrapper.setProps({ report: "transfer-accounts" });
  await flushPromises();
  expect(request.mock.calls.at(-1)![0]).toContain(
    "/reports/transfer-accounts?",
  );
  resolveOld({ ...result, items: [{ dimension: "stale" }] });
  await flushPromises();
  expect(wrapper.text()).toContain("current");
  expect(wrapper.text()).not.toContain("stale");
});
