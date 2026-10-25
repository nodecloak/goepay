import { it, expect, vi } from "vitest";
import { mount, flushPromises } from "@vue/test-utils";
import { ApiClient } from "../src/api";
import { viewKind } from "../src/menu";
import { settlementBatchBody } from "../src/settlement-batches";
import Page from "../src/components/SettlementBatchesPage.vue";

it("routes original batch menu independently and groups only canonical original request IDs", () => {
  expect(viewKind("admin", "/settle/batch")).toBe("settlement-batches");
  expect(() => settlementBatchBody("x", "1,1")).toThrow();
  expect(() => settlementBatchBody("x", "01")).toThrow();
  expect(settlementBatchBody("retry84", "9007199254740993,2")).toEqual({
    external_no: "retry84",
    request_ids: ["9007199254740993", "2"],
  });
});
it("shows original columns and ignores stale detail after switching batches", async () => {
  let old: (value: unknown) => void = () => {};
  const row = (id: string) => ({
    id,
    batch: "2026102500000000000" + id,
    allmoney: "9.70",
    count: 1,
    time: "2026-10-25T00:00:00Z",
    status: "active",
    all_paid: false,
    items: [],
  });
  const request = vi.fn((path: string) =>
    path === "/settlement-batches/1"
      ? new Promise((r) => (old = r))
      : Promise.resolve(
          path === "/settlement-batches/2"
            ? row("2")
            : { items: [row("1"), row("2")], total: 2, next_before_id: "0" },
        ),
  );
  const w = mount(Page, {
    props: {
      client: Object.assign(new ApiClient("admin"), { request }),
      adminClass: "super",
      ownSession: true,
    },
  });
  await flushPromises();
  for (const label of ["批次号", "总金额", "总笔数", "创建时间", "状态"])
    expect(w.text()).toContain(label);
  await w.get('[aria-label="查看批次 1"]').trigger("click");
  await w.get('[aria-label="查看批次 2"]').trigger("click");
  await flushPromises();
  old({ ...row("1"), batch: "stale-batch-secret84" });
  await flushPromises();
  expect(w.text()).not.toContain("stale-batch-secret84");
  expect(
    w
      .findAll("button")
      .find((b) => b.text() === "完成批次")
      ?.attributes("disabled"),
  ).toBeDefined();
  w.unmount();
});
it("denies delegated sessions before any data read", async () => {
  const request = vi.fn();
  const w = mount(Page, {
    props: {
      client: Object.assign(new ApiClient("admin"), { request }),
      adminClass: "super",
      ownSession: false,
    },
  });
  await flushPromises();
  expect(request).not.toHaveBeenCalled();
  expect(w.text()).toContain("本人管理员");
  w.unmount();
});
