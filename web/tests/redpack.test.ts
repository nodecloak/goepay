import { expect, it } from "vitest";
import { navigationMenus, viewKind } from "../src/menu";
import {
  defaultRedpackBizNo,
  readRedpacks,
  redpackStatusLabel,
  validRedpackForm,
} from "../src/redpacks";
import { settingDefinition } from "../src/settings";

it("adds the original 创建红包 page for admin and merchant", () => {
  for (const area of ["admin", "merchant"] as const) {
    const titles = JSON.stringify(navigationMenus(area));
    expect(titles).toContain("创建红包");
    expect(viewKind(area, "/transfer/red")).toBe("redpacks");
  }
  const d = settingDefinition("user_transfer_red");
  expect([d.tab, d.label]).toEqual(["transfer", "用户中心创建红包功能"]);
});

it("defaults and validates the original 19-digit 交易号 form", () => {
  expect(defaultRedpackBizNo(new Date(2026, 9, 28, 11, 5, 9), 0)).toBe(
    "2026102811050911111",
  );
  const ok = {
    merchant_id: "1",
    out_biz_no: "2026102811050911111",
    amount: "1.00",
    remark: "恭喜发财",
    type: "alipay",
    channel_id: "",
  };
  expect(validRedpackForm(ok, true)).toBe("");
  expect(validRedpackForm({ ...ok, out_biz_no: "123" }, false)).toContain(
    "19位",
  );
  expect(validRedpackForm({ ...ok, amount: "1.001" }, false)).toContain("金额");
  expect(validRedpackForm({ ...ok, merchant_id: "" }, true)).toContain("商户");
  expect(redpackStatusLabel("claimable")).toBe("待领取");
  expect(() => readRedpacks({ items: [{ id: "1" }], open: true })).toThrow();
});
