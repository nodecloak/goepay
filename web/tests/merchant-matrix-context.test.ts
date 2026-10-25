// @vitest-environment jsdom
import { it, expect, vi } from "vitest";
import { mount, flushPromises } from "@vue/test-utils";
import { ApiClient } from "../src/api";
import ReportPage from "../src/components/ReportPage.vue";
it("revokes the admin matrix context before any late response after an identity change", async () => {
  const admin = new ApiClient("admin"),
    merchant = new ApiClient("merchant");
  let release: (v: any) => void = () => {};
  vi.spyOn(admin, "request").mockImplementation(
    () =>
      new Promise((resolve) => {
        release = resolve;
      }) as any,
  );
  const other = vi.spyOn(merchant, "request");
  const w = mount(ReportPage, {
    props: { client: admin, report: "merchants" },
  });
  await flushPromises();
  await w.setProps({ client: merchant });
  await flushPromises();
  expect(w.text()).toContain("仅管理员可查看");
  release({
    columns: [{ key: "uid", label: "商户ID" }],
    items: [{ uid: "late-private-merchant" }],
    summary: { total: "1.00" },
    total: 1,
  });
  await flushPromises();
  expect(w.text()).not.toContain("late-private-merchant");
  expect(other).not.toHaveBeenCalled();
  w.unmount();
});
