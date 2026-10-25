import { it, expect, vi } from "vitest";
import { mount, flushPromises } from "@vue/test-utils";
import { reactive } from "vue";
import { ApiClient } from "../src/api";
import ReferralPage from "../src/components/ReferralPage.vue";
import ReferralDailyQueue from "../src/components/ReferralDailyQueue.vue";
import { readReferralDailyQueue } from "../src/referral";

for (const contradictory of [
  { state: "pending", pending_amount: "0" },
  { state: "pending", pending_amount: "500" },
  { state: "awarded", pending_amount: "501" },
  { state: "blocked", pending_amount: "501" },
  { state: "voided", pending_amount: "501" },
  { state: "pending", reward_amount: "0", pending_amount: "0" },
  {
    state: "awarded",
    platform_fee: "0",
    reward_amount: "0",
    pending_amount: "0",
  },
])
  it(`rejects contradictory verified state/money ${JSON.stringify(contradictory)}`, () => {
    expect(() =>
      readReferralDailyQueue(
        { items: [{ ...row, ...contradictory }], next_cursor: row.order_id },
        "1000",
        "0",
      ),
    ).toThrow();
  });

const row = {
  order_id: "9007199254740993",
  merchant_id: "8",
  promoter_id: "1000",
  trade_no: "daily-original-order",
  amount: "9007199254740993",
  platform_fee: "1001",
  reward_amount: "501",
  pending_amount: "501",
  state: "pending",
  activated_at: "2026-10-23T16:15:00Z",
  business_day: "2026-10-24",
  eligible_day: "2026-10-25",
  source_version: 2,
  source_verified: true,
};
const overview = {
  invited_count: "1",
  issued_amount: "0",
  voided_amount: "0",
  recovery_pending_amount: "0",
  today_issued: "0",
  yesterday_issued: "0",
};
function client(
  queue: unknown = { items: [row], next_cursor: row.order_id },
  ...capabilities: unknown[]
) {
  const capability = capabilities.length ? capabilities[0] : true;
  const request = vi.fn(
    (path: string, _options?: { signal?: AbortSignal; method?: string }) =>
      Promise.resolve(
        path === "/referral/overview"
          ? { ...overview, daily_queue_available: capability }
          : path === "/referral/token"
            ? {
                id: "1",
                merchant_id: "1000",
                version: "1",
                enabled: false,
                masked: "***",
              }
            : path.startsWith("/referral/daily-queue?")
              ? queue
              : { items: [], next_cursor: "" },
      ),
  );
  return Object.assign(new ApiClient("merchant"), { request });
}
it("reads only a capability-confirmed own queue and renders exact minor money with separate historical list", async () => {
  const c = client();
  const w = mount(ReferralPage, { props: { client: c } });
  await flushPromises();
  expect(w.find('[aria-label="每日待发奖励"]').exists()).toBe(true);
  expect(w.text()).toContain("90071992547409.93");
  expect(w.text()).toContain("5.01");
  expect(w.text()).toContain("2026-10-24 00:15:00");
  expect(w.text()).toContain("2026-10-25");
  expect(w.text()).toContain("原条款已验证");
  expect(w.text()).toContain("邀请奖励流水");
  expect(
    c.request.mock.calls.some(
      ([p]) => p === "/referral/daily-queue?after_id=0&limit=20",
    ),
  ).toBe(true);
  expect(
    c.request.mock.calls.every(
      ([p]) => !p.includes("merchant_id=") && !p.includes("promoter_id="),
    ),
  ).toBe(true);
  w.unmount();
});
for (const cap of [undefined, false, "true", 1])
  it(`missing or nonboolean capability ${String(cap)} never reads queue`, async () => {
    const c = client(undefined, cap);
    const w = mount(ReferralPage, { props: { client: c } });
    await flushPromises();
    expect(w.find('[aria-label="每日待发奖励"]').exists()).toBe(false);
    expect(c.request.mock.calls.some(([p]) => p.includes("/daily-queue"))).toBe(
      false,
    );
    w.unmount();
  });
for (const bad of [
  { promoter_id: "1001" },
  { order_id: 9007199254740993 },
  { amount: "01" },
  { reward_amount: "1002" },
  { pending_amount: "502" },
  { source_version: 1 },
  { source_verified: false },
  { state: "unknown" },
  { eligible_day: "2026-02-30" },
])
  it(`fails closed for invalid queue row ${JSON.stringify(bad)}`, async () => {
    const c = client({
      items: [{ ...row, ...bad }],
      next_cursor: row.order_id,
    });
    const w = mount(ReferralPage, { props: { client: c } });
    await flushPromises();
    const queue = w.find('[aria-label="每日待发奖励"]');
    expect(queue.exists()).toBe(true);
    expect(queue.text()).not.toContain(row.trade_no);
    expect(queue.find('[role="alert"]').exists()).toBe(true);
    expect(w.text()).toContain("邀请奖励流水");
    w.unmount();
  });
it("shows blocked and voided as independent original states without treating them as paid", async () => {
  const c = client({
    items: [
      { ...row, order_id: "1", state: "blocked", pending_amount: "0" },
      { ...row, order_id: "2", state: "voided", pending_amount: "0" },
    ],
    next_cursor: "2",
  });
  const w = mount(ReferralPage, { props: { client: c } });
  await flushPromises();
  expect(w.text()).toContain("退款结果待核实");
  expect(w.text()).toContain("已撤销待发奖励");
  w.unmount();
});
for (const context of ["client", "csrf", "area"] as const)
  it(`clears original queue and ignores late response after ${context} changes`, async () => {
    let finish: (v: unknown) => void = () => {};
    const c = reactive(client());
    const original = c.request.getMockImplementation()!;
    c.request.mockImplementation((path: string) =>
      path.startsWith("/referral/daily-queue?")
        ? new Promise((resolve) => {
            finish = resolve;
          })
        : original(path),
    );
    const w = mount(ReferralPage, { props: { client: c } });
    await flushPromises();
    expect(c.request.mock.calls.some(([p]) => p.includes("/daily-queue"))).toBe(
      true,
    );
    if (context === "client") await w.setProps({ client: client() });
    else if (context === "csrf") c.csrfToken = "replacement-session";
    else c.area = "admin";
    finish({ items: [row], next_cursor: row.order_id });
    await flushPromises();
    expect(w.text()).not.toContain(row.trade_no);
    expect(w.find('[aria-label="每日待发奖励"]').exists()).toBe(false);
    w.unmount();
  });
for (const key of ["available", "promoterId"] as const)
  it(`clears a loaded queue immediately when ${key} changes`, async () => {
    const c = client();
    const w = mount(ReferralDailyQueue, {
      props: { client: c, available: true, promoterId: "1000" },
    });
    await flushPromises();
    expect(w.text()).toContain(row.trade_no);
    await w.setProps(
      key === "available" ? { available: false } : { promoterId: "1001" },
    );
    expect(w.text()).not.toContain(row.trade_no);
    expect(c.request).toHaveBeenCalledTimes(1);
    w.unmount();
  });
it("paginates only the original exact decimal order cursor and never submits a write", async () => {
  const c = client();
  const w = mount(ReferralDailyQueue, {
    props: { client: c, available: true, promoterId: "1000" },
  });
  await flushPromises();
  c.request.mockResolvedValue({ items: [], next_cursor: "" });
  await w
    .findAll("button")
    .find((b) => b.text() === "下一页待发奖励")!
    .trigger("click");
  await flushPromises();
  expect(c.request.mock.calls[1]![0]).toBe(
    "/referral/daily-queue?after_id=9007199254740993&limit=20",
  );
  expect(w.text()).toContain("暂无每日待发奖励记录");
  expect(c.request.mock.calls.every(([, o]) => !o || !("method" in o))).toBe(
    true,
  );
  w.unmount();
});
it("strips undocumented proof or raw recipient fields and rejects invalid or nonadvancing cursors", () => {
  const page = readReferralDailyQueue(
    {
      items: [{ ...row, proof_hash: "never-render", account: "raw-secret" }],
      next_cursor: row.order_id,
    },
    "1000",
    "0",
  );
  expect(JSON.stringify(page)).not.toContain("never-render");
  expect(JSON.stringify(page)).not.toContain("raw-secret");
  for (const value of [
    { items: [row], next_cursor: "01" },
    { items: [], next_cursor: "1" },
    { items: [row, row], next_cursor: row.order_id },
    { items: [row], next_cursor: row.order_id, after: row.order_id },
  ])
    expect(() =>
      readReferralDailyQueue(
        value,
        "1000",
        "after" in value ? (value.after as string) : "0",
      ),
    ).toThrow();
});
