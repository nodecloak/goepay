import { expect, it } from "vitest";
import {
  printingState,
  manualPrintBody,
  printingMoney,
  recoverPrintBody,
} from "../src/printing";

it("distinguishes receipt acceptance and uncertainty from physical printing", () => {
  expect(printingState("accepted", "excelsecu")).toContain(
    "不代表实际打印完成",
  );
  expect(printingState("accepted", "local_fake")).toContain("本地模拟");
  expect(printingState("unknown", "excelsecu")).toContain("禁止自动重发");
  expect(printingState("sending", "excelsecu")).toContain("处理中");
  expect(printingState("invented", "local_fake")).toContain("未识别");
});

it("keeps receipt money and retry choices exact", () => {
  expect(printingMoney("9007199254740993")).toBe("90071992547409.93");
  expect(() => printingMoney(12)).toThrow();
  expect(manualPrintBody("local-1")).toEqual({
    external_no: "local-1",
    previous_job_id: "0",
    acknowledge_duplicate: false,
  });
  expect(() => manualPrintBody("")).toThrow();
  expect(recoverPrintBody("44")).toEqual({ expected_task_id: "44" });
  for (const invalid of [0, "0", "044", "9223372036854775808", null])
    expect(() => recoverPrintBody(invalid)).toThrow();
});
