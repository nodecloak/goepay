import {
  expect,
  type APIRequestContext,
  type Page,
  type Playwright,
} from "@playwright/test";
import { randomUUID } from "node:crypto";
import { localSandboxBase, readLocalSandboxEnv } from "./local-sandbox-env";

type Area = "admin" | "merchant";
export async function localCrudFixture(
  playwright: Playwright,
  moduleURL: string,
) {
  const baseURL = localSandboxBase();
  const env = Object.fromEntries(
    readLocalSandboxEnv(moduleURL)
      .trim()
      .split(/\r?\n/)
      .map((line) => {
        const i = line.indexOf("=");
        return [line.slice(0, i), line.slice(i + 1)];
      }),
  );
  expect(env.GOEPAY_SANDBOX).toBe("true");
  expect(env.GOEPAY_PROVIDER_MODE).toBe("simulation");
  expect(env.GOEPAY_ENABLE_PROVIDER_WRITES).toBe("false");
  const sessions = new Map<Area, { api: APIRequestContext; csrf: string }>();
  async function session(area: Area) {
    let value = sessions.get(area);
    if (!value) {
      const api = await playwright.request.newContext({ baseURL });
      const response = await api.post(`/console/v1/${area}/login`, {
        data: {
          username: area === "admin" ? "admin" : "merchant",
          password: env.GOEPAY_SEED_PASSWORD,
        },
      });
      const body = await response.json();
      expect(response.ok(), body.message).toBeTruthy();
      expect(body.code, body.message).toBe(0);
      value = { api, csrf: body.data.csrf_token };
      sessions.set(area, value);
    }
    return value;
  }
  async function request(
    area: Area,
    path: string,
    method = "GET",
    data?: unknown,
  ) {
    const { api, csrf } = await session(area);
    return api.fetch(`/console/v1/${area}${path}`, {
      method,
      data,
      headers: method === "GET" ? {} : { "X-CSRF-Token": csrf },
    });
  }
  async function call(
    area: Area,
    path: string,
    method = "GET",
    data?: unknown,
  ): Promise<any> {
    const response = await request(area, path, method, data);
    const body = await response.json();
    expect(response.ok(), `${method} ${path}: ${body.message}`).toBeTruthy();
    expect(body.code, body.message).toBe(0);
    return body.data;
  }
  async function attach(page: Page, area: Area) {
    page.setDefaultTimeout(8000);
    page.setDefaultNavigationTimeout(15000);
    const { api } = await session(area);
    await page.context().addCookies((await api.storageState()).cookies);
    await page
      .context()
      .route("**/*", (route) =>
        new URL(route.request().url()).origin === baseURL
          ? route.continue()
          : route.abort(),
      );
  }
  expect((await call("admin", "/dashboard")).sandbox).toBe(true);
  return {
    baseURL,
    env,
    call,
    request,
    session,
    attach,
    tag: "crud227-" + randomUUID().slice(0, 8),
    async dispose() {
      for (const { api } of sessions.values()) await api.dispose();
    },
  };
}
