import { expect, it } from "vitest";
import { settingDefinition, settingStorage } from "../src/settings";

it("shows the original WeChat merchant-transfer settings on the transfer tab", () => {
  const type = settingDefinition("transfer_wxpay_type");
  expect(type.tab).toBe("transfer");
  expect(type.label).toBe("微信V3转账产品类型");
  expect(type.options?.map((o) => [o.value, o.label])).toEqual([
    ["0", "商家转账到零钱（旧）"],
    ["1", "商家转账（新）"],
  ]);
  const scene = settingDefinition("transfer_wxpay_scene_id");
  const types = settingDefinition("transfer_wxpay_info_type");
  const contents = settingDefinition("transfer_wxpay_info_content");
  expect([scene.label, types.label, contents.label]).toEqual([
    "微信商家转账-场景ID",
    "微信商家转账-报备信息类型",
    "微信商家转账-报备信息内容",
  ]);
  expect([scene, types, contents].every((d) => d.tab === "transfer")).toBe(
    true,
  );
  expect(scene.hint).toContain("1001-现金营销");
  expect(settingStorage(type, "1")).toBe("1");
  expect(() => settingStorage(type, "2")).toThrow();
  expect(settingStorage(scene, "1005")).toBe("1005");
  expect(() => settingStorage(scene, "1001-现金营销")).toThrow();
  expect(settingStorage(types, "岗位类型|报酬说明")).toBe("岗位类型|报酬说明");
  expect(settingStorage(contents, "外卖员|7月份配送费")).toBe(
    "外卖员|7月份配送费",
  );
  expect(() => settingStorage(types, "a||b")).toThrow();
  expect(() =>
    settingStorage(types, "一二三四五六七八九十一二三四五六"),
  ).toThrow();
  expect(() => settingStorage(contents, "1|2|3|4|5|6|7|8|9")).toThrow();
});
