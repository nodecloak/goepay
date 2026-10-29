import { it, expect } from "vitest";
import { mount } from "@vue/test-utils";
import TransferListTable from "../src/components/TransferListTable.vue";
import {
  readTransferListRow,
  originalTransferColumns,
} from "../src/transfer-list";
import { transferListFixture } from "./fixtures/transfer-list";

const fixture = (overrides: Record<string, unknown> = {}) =>
  transferListFixture({
    amount: "10000",
    fee_amount: "200",
    cost_amount: "10200",
    remark: "原始转账业务备注".repeat(4),
    ...overrides,
  });

it("projects the original remark, exact total cost and actual provider payment time independently of status", () => {
  const row = readTransferListRow(
    fixture({ status: "succeeded", pay_date: "2026-10-25 23:31:02" }),
  );
  expect(row).toMatchObject({
    amount: "100.00",
    fee: "2.00",
    cost: "102.00",
    remark: "原始转账业务备注".repeat(4),
    payDate: "2026-10-25 23:31:02",
  });
  expect(
    readTransferListRow(fixture({ status: "succeeded", pay_date: "" })),
  ).toMatchObject({ status: "转账成功", payDate: "未记录" });
  expect(
    readTransferListRow(
      fixture({ status: "pending", pay_date: "2026-10-25T15:31:02Z" }),
    ),
  ).toMatchObject({ status: "正在处理", payDate: "2026-10-25 23:31:02" });
  expect(readTransferListRow(transferListFixture()).remark).toBe("");
});

for (const area of ["admin", "merchant"] as const) {
  it(`${area} preserves an authenticated original remark containing a tab`, () => {
    const value = fixture({ remark: "含\t制表符" });
    expect(readTransferListRow(value).remark).toBe("含\t制表符");
    const view = mount(TransferListTable, {
      props: { items: [value], area, busy: false },
    });
    expect(view.find('[role="alert"]').exists()).toBe(false);
    // Wave 3: 备注 is the muted second line of the 付款方式 column (Pro pairing).
    expect(view.get(".transfer-remark").element.textContent).toBe("含\t制表符");
    view.unmount();
  });
  it(`${area} renders three secondary fields inside their original columns without losing identifiers or actions`, () => {
    const view = mount(TransferListTable, {
      props: {
        items: [
          fixture({ status: "succeeded", pay_date: "2026-10-25 23:31:02" }),
        ],
        area,
        busy: false,
      },
    });
    // Wave 3: each paired column stacks a 主 label over a muted 次 label, so a
    // header's text is the concatenation of both (e.g. 付款方式 + 备注).
    expect(view.findAll("th").map((v) => v.text())).toEqual([
      ...originalTransferColumns(area).map((v) =>
        v.sub ? `${v.label}${v.sub}` : v.label,
      ),
      "操作",
    ]);
    const cells = view.findAll("tbody tr")[0]!.findAll("td");
    const cell = (key: string) =>
      cells[originalTransferColumns(area).findIndex((v) => v.key === key)]!;
    // Wave 3: 备注 now rides the 付款方式 column; 手续费 stays its own column.
    expect(cell("method").text()).toContain("原始转账业务备注".repeat(4));
    expect(cell("fee").text()).toContain("¥2.00");
    expect(cell("amount").text()).toContain("花费金额：¥102.00");
    expect(cell("createdAt").text()).toContain("付款时间：2026-10-25 23:31:02");
    expect(view.text()).toContain("1000000000000000001");
    expect(view.find('[data-transfer-id="9007199254740993"]').exists()).toBe(
      true,
    );
    view.unmount();
  });
}

it("empty remark and payment time never borrow another field or imply success", () => {
  const view = mount(TransferListTable, {
    props: {
      items: [fixture({ remark: "", pay_date: "", status: "unknown" })],
      area: "merchant",
      busy: false,
    },
  });
  expect(view.text()).toContain("付款时间：未记录");
  expect(view.text()).toContain("结果待核对");
  expect(view.text()).not.toContain("备注：");
  view.unmount();
});
