import { expect, it, vi } from "vitest";
import { mount, flushPromises } from "@vue/test-utils";
import { ApiClient } from "../src/api";
import TransferBatchImport from "../src/components/TransferBatchImport.vue";
import {
  importDraftRows,
  importErrorLabel,
} from "../src/transfer-batch-import";
it("shows source row and fixed diagnostic without echoing arbitrary upstream text", () => {
  expect(importErrorLabel("row 2: positive_decimal_amount_required")).toContain(
    "第 2 行",
  );
  expect(importErrorLabel("private account and Private Name")).not.toContain(
    "Private Name",
  );
});
it("preserves the safe source-row diagnostic after the API error has been localized", async () => {
  vi.stubGlobal(
    "fetch",
    vi.fn().mockResolvedValue(
      new Response(
        JSON.stringify({
          code: -1,
          message: "row 3: positive_decimal_amount_required",
          data: { error: "positive_decimal_amount_required", source_row: 3 },
        }),
        { status: 422, headers: { "Content-Type": "application/json" } },
      ),
    ),
  );
  const client = new ApiClient("merchant");
  client.csrfToken = "synthetic-csrf";
  const wrapper = mount(TransferBatchImport, {
    props: {
      client,
      policy: { channels: [] },
      merchantID: "1",
      disabled: false,
    },
  });
  try {
    const input = wrapper.get('input[type="file"]');
    Object.defineProperty(input.element, "files", {
      value: [
        {
          name: "invalid.csv",
          size: 9,
          arrayBuffer: async () => new TextEncoder().encode("synthetic").buffer,
        },
      ],
    });
    await input.trigger("change");
    await flushPromises();
    expect(wrapper.get('[role="alert"]').text()).toBe(
      "文件校验失败，第 3 行：金额须为正数且最多两位小数",
    );
    expect(wrapper.find('[aria-label="文件校验预览"]').exists()).toBe(false);
    expect(wrapper.emitted("apply")).toBeUndefined();
  } finally {
    wrapper.unmount();
    vi.unstubAllGlobals();
  }
});
const policy = {
  channels: [
    { id: "7", types: ["alipay"] },
    {
      id: "8",
      types: ["bank"],
      bank_product: "wxpay_v2",
      bank_code_required: true,
    },
  ],
};
const preview = {
  merchant_id: "1",
  format: "csv",
  count: 1,
  total_cents: "10",
  rows: [
    {
      source_row: 2,
      serial: "1",
      account_masked: "0***78",
      name_masked: "P***me",
      amount: "0.10",
      has_remark: true,
    },
  ],
  draft_items: [
    {
      source_row: 2,
      serial: "1",
      account: "000123456789012345678",
      name: "Private Name",
      amount: "0.10",
      remark: "memo",
    },
  ],
};
it("preserves private text only in explicit current-memory draft with selected original capability", () => {
  expect(importDraftRows(preview, policy, "7", "alipay")).toEqual([
    {
      channel_id: "7",
      type: "alipay",
      account: "000123456789012345678",
      name: "Private Name",
      amount: "0.10",
      remark: "memo",
      bank_code: "",
    },
  ]);
  expect(importDraftRows(preview, policy, "8", "bank")[0]!.bank_code).toBe("");
});
it("rejects stale capability, unknown product, corrupt rows, precision and misleading masked summary", () => {
  for (const [channel, type] of [
    ["9", "alipay"],
    ["7", "wxpay"],
    ["8", "qqpay"],
  ])
    expect(() => importDraftRows(preview, policy, channel!, type!)).toThrow();
  for (const changed of [
    { ...preview, total_cents: "11" },
    { ...preview, count: 2 },
    { ...preview, draft_items: [{ ...preview.draft_items[0], amount: "1e3" }] },
    { ...preview, draft_items: [{ ...preview.draft_items[0], account: 123 }] },
    { ...preview, rows: [{ ...preview.rows[0], serial: "2" }] },
  ])
    expect(() => importDraftRows(changed, policy, "7", "alipay")).toThrow();
});
