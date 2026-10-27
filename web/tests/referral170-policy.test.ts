import { it, expect, vi } from "vitest";
import { mount, flushPromises } from "@vue/test-utils";
import * as referral from "../src/referral";
import { ApiClient } from "../src/api";
import ReferralPage from "../src/components/ReferralPage.vue";
import policies from "../e2e/referral170-policies.json";

it("referral170 consumes only a confirmed own current policy and preserves unknown order estimates", () => {
  expect(typeof (referral as any).readReferralCurrentPolicy).toBe("function");
  const read = (referral as any).readReferralCurrentPolicy;
  expect(read(policies[0], "1").example.percentage_amount).toBe("3333");
  expect(read(policies[4], "1").rate_bps).toBeNull();
  expect(() => read({ ...policies[0], merchant_id: "2" }, "1")).toThrow();
  expect(() => read({ ...policies[0], rate_bps: NaN }, "1")).toThrow();
  expect(() =>
    read(
      {
        ...policies[0],
        example: { ...policies[0]!.example, order_estimate: "0" },
      },
      "1",
    ),
  ).toThrow();
});
it("referral170 the actual merchant referral page adds the current policy card while keeping token and history", async () => {
  const c = new ApiClient("merchant");
  c.csrfToken = "synthetic170";
  vi.spyOn(c, "request").mockImplementation(async (path) =>
    path === "/referral/overview"
      ? ({
          invited_count: "1",
          issued_amount: "9007199254740993",
          voided_amount: "0",
          recovery_pending_amount: "0",
          today_issued: "0",
          yesterday_issued: "0",
        } as never)
      : path === "/referral/token"
        ? ({
            id: "0",
            merchant_id: "1",
            version: "0",
            enabled: false,
            masked: "",
          } as never)
        : path === "/referral/current-policy"
          ? (policies[0] as never)
          : ({ items: [], next_cursor: "" } as never),
  );
  const view = mount(ReferralPage, { props: { client: c } });
  await flushPromises();
  expect(view.text()).toContain("当前通用返现比例");
  expect(view.text()).toContain("33.33%");
  expect(view.text()).toContain("真实订单预估：无法估算");
  expect(view.text()).toContain("90071992547409.93");
  expect(view.text()).toContain("推广链接");
  expect(view.text()).toContain("邀请奖励流水");
  view.unmount();
});
