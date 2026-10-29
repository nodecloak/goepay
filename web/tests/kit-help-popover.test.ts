import { flushPromises, mount } from "@vue/test-utils";
import { afterEach, expect, it, vi } from "vitest";
import KitHelpPopover from "../src/components/KitHelpPopover.vue";

// The 说明 panel is rendered in <body> and fixed beside its trigger, so a
// card's overflow:hidden cannot clip it and it never runs off the window.

let triggerRect = { left: 0, right: 0, top: 0, bottom: 0 };
function rect(r: typeof triggerRect) {
  return {
    ...r,
    x: r.left,
    y: r.top,
    width: r.right - r.left,
    height: r.bottom - r.top,
    toJSON: () => r,
  } as DOMRect;
}

afterEach(() => {
  vi.restoreAllMocks();
  document.body.innerHTML = "";
});

async function openAt(r: typeof triggerRect) {
  triggerRect = r;
  vi.spyOn(Element.prototype, "getBoundingClientRect").mockImplementation(
    function (this: Element) {
      return this.classList.contains("pa-help-trigger")
        ? rect(triggerRect)
        : rect({ left: 0, right: 0, top: 0, bottom: 0 });
    },
  );
  vi.spyOn(HTMLElement.prototype, "offsetWidth", "get").mockReturnValue(300);
  vi.spyOn(HTMLElement.prototype, "offsetHeight", "get").mockReturnValue(120);
  const w = mount(KitHelpPopover, {
    props: { text: "核准后转入商户余额" },
    attachTo: document.body,
  });
  await w.get(".pa-help-trigger").trigger("click");
  await flushPromises();
  const panel = document.body.querySelector<HTMLElement>(".pa-help-panel")!;
  return { w, panel };
}

it("opens in <body>, under a trigger on the left, starting at the trigger's left edge", async () => {
  const { w, panel } = await openAt({
    left: 20,
    right: 98,
    top: 40,
    bottom: 72,
  });
  expect(panel).toBeTruthy();
  expect(w.element.contains(panel)).toBe(false);
  expect(panel.textContent).toContain("核准后转入商户余额");
  expect(panel.style.left).toBe("20px");
  expect(panel.style.top).toBe("78px");
  expect(panel.style.visibility).toBe("");
  w.unmount();
});

it("ends at the trigger's right edge when there is no room to the right", async () => {
  const width = window.innerWidth;
  const { w, panel } = await openAt({
    left: width - 90,
    right: width - 12,
    top: 40,
    bottom: 72,
  });
  expect(panel.style.left).toBe(`${width - 12 - 300}px`);
  w.unmount();
});

it("opens above the trigger when there is no room below", async () => {
  const height = window.innerHeight;
  const { w, panel } = await openAt({
    left: 20,
    right: 98,
    top: height - 40,
    bottom: height - 8,
  });
  expect(panel.style.top).toBe(`${height - 40 - 6 - 120}px`);
  w.unmount();
});

it("closes on Esc and on Tab, handing focus back to the trigger", async () => {
  for (const key of ["Escape", "Tab"]) {
    const { w } = await openAt({ left: 20, right: 98, top: 40, bottom: 72 });
    document.dispatchEvent(new KeyboardEvent("keydown", { key }));
    await flushPromises();
    expect(document.body.querySelector(".pa-help-panel")).toBeNull();
    expect(document.activeElement).toBe(w.get(".pa-help-trigger").element);
    expect(w.get(".pa-help-trigger").attributes("aria-expanded")).toBe("false");
    w.unmount();
    vi.restoreAllMocks();
  }
});
