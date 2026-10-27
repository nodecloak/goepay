import { expect, it, vi } from "vitest";
import { mount, flushPromises } from "@vue/test-utils";
import WorkflowPage from "../src/components/WorkflowPage.vue";
import { ApiClient } from "../src/api";

const row = (overrides: Record<string, unknown> = {}) => ({
  id: "9007199254741001",
  direction: "in",
  peer_merchant_id: "9007199254740993",
  amount: "1.23",
  status: "approved",
  created_at: "2026-10-25T01:00:00Z",
  completed_at: "2026-10-25T02:00:00Z",
  remark: null,
  ...overrides,
});
const fixture = async (data = row()) => {
  const client = new ApiClient("merchant");
  const request = vi
    .spyOn(client, "request")
    .mockResolvedValue({ items: [data], total: 1 });
  const w = mount(WorkflowPage, {
    props: {
      client,
      area: "merchant",
      workflow: "internal-transfers",
      selfID: "9007199254740995",
    },
  });
  await flushPromises();
  return { w, request };
};
it("incoming164 renders recipient direction, exact counterpart IDs and terminal event time without write controls", async () => {
  const { w } = await fixture();
  expect(w.findAll("thead th").map((c) => c.text())).toEqual([
    "ID",
    "方向",
    "对方商户",
    "金额",
    "状态",
    "备注",
    "提交时间",
    "完成时间",
  ]);
  expect(w.text()).toContain("转入");
  expect(w.text()).toContain("9007199254740993");
  expect(w.text()).toContain("+1.23");
  expect(w.text()).toContain("成功");
  expect(w.text()).toContain("2026-10-25 10:00:00");
  expect(w.text()).toContain("未记录");
  expect(
    w.findAll("button").some((b) => /核准|拒绝|取消转账/.test(b.text())),
  ).toBe(false);
  w.unmount();
});
it("incoming164 applies direction and original status filters without converting merchant IDs to numbers", async () => {
  const { w, request } = await fixture();
  await w.get('select[aria-label="方向"]').setValue("in");
  await w.get('select[aria-label="状态"]').setValue("approved");
  await w.get('form[aria-label="站内转账筛选"]').trigger("submit");
  await flushPromises();
  const path = String(request.mock.calls.at(-1)![0]);
  expect(path).toContain("direction=in");
  expect(path).toContain("status=approved");
  expect(path).not.toContain("merchant_id=");
  expect(path).toContain("page=1");
  await w
    .get('button[data-action="reset-internal-transfers"]')
    .trigger("click");
  await flushPromises();
  expect(String(request.mock.calls.at(-1)![0])).not.toContain("direction=");
  w.unmount();
});
it("incoming164 shows pending completion as unrecorded and never as creation or updated time", async () => {
  const { w } = await fixture(
    row({ direction: "out", status: "pending", completed_at: null }),
  );
  expect(w.text()).toContain("转出");
  expect(w.text()).toContain("-1.23");
  expect(w.text()).toContain("待审核");
  expect(w.find("tbody tr").findAll("td").at(-1)!.text()).toBe("未记录");
  w.unmount();
});
it("incoming164 fails closed on malformed direction, unsafe IDs and mismatched terminal completion", async () => {
  for (const bad of [
    row({ direction: "sideways" }),
    row({ peer_merchant_id: 9007199254740993 }),
    row({ id: "9223372036854775808" }),
    row({ status: "pending", completed_at: "2026-10-25T02:00:00Z" }),
  ]) {
    const { w } = await fixture(bad);
    expect(w.find('[role="alert"]').exists()).toBe(true);
    expect(w.find("tbody tr[data-transfer-id]").exists()).toBe(false);
    w.unmount();
  }
});
it("incoming164 leaves the other merchant workflow layout and filter contract unchanged", async () => {
  const client = new ApiClient("merchant");
  const request = vi
    .spyOn(client, "request")
    .mockResolvedValue({ items: [], total: 0 });
  const w = mount(WorkflowPage, {
    props: { client, area: "merchant", workflow: "profitsharing" },
  });
  await flushPromises();
  expect(w.find('form[aria-label="站内转账筛选"]').exists()).toBe(false);
  expect(String(request.mock.calls[0]![0])).toBe(
    "/workflows/profitsharing?page=1&limit=20",
  );
  w.unmount();
});
