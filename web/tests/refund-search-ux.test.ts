// @vitest-environment jsdom
import { it, expect } from "vitest";
import { mount } from "@vue/test-utils";
import { readListRouteQuery, appendListFilters } from "../src/list-filters";
import DataTable from "../src/components/DataTable.vue";
it("original refund column/value links retain the exact merchant request key and a canonical scoped selector", () => {
  const r = readListRouteQuery(
    { column: "out_refund_no", value: " shared-refund-request ", uid: "1" },
    "refunds",
    "admin",
    "1",
  );
  expect(r.error).toBe("");
  expect(r.search).toBe(" shared-refund-request ");
  expect(r.filters.search_field).toBe("out_refund_no");
  const q = new URLSearchParams();
  appendListFilters(q, r.filters, "refunds", "admin");
  expect(q.get("search_field")).toBe("out_refund_no");
  expect(q.get("merchant_id")).toBe("1");
});
it("refund links reject unknown fields, ambiguous aliases and another merchant while retaining all original three selectors", () => {
  for (const field of ["refund_no", "out_refund_no", "trade_no"])
    expect(
      readListRouteQuery({ column: field, value: "1" }, "refunds", "admin", "1")
        .error,
    ).toBe("");
  for (const raw of [
    { column: "f.id", value: "1" },
    { column: ["refund_no", "trade_no"], value: "1" },
    { column: "refund_no", search_field: "out_refund_no", value: "1" },
    { column: "refund_no", value: "1", q: "2" },
  ])
    expect(readListRouteQuery(raw, "refunds", "admin", "1").error).not.toBe("");
  expect(
    readListRouteQuery(
      { uid: "2", column: "refund_no", value: "1" },
      "refunds",
      "merchant",
      "1",
    ).error,
  ).toContain("本人");
});
it("refund table displays the real platform id and original merchant key under the original two-line header", () => {
  const w = mount(DataTable, {
    props: {
      refunds: true,
      columns: [{ key: "refund_no", label: "退款单号" }],
      items: [
        {
          id: "9223372036854775807",
          platform_refund_no: "9223372036854775807",
          out_refund_no: "merchant-refund-128",
          refund_no: "legacy-console-key",
        },
      ],
    },
  });
  expect(w.get("th").text()).toContain("商户退款单号");
  expect(w.get("td").text()).toContain("9223372036854775807");
  expect(w.get("td").text()).toContain("merchant-refund-128");
  expect(w.get("td").text()).not.toContain("legacy-console-key");
  w.unmount();
});
