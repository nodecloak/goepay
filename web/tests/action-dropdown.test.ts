// @vitest-environment jsdom
import { afterEach, it, expect } from "vitest";
import { mount, type VueWrapper } from "@vue/test-utils";
import { nextTick } from "vue";
import ActionDropdown from "../src/components/ActionDropdown.vue";
const mounted: VueWrapper[] = [];
afterEach(() => {
  mounted.forEach((w) => w.unmount());
  mounted.length = 0;
  document.body.innerHTML = "";
});
it("keeps read links in a keyboard menu outside a scrolling table", async () => {
  const w = mount(ActionDropdown, {
    props: {
      label: "商户关联记录 1000",
      items: [
        {
          id: "orders",
          label: "订单管理",
          href: "/admin/trade/order?uid=1000",
        },
      ],
    },
    attachTo: document.body,
  });
  mounted.push(w);
  await w.get("button").trigger("keydown", { key: "ArrowDown" });
  await nextTick();
  const link = document.querySelector<HTMLAnchorElement>('[role="menuitem"]')!;
  expect(link.parentElement?.parentElement).toBe(document.body);
  expect(link.getAttribute("href")).toBe("/admin/trade/order?uid=1000");
  expect(document.activeElement).toBe(link);
  link.dispatchEvent(
    new KeyboardEvent("keydown", { key: "Escape", bubbles: true }),
  );
  await nextTick();
  expect(document.querySelector('[role="menu"]')).toBeNull();
  expect(document.activeElement).toBe(w.get("button").element);
  expect(w.emitted("action")).toBeUndefined();
});
it("withdraws an open menu immediately when its permission or busy gate closes", async () => {
  const w = mount(ActionDropdown, {
    props: { label: "action", items: [{ id: "write", label: "确认" }] },
    attachTo: document.body,
  });
  mounted.push(w);
  await w.get("button").trigger("click");
  await nextTick();
  expect(document.querySelector('[role="menu"]')).not.toBeNull();
  await w.setProps({ disabled: true });
  expect(document.querySelector('[role="menu"]')).toBeNull();
  expect(w.emitted("action")).toBeUndefined();
});
it("does not turn an untrusted URL into an executable or external menu link", async () => {
  const w = mount(ActionDropdown, {
    props: {
      label: "links",
      items: [
        { id: "script", label: "invalid", href: "javascript:alert(1)" },
        { id: "external", label: "external", href: "//example.invalid" },
        { id: "backslash", label: "backslash", href: "/\\example.invalid" },
        {
          id: "local",
          label: "local",
          href: "/admin/merchant/record?uid=1000",
        },
      ],
    },
    attachTo: document.body,
  });
  mounted.push(w);
  await w.get("button").trigger("click");
  await nextTick();
  expect(
    [...document.querySelectorAll('[role="menuitem"]')].map((x) =>
      x.textContent?.trim(),
    ),
  ).toEqual(["local"]);
});
