// @vitest-environment jsdom
import { it, expect, vi } from "vitest";
import { mount, flushPromises } from "@vue/test-utils";
import { ApiClient } from "../src/api";
import TransferBatchPage from "../src/components/TransferBatchPage.vue";
import RedpackPage from "../src/components/RedpackPage.vue";

// Both pages use the full-width records card of 付款记录: the intro is a
// table-note (inset like the toolbar and the first table column), not a bare
// paragraph at the card edge.
it("批量付款 uses the records card with an inset note, and explains a grey 新建批量付款", async () => {
  const admin = new ApiClient("admin");
  const w = mount(TransferBatchPage, {
    props: { client: admin },
    global: { stubs: { teleport: true } },
  });
  await flushPromises();
  const card = w.get("section.records.batch-records");
  expect(card.find(":scope > p.table-note").text()).toContain("先整批预留");
  expect(card.find(":scope > p.muted").exists()).toBe(false);
  const create = card.get(".table-tools button");
  expect(create.text()).toBe("新建批量付款");
  expect(create.attributes("disabled")).toBeDefined();
  expect(card.get(".batch-hint").text()).toBe("请先输入付款商户并查询");
  w.unmount();

  const merchant = new ApiClient("merchant");
  vi.spyOn(merchant, "request").mockImplementation(
    async (path: string) =>
      (path.startsWith("/transfer-batches")
        ? { items: [], total: 0, next_before_id: "0" }
        : { writes_enabled: false }) as never,
  );
  const m = mount(TransferBatchPage, {
    props: { client: merchant },
    global: { stubs: { teleport: true } },
  });
  await flushPromises();
  expect(m.get(".batch-hint").text()).toBe(
    "当前商户未开启付款能力，暂不能新建批量付款",
  );
  m.unmount();
});

it("创建红包 uses the records card with an inset note", async () => {
  const client = new ApiClient("admin");
  vi.spyOn(client, "request").mockResolvedValue({ items: [] } as never);
  const w = mount(RedpackPage, {
    props: { client },
    global: { stubs: { teleport: true } },
  });
  await flushPromises();
  const card = w.get("section.records.redpack-records");
  expect(card.find(":scope > p.table-note").text()).toContain(
    "收款人扫描二维码授权后领取",
  );
  expect(card.find(":scope > p.muted").exists()).toBe(false);
  w.unmount();
});
