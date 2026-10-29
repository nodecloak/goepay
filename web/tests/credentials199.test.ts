// @vitest-environment jsdom
import { afterEach, describe, expect, it, vi } from "vitest";
import { flushPromises, mount, type VueWrapper } from "@vue/test-utils";
import { reactive } from "vue";
import AccountPage from "../src/components/AccountPage.vue";
import { ApiClient, ApiError } from "../src/api";

type RequestOptions = Parameters<ApiClient["request"]>[1];
type Write = { path: string; body: unknown };
const mounted: VueWrapper[] = [];
afterEach(() => {
  for (const w of mounted.splice(0)) w.unmount();
  document.body.innerHTML = "";
  vi.restoreAllMocks();
});

function deferred<T>() {
  let resolve!: (value: T) => void;
  let reject!: (reason: unknown) => void;
  const promise = new Promise<T>((yes, no) => {
    resolve = yes;
    reject = no;
  });
  return { promise, resolve, reject };
}

function account(
  onWrite?: (path: string, options: RequestOptions) => Promise<unknown>,
  hasPassword = true,
  area: "merchant" | "admin" = "merchant",
) {
  const client = reactive(new ApiClient(area));
  client.csrfToken = "credentials199-first-session";
  const writes: Write[] = [];
  client.request = vi.fn(async (path: string, options?: RequestOptions) => {
    if (options?.method && options.method !== "GET") {
      writes.push({ path, body: JSON.parse(JSON.stringify(options.body)) });
      return onWrite ? onWrite(path, options) : {};
    }
    if (path === "/account/profile")
      return {
        name: "本地商户199",
        email: "",
        uid: "9007199254740993",
        addtime: "2020-01-02 11:04:05",
        registration_time_known: true,
        has_pwd: hasPassword,
        pay: "1",
      };
    if (path === "/account/api-info")
      return { pid: "1000", algorithm: "MD5", api_key_masked: "old****key" };
    return {};
  }) as ApiClient["request"];
  return { client, writes };
}

// KitModal teleports to <body>; @vue/test-utils cannot query a teleported tree
// from the wrapper, and serializing it can recurse. This inline stub keeps the
// behaviours the tests assert: role="dialog" (+ aria-label=title), a <form> with
// a submit button, a 取消 button and Esc both emitting close, and focus restore
// to whatever was focused when it mounted (matching KitModal's own restore).
const KitModalStub = {
  props: ["title", "loading", "submitText", "cancelText", "danger"],
  emits: ["close", "submit"],
  mounted() {
    (this as any)._restore = document.activeElement as HTMLElement | null;
  },
  beforeUnmount() {
    (this as any)._restore?.focus?.();
  },
  template: `<div class="pa-modal" role="dialog" aria-modal="true" :aria-label="title"
      @keydown="$event.key==='Escape' && $emit('close')">
    <h2>{{ title }}</h2>
    <form @submit.prevent="$emit('submit')">
      <slot />
      <button type="button" @click="$emit('close')">{{ cancelText || '取消' }}</button>
      <button type="submit">{{ submitText || '保存' }}</button>
    </form>
  </div>`,
};

async function setup(
  client: ApiClient,
  section: "profile" | "api-info" | "security" = "profile",
) {
  const w = mount(AccountPage, {
    attachTo: document.body,
    props: { client, section, showLoginLog: true },
    global: {
      stubs: {
        MerchantEmailBinding197: true,
        MerchantBindings134: true,
        MerchantRSAKeyRotation: true,
        MerchantSignaturePolicy: true,
        KitModal: KitModalStub,
      },
    },
  });
  mounted.push(w);
  await flushPromises();
  return w;
}

function button(w: VueWrapper, name: string) {
  const found = w.findAll("button").find((b) => b.text() === name);
  expect(found, `visible button ${name}`).toBeDefined();
  return found!;
}

function passwordRow(w: VueWrapper) {
  const row = w
    .findAll("dl > div")
    .find((entry) =>
      entry.findAll("dt").some((dt) => dt.text() === "登录密码"),
    );
  expect(row, "original 登录密码 description row").toBeDefined();
  return row!;
}

async function fillPassword(w: VueWrapper) {
  const values = [
    "current-password199",
    "new-password-long199",
    "new-password-long199",
  ];
  const inputs = w.findAll<HTMLInputElement>('input[type="password"]');
  expect(inputs).toHaveLength(3);
  for (const [index, input] of inputs.entries())
    await input.setValue(values[index]);
}

describe("original merchant password row and dialog", () => {
  it("shows the real status and original modify/log links before opening the password dialog", async () => {
    const { client } = account();
    const w = await setup(client);
    const row = passwordRow(w);
    expect(row.text()).toContain("已设置");
    expect(row.findAll("button").map((b) => b.text())).toEqual([
      "修改",
      "登录日志",
    ]);
    expect(w.find('input[type="password"]').exists()).toBe(false);
    await button(w, "登录日志").trigger("click");
    expect(w.emitted("login-log")).toHaveLength(1);
    await button(w, "修改").trigger("click");
    expect(w.get('[role="dialog"]').attributes("aria-label")).toBe(
      "修改登录密码",
    );
    expect(w.findAll('input[type="password"]')).toHaveLength(3);
    expect(w.text()).toContain("9007199254740993");
    expect(w.text()).toContain("2020-01-02 11:04:05");
  });

  it("does not invent first-password initialization for a merchant without a password", async () => {
    const { client, writes } = account(undefined, false);
    const w = await setup(client);
    expect(passwordRow(w).text()).toContain("未设置");
    const modify = button(w, "修改");
    if (!modify.attributes("disabled")) await modify.trigger("click");
    expect(w.find('input[autocomplete="new-password"]').exists()).toBe(false);
    expect(w.text()).toMatch(/找回密码|管理员/);
    expect(writes).toEqual([]);
  });

  it("restores focus and clears all dialog passwords on Escape and cancellation", async () => {
    const { client } = account();
    const w = await setup(client);
    const modify = button(w, "修改");
    (modify.element as HTMLButtonElement).focus();
    await modify.trigger("click");
    await fillPassword(w);
    const dialog = w.get('[role="dialog"]');
    await dialog.trigger("keydown", { key: "Escape" });
    expect(w.find('[role="dialog"]').exists()).toBe(false);
    expect(document.activeElement).toBe(modify.element);
    await modify.trigger("click");
    expect(
      w
        .findAll<HTMLInputElement>('input[type="password"]')
        .map((i) => i.element.value),
    ).toEqual(["", "", ""]);
    await fillPassword(w);
    await button(w, "取消").trigger("click");
    await modify.trigger("click");
    expect(
      w
        .findAll<HTMLInputElement>('input[type="password"]')
        .map((i) => i.element.value),
    ).toEqual(["", "", ""]);
  });

  it("keeps a wrong-current-password response retryable and emits the genuine success message", async () => {
    let attempts = 0;
    const { client, writes } = account(async () => {
      if (++attempts === 1)
        throw new ApiError("当前密码错误，请重试", 422, 422);
      return {};
    });
    const w = await setup(client);
    await button(w, "修改").trigger("click");
    await fillPassword(w);
    await w.get('[role="dialog"] form').trigger("submit");
    await flushPromises();
    expect(w.get('[role="dialog"]').text()).toContain("当前密码错误，请重试");
    expect(w.emitted("reauthenticate")).toBeUndefined();
    await fillPassword(w);
    await w.get('[role="dialog"] form').trigger("submit");
    await flushPromises();
    expect(writes).toEqual([
      {
        path: "/account/password",
        body: {
          current_password: "current-password199",
          new_password: "new-password-long199",
        },
      },
      {
        path: "/account/password",
        body: {
          current_password: "current-password199",
          new_password: "new-password-long199",
        },
      },
    ]);
    expect(w.emitted("reauthenticate")).toEqual([["密码已修改，请重新登录"]]);
    expect(w.find('input[type="password"]').exists()).toBe(false);
  });

  it("burns a pending password result when the user closes the dialog", async () => {
    const response = deferred<unknown>();
    const { client, writes } = account(() => response.promise);
    const w = await setup(client);
    await button(w, "修改").trigger("click");
    await fillPassword(w);
    await w.get('[role="dialog"] form').trigger("submit");
    await button(w, "取消").trigger("click");
    expect(w.find('[role="dialog"]').exists()).toBe(false);
    expect(w.text()).toContain("修改可能已生效，请重新登录核对");
    response.resolve({});
    await flushPromises();
    expect(w.emitted("reauthenticate")).toBeUndefined();
    expect(writes).toHaveLength(1);
    expect(button(w, "修改").attributes("disabled")).toBeDefined();
  });

  it("does not resubmit an uncertain password change or leave its plaintext in the form", async () => {
    const { client, writes } = account(async () => {
      throw new TypeError("synthetic network interruption");
    });
    const w = await setup(client);
    await button(w, "修改").trigger("click");
    await fillPassword(w);
    await w.get('[role="dialog"] form').trigger("submit");
    await flushPromises();
    expect(w.get('[role="dialog"]').text()).toContain("不会自动重试");
    expect(
      w
        .findAll<HTMLInputElement>('input[type="password"]')
        .map((i) => i.element.value),
    ).toEqual(["", "", ""]);
    await w.get('[role="dialog"] form').trigger("submit");
    expect(writes).toHaveLength(1);
    await button(w, "取消").trigger("click");
    expect(w.text()).toContain("未能确认密码修改结果");
    expect(w.emitted("reauthenticate")).toBeUndefined();
  });

  it("requires a read-only recheck after an uncertain MD5 result and never silently retries", async () => {
    const { client, writes } = account(async () => {
      throw new TypeError("synthetic rotation interruption");
    });
    const w = await setup(client, "api-info");
    await button(w, "重置 API 密钥").trigger("click");
    await w
      .get('input[autocomplete="current-password"]')
      .setValue("synthetic-current-password199");
    await w.get('[role="dialog"] form').trigger("submit");
    await flushPromises();
    expect(w.text()).toContain("不会自动重试");
    expect(w.find('[role="dialog"]').exists()).toBe(false);
    expect(button(w, "重置 API 密钥").attributes("disabled")).toBeDefined();
    await button(w, "重新读取接入信息").trigger("click");
    await flushPromises();
    expect(writes).toHaveLength(1);
    expect(button(w, "重置 API 密钥").attributes("disabled")).toBeUndefined();
    expect(w.find("textarea[readonly]").exists()).toBe(false);
  });
});

type Change = "client" | "area" | "csrf" | "section" | "unmount";
const changes: Change[] = ["client", "area", "csrf", "section", "unmount"];
async function switchContext(
  w: VueWrapper<InstanceType<typeof AccountPage>>,
  client: ApiClient,
  change: Change,
) {
  if (change === "client") {
    const replacement = account().client;
    replacement.csrfToken = "credentials199-replacement-session";
    await w.setProps({ client: replacement });
  } else if (change === "area") {
    client.area = client.area === "merchant" ? "admin" : "merchant";
    client.area = client.area === "merchant" ? "admin" : "merchant";
  } else if (change === "csrf") {
    client.csrfToken = "credentials199-replacement-session";
  } else if (change === "section") {
    await w.setProps({
      section: w.props("section") === "profile" ? "api-info" : "profile",
    });
  } else w.unmount();
  await flushPromises();
}

describe.each(changes)(
  "credential response after %s context change",
  (change) => {
    it("never revives a prior MD5 key or prior error", async () => {
      const response = deferred<unknown>();
      const { client, writes } = account(() => response.promise);
      const w = await setup(client, "api-info");
      await button(w, "重置 API 密钥").trigger("click");
      await w
        .get('input[autocomplete="current-password"]')
        .setValue("old-rotation-password199");
      await w.get('[role="dialog"] form').trigger("submit");
      expect(writes).toHaveLength(1);
      await switchContext(w, client, change);
      response.resolve({ api_key: "prior-session-secret-MD5-199" });
      await flushPromises();
      expect(document.body.textContent).not.toContain(
        "prior-session-secret-MD5-199",
      );
      expect(w.find("textarea[readonly]").exists()).toBe(false);
      expect(w.emitted("reauthenticate")).toBeUndefined();
    });

    it("never emits a password reauthentication for the replacement context", async () => {
      const response = deferred<unknown>();
      const { client, writes } = account(() => response.promise, true, "admin");
      const w = await setup(client, "security");
      await fillPassword(w);
      await w.get("form").trigger("submit");
      expect(writes).toHaveLength(1);
      await switchContext(w, client, change);
      response.resolve({});
      await flushPromises();
      expect(w.emitted("reauthenticate")).toBeUndefined();
      expect(document.body.textContent).not.toContain("密码已修改");
    });

    it("discards a prior MD5 failure without changing the new screen", async () => {
      const response = deferred<unknown>();
      const { client } = account(() => response.promise);
      const w = await setup(client, "api-info");
      await button(w, "重置 API 密钥").trigger("click");
      await w
        .get('input[autocomplete="current-password"]')
        .setValue("old-rotation-password199");
      await w.get('[role="dialog"] form').trigger("submit");
      await switchContext(w, client, change);
      response.reject(new ApiError("prior-session-secret-error199", 422, 422));
      await flushPromises();
      expect(document.body.textContent).not.toContain(
        "prior-session-secret-error199",
      );
      expect(w.emitted("reauthenticate")).toBeUndefined();
    });

    it("discards a prior password failure without changing the new screen", async () => {
      const response = deferred<unknown>();
      const { client } = account(() => response.promise, true, "admin");
      const w = await setup(client, "security");
      await fillPassword(w);
      await w.get("form").trigger("submit");
      await switchContext(w, client, change);
      response.reject(
        new ApiError("prior-session-password-error199", 422, 422),
      );
      await flushPromises();
      expect(document.body.textContent).not.toContain(
        "prior-session-password-error199",
      );
      expect(w.emitted("reauthenticate")).toBeUndefined();
    });
  },
);
