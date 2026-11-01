import { mount, flushPromises } from "@vue/test-utils";
import { afterEach, expect, it } from "vitest";
import KitModal from "../src/components/KitModal.vue";
import PluginDrawer from "../src/components/PluginDetailDrawer.vue";
import type { CatalogEntry } from "../src/plugin-catalog";

const views: ReturnType<typeof mount>[] = [];
afterEach(() => {
  for (const view of views.reverse()) view.unmount();
  views.length = 0;
  document.body.replaceChildren();
});
function opener() {
  const button = document.createElement("button");
  button.textContent = "打开详情";
  document.body.append(button);
  button.focus();
  return button;
}
function key(target: Element, name: string, shiftKey = false) {
  const event = new KeyboardEvent("keydown", {
    key: name,
    shiftKey,
    bubbles: true,
    cancelable: true,
  });
  target.dispatchEvent(event);
  return event;
}
function kit(
  props: Record<string, unknown> = {},
  slots = '<input aria-label="名称"><button hidden>隐藏操作</button><button disabled>停用操作</button>',
) {
  const view = mount(KitModal, {
    attachTo: document.body,
    props: { title: "编辑记录", ...props },
    slots: { default: slots },
  });
  views.push(view);
  return view;
}
const entry: CatalogEntry = {
  type: "payment",
  name: "test-plugin",
  title: "测试插件",
  desc: "合成插件说明",
  author: "合成作者",
  version: "1.0",
  price: "0",
  pay: [],
  trans: [],
  link: "https://example.com/plugin",
};
function plugin() {
  const view = mount(PluginDrawer, {
    attachTo: document.body,
    props: { entry, title: "插件详情" },
  });
  views.push(view);
  return view;
}

it("includes header close in the complete modal Tab loop and restores its opener", async () => {
  const trigger = opener(),
    view = kit();
  await flushPromises();
  const dialog = document.querySelector<HTMLElement>('[role="dialog"]')!,
    close = dialog.querySelector<HTMLButtonElement>('[aria-label="关闭"]')!,
    submit = dialog.querySelector<HTMLButtonElement>('[type="submit"]')!;
  expect(document.activeElement).toBe(dialog.querySelector("input"));
  submit.focus();
  expect(key(submit, "Tab").defaultPrevented).toBe(true);
  expect(document.activeElement).toBe(close);
  expect(key(close, "Tab", true).defaultPrevented).toBe(true);
  expect(document.activeElement).toBe(submit);
  view.unmount();
  views.splice(views.indexOf(view), 1);
  expect(document.activeElement).toBe(trigger);
});

it("traps the header-only modal while skipping hidden and disabled body controls", async () => {
  opener();
  kit(
    { hideCancel: true, submitDisabled: true },
    '<button hidden>不可见</button><input disabled><input type="hidden"><div style="display:none"><button>隐藏容器</button></div>',
  );
  await flushPromises();
  const dialog = document.querySelector<HTMLElement>('[role="dialog"]')!,
    close = dialog.querySelector<HTMLButtonElement>('[aria-label="关闭"]')!;
  close.focus();
  expect(key(close, "Tab").defaultPrevented).toBe(true);
  expect(document.activeElement).toBe(close);
  expect(key(close, "Tab", true).defaultPrevented).toBe(true);
  expect(document.activeElement).toBe(close);
});

it("keeps a busy modal open for Escape, backdrop, header and cancel until loading ends", async () => {
  opener();
  const view = kit({ loading: true });
  await flushPromises();
  const dialog = document.querySelector<HTMLElement>('[role="dialog"]')!,
    close = dialog.querySelector<HTMLButtonElement>('[aria-label="关闭"]')!,
    cancel = dialog.querySelector<HTMLButtonElement>(
      '.pa-modal-foot button[type="button"]',
    )!;
  key(dialog, "Escape");
  close.click();
  cancel.click();
  dialog.parentElement!.dispatchEvent(
    new Event("pointerdown", { bubbles: true }),
  );
  expect(view.emitted("close")).toBeUndefined();
  expect(close.disabled).toBe(true);
  expect(cancel.disabled).toBe(true);
  await view.setProps({ loading: false });
  key(dialog, "Escape");
  expect(view.emitted("close")).toHaveLength(1);
});

it("leaves Escape handled by an inner control with that control", async () => {
  opener();
  const view = kit();
  await flushPromises();
  const input = document.querySelector("input")!;
  input.addEventListener("keydown", (event) => event.preventDefault());
  key(input, "Escape");
  expect(view.emitted("close")).toBeUndefined();
});

it("returns panel-level focus into the Tab loop after busy controls become available", async () => {
  opener();
  const view = kit({ loading: true }, "");
  await flushPromises();
  const dialog = document.querySelector<HTMLElement>('[role="dialog"]')!;
  expect(document.activeElement).toBe(dialog);
  await view.setProps({ loading: false });
  expect(key(dialog, "Tab", true).defaultPrevented).toBe(true);
  expect(document.activeElement).toBe(dialog.querySelector('[type="submit"]'));
});

it("keeps nested modal keyboard handling in the top dialog and restores the parent trigger", async () => {
  const original = opener(),
    outer = kit();
  await flushPromises();
  const parentTrigger = document.querySelector<HTMLInputElement>(
    '[role="dialog"] input',
  )!;
  parentTrigger.focus();
  const inner = kit({ title: "第二层" });
  await flushPromises();
  const dialogs = document.querySelectorAll<HTMLElement>('[role="dialog"]');
  key(dialogs[1]!, "Escape");
  expect(inner.emitted("close")).toHaveLength(1);
  expect(outer.emitted("close")).toBeUndefined();
  inner.unmount();
  views.splice(views.indexOf(inner), 1);
  expect(document.activeElement).toBe(parentTrigger);
  outer.unmount();
  views.splice(views.indexOf(outer), 1);
  expect(document.activeElement).toBe(original);
});

it("traps plugin drawer Tab in both directions and restores the actual trigger without changing scroll state", async () => {
  const trigger = opener();
  document.body.style.overflow = "auto";
  const view = plugin();
  await flushPromises();
  const dialog = document.querySelector<HTMLElement>('[role="dialog"]')!,
    close = dialog.querySelector<HTMLButtonElement>(".drawer-head button")!,
    last = dialog.querySelector<HTMLButtonElement>(".drawer-foot button")!;
  expect(document.activeElement).toBe(dialog);
  expect(key(dialog, "Tab").defaultPrevented).toBe(true);
  expect(document.activeElement).toBe(close);
  expect(key(close, "Tab", true).defaultPrevented).toBe(true);
  expect(document.activeElement).toBe(last);
  expect(key(last, "Tab").defaultPrevented).toBe(true);
  expect(document.activeElement).toBe(close);
  key(close, "Escape");
  expect(view.emitted("close")).toHaveLength(1);
  view.unmount();
  views.splice(views.indexOf(view), 1);
  expect(document.activeElement).toBe(trigger);
  expect(document.body.style.overflow).toBe("auto");
});

it("does not close a plugin drawer under a nested modal", async () => {
  opener();
  const drawer = plugin();
  await flushPromises();
  const modal = kit({ title: "上层确认" });
  await flushPromises();
  key(document.querySelector(".pa-modal")!, "Escape");
  expect(modal.emitted("close")).toHaveLength(1);
  expect(drawer.emitted("close")).toBeUndefined();
});
