// Test-only deployment-CSP mode for native-scan202-http-browser.mjs.
// All application assets are a fresh Vite production build in memory. Official
// SDK URLs are still fulfilled by the parent bridge; no vendor network is used.
import { build } from "vite";
import { expect } from "@playwright/test";
import { createHash } from "node:crypto";
import fs from "node:fs/promises";
import path from "node:path";

const sdkSources = [
  "https://res.wx.qq.com/open/js/jweixin-1.6.3.js",
  "https://gw.alipayobjects.com/as/g/h5-lib/alipayjsapi/3.1.1/alipayjsapi.min.js",
];
const rejectedSources = [
  "https://res.wx.qq.com/open/js/unapproved-native202.js",
  "https://gw.alipayobjects.com/as/g/h5-lib/alipayjsapi/3.1.1/unapproved-native202.js",
  "https://unapproved-script202.example.invalid/probe.js",
];

// A deliberately small nginx syntax reader. It reads the server's actual header
// and literal/map values, rather than copying the desired policy into the test.
// Unknown variables or unsupported map expressions fail the harness explicitly.
function nginxStatements(source) {
  const tokens = [];
  for (let i = 0; i < source.length;) {
    if (/\s/.test(source[i])) {
      i++;
      continue;
    }
    if (source[i] === "#") {
      while (i < source.length && source[i] !== "\n") i++;
      continue;
    }
    if ("{};".includes(source[i])) {
      tokens.push(source[i++]);
      continue;
    }
    let token = "";
    if (source[i] === '"' || source[i] === "'") {
      const quote = source[i++];
      while (i < source.length && source[i] !== quote) {
        if (source[i] === "\\" && /[\\"'$]/.test(source[i + 1] || "")) i++;
        token += source[i++];
      }
      if (source[i++] !== quote) throw Error("Unclosed nginx CSP token");
    } else {
      while (i < source.length && !/[\s{};]/.test(source[i]))
        token += source[i++];
    }
    tokens.push(token);
  }
  let at = 0;
  function statements(nested = false) {
    const output = [];
    while (at < tokens.length) {
      if (tokens[at] === "}") {
        if (!nested) throw Error("Unexpected nginx closing block");
        at++;
        return output;
      }
      const words = [];
      while (at < tokens.length && ![";", "{", "}"].includes(tokens[at]))
        words.push(tokens[at++]);
      const end = tokens[at++];
      if (!words.length || (end !== ";" && end !== "{"))
        throw Error("Unsupported nginx CSP statement");
      output.push({ words, children: end === "{" ? statements(true) : [] });
    }
    if (nested) throw Error("Unclosed nginx CSP block");
    return output;
  }
  return statements();
}

function deploymentPolicy(source, requestURI) {
  const statements = nginxStatements(source);
  const servers = statements.filter((node) => node.words[0] === "server");
  if (servers.length !== 1) throw Error("Expected one deployment nginx server");
  const headers = servers[0].children.filter(
    (node) =>
      node.words[0] === "add_header" &&
      node.words[1]?.toLowerCase() === "content-security-policy",
  );
  if (headers.length !== 1 || headers[0].words[3] !== "always")
    throw Error("Expected one always-on deployment CSP header");
  const resolving = new Set();
  function expand(value) {
    return value.replace(/\$[a-zA-Z_][\w]*/g, (variable) => {
      if (variable === "$request_uri") return requestURI;
      // try_files internally redirects the SPA document to /index.html. A map
      // keyed on $uri must not accidentally pass by using the pre-redirect URL.
      if (variable === "$uri") return "/index.html";
      if (resolving.has(variable)) throw Error("Cyclic nginx CSP variable");
      const maps = statements.filter(
        (node) => node.words[0] === "map" && node.words[2] === variable,
      );
      if (
        maps.length !== 1 ||
        !["$request_uri", "$uri"].includes(maps[0].words[1])
      )
        throw Error("Unsupported nginx CSP variable: " + variable);
      resolving.add(variable);
      const input = expand(maps[0].words[1]);
      let fallback;
      let match;
      for (const node of maps[0].children) {
        if (node.children.length || node.words.length !== 2)
          throw Error("Unsupported nginx CSP map value");
        const [key, mapped] = node.words;
        if (key === "default") fallback = mapped;
        else if (key.startsWith("~")) {
          const insensitive = key.startsWith("~*");
          const pattern = key.slice(insensitive ? 2 : 1);
          if (
            match === undefined &&
            new RegExp(pattern, insensitive ? "i" : "").test(input)
          )
            match = mapped;
        } else if (key === input) {
          // Exact nginx map entries have precedence over regular expressions.
          match = mapped;
          break;
        }
      }
      if (match === undefined && fallback === undefined)
        throw Error("Missing nginx CSP map default");
      const result = expand(match ?? fallback);
      resolving.delete(variable);
      return result;
    });
  }
  const policy = expand(headers[0].words[2]);
  if (!policy || /[\r\n]/.test(policy))
    throw Error("Invalid deployment CSP header");
  return policy;
}

export async function createNativeScanDeploymentCSP202({
  web,
  origin,
  artifacts,
  kind,
}) {
  const source = await fs.readFile(path.join(web, "nginx.conf"), "utf8");
  const built = await build({
    root: web,
    configFile: path.join(web, "vite.config.ts"),
    logLevel: "silent",
    build: { write: false },
  });
  const assets = new Map();
  for (const output of Array.isArray(built) ? built : [built]) {
    if (!Array.isArray(output.output))
      throw Error("Expected an in-memory production build");
    for (const asset of output.output) {
      const body = asset.type === "chunk" ? asset.code : asset.source;
      assets.set(
        "/" + asset.fileName,
        typeof body === "string" ? body : Buffer.from(body),
      );
    }
  }
  if (!assets.has("/index.html")) throw Error("Production index.html missing");
  const probes = [];
  const pages = [];
  let probeRequests = 0;
  const policies = {};
  const mime = (url) =>
    url.endsWith(".css")
      ? "text/css"
      : url.endsWith(".js")
        ? "application/javascript"
        : "application/octet-stream";

  async function violations(page) {
    return page
      .evaluate(() => window.__nativeCSP202?.violations ?? [])
      .catch(() => []);
  }
  async function blocked(page, sources, scope) {
    const before = probeRequests;
    const results = await page.evaluate(async (urls) => {
      return Promise.all(
        urls.map(
          (url) =>
            new Promise((resolve) => {
              const script = document.createElement("script");
              script.src = url;
              script.onload = () => {
                script.remove();
                resolve({ url, loaded: true });
              };
              script.onerror = () => {
                script.remove();
                resolve({ url, loaded: false });
              };
              document.head.appendChild(script);
            }),
        ),
      );
    }, sources);
    await expect
      .poll(
        async () =>
          (await violations(page)).filter(
            (value) =>
              value.effectiveDirective.startsWith("script-src") &&
              sources.some(
                (url) =>
                  value.blockedURI === url ||
                  value.blockedURI === new URL(url).origin,
              ),
          ).length,
      )
      .toBeGreaterThanOrEqual(sources.length);
    expect(results, "CSP must reject all unauthorized script elements").toEqual(
      sources.map((url) => ({ url, loaded: false })),
    );
    expect(
      probeRequests,
      "CSP rejection must occur before any request interception",
    ).toBe(before);
    probes.push({
      scope,
      results,
      violations: await violations(page),
      browser_csp_enforced: true,
    });
  }

  return {
    async observe(context) {
      await context.addInitScript(() => {
        window.__nativeCSP202 = { violations: [] };
        document.addEventListener("securitypolicyviolation", (event) => {
          window.__nativeCSP202.violations.push({
            blockedURI: event.blockedURI,
            effectiveDirective: event.effectiveDirective,
            disposition: event.disposition,
          });
        });
      });
    },
    track(page, counters) {
      pages.push({ page, counters });
    },
    async checkpoint(page) {
      return (await violations(page)).length;
    },
    async expectSDK(page, sdkURL, checkpoint) {
      const state = () =>
        page.evaluate(
          ({ url, checkpoint }) => {
            if (window.__nativeBridge202) return "loaded";
            if (
              window.__nativeCSP202.violations
                .slice(checkpoint)
                .some(
                  (value) =>
                    value.effectiveDirective.startsWith("script-src") &&
                    (value.blockedURI === url ||
                      value.blockedURI === new URL(url).origin),
                )
            )
              return "blocked";
            return "pending";
          },
          { url: sdkURL, checkpoint },
        );
      await expect.poll(state).not.toBe("pending");
      expect(
        await state(),
        "Deployment CSP must permit the exact native202 SDK on the cashier document",
      ).toBe("loaded");
    },
    async route(route, api) {
      const request = route.request();
      const target = new URL(request.url());
      if (rejectedSources.includes(request.url())) {
        probeRequests++;
        // A permissive CSP must fail the test even though external network is
        // prohibited: allow this controlled script to execute if Chromium asks.
        await route.fulfill({
          status: 200,
          contentType: "application/javascript",
          body: "window.__unauthorizedNativeCSP202 = true;",
        });
        return true;
      }
      if (target.origin !== origin) return false;
      if (
        target.pathname.startsWith("/console/") ||
        target.pathname.startsWith("/static/img/")
      ) {
        const upstream = await route.fetch({
          url: api + target.pathname + target.search,
          headers: await request.allHeaders(),
          maxRedirects: 0,
        });
        await route.fulfill({ response: upstream });
      } else if (request.isNavigationRequest()) {
        const policy = deploymentPolicy(
          source,
          target.pathname + target.search,
        );
        policies[target.pathname + target.search] = policy;
        await route.fulfill({
          status: 200,
          contentType: "text/html",
          headers: {
            "Content-Security-Policy": policy,
            "X-Content-Type-Options": "nosniff",
          },
          body: assets.get("/index.html"),
        });
      } else if (assets.has(target.pathname)) {
        await route.fulfill({
          status: 200,
          contentType: mime(target.pathname),
          body: assets.get(target.pathname),
        });
      } else {
        await route.fulfill({
          status: 404,
          body: "Synthetic production asset not found",
        });
      }
      return true;
    },
    async checkScope(context, page) {
      await blocked(
        page,
        rejectedSources,
        "exact-cashier-page-rejects-other-paths-and-hosts",
      );
      const ordinary = await context.newPage();
      try {
        // The same production shell on an ordinary merchant document must keep
        // the original strict policy, including when a query string is present.
        for (const pathname of [
          "/merchant/orders?csp202=scope",
          "/merchant/scanpay-other",
          "/merchant/scanpay/nested",
        ]) {
          await ordinary.goto(origin + pathname);
          await blocked(
            ordinary,
            [...sdkSources, ...rejectedSources],
            "non-cashier-document-rejects-sdk-exceptions:" + pathname,
          );
        }
      } finally {
        await ordinary.close();
      }
    },
    async checkTransitions(context) {
      const page = await context.newPage();
      const navigations = [];
      page.on("request", (request) => {
        if (request.isNavigationRequest())
          navigations.push(new URL(request.url()).pathname);
      });
      const entry = {
        scope: "real-cashier-entry-and-return-create-new-csp-documents",
        navigations,
      };
      probes.push(entry);
      try {
        await page.goto(origin + "/merchant/onecode");
        await page
          .getByRole("link", { name: "付款码收款", exact: true })
          .click();
        await expect(
          page.getByRole("heading", { name: "付款码收款", exact: true }),
        ).toBeVisible();
        await expect
          .poll(
            () =>
              navigations.filter((value) => value === "/merchant/scanpay")
                .length,
            {
              message:
                "Entering the cashier through its real shortcut must load a new document and CSP header",
            },
          )
          .toBe(1);
        const sdkURL = sdkSources[kind === "wechat" ? 0 : 1];
        const loaded = await page.evaluate(
          (url) =>
            new Promise((resolve) => {
              const script = document.createElement("script");
              script.src = url;
              script.onload = () => {
                script.remove();
                resolve(true);
              };
              script.onerror = () => {
                script.remove();
                resolve(false);
              };
              document.head.appendChild(script);
            }),
          sdkURL,
        );
        expect(
          loaded,
          "The cashier document reached through the actual shortcut must permit its exact SDK",
        ).toBe(true);
        const returnsBefore = navigations.filter(
          (value) => value === "/merchant/onecode",
        ).length;
        await page
          .getByRole("link", { name: "返回聚合收款", exact: false })
          .click();
        await expect
          .poll(
            () =>
              navigations.filter((value) => value === "/merchant/onecode")
                .length - returnsBefore,
            {
              message:
                "Leaving the cashier through its real return link must restore a strict document CSP",
            },
          )
          .toBe(1);
        await blocked(
          page,
          sdkSources,
          "real-return-document-rejects-native-sdk-exceptions",
        );
        entry.entered_with_sdk_permission = true;
        entry.returned_with_strict_policy = true;
      } catch (error) {
        await page
          .screenshot({
            path: path.join(artifacts, "csp-document-boundary.png"),
            fullPage: true,
          })
          .catch(() => {});
        throw error;
      } finally {
        await page.close();
      }
    },
    async write(error) {
      await fs.mkdir(artifacts, { recursive: true });
      const browser = [];
      for (const tracked of pages) {
        const { page, counters } = tracked;
        const entry = {
          ...counters(),
          violations: page.isClosed()
            ? (tracked.savedViolations ?? [])
            : await violations(page),
        };
        tracked.savedViolations = entry.violations;
        browser.push(entry);
        if (!page.isClosed())
          await page
            .screenshot({
              path: path.join(artifacts, "csp-" + entry.width + ".png"),
              fullPage: true,
            })
            .catch(() => {});
      }
      await fs.writeFile(
        path.join(artifacts, "csp-result.json"),
        JSON.stringify(
          {
            kind,
            policy_source: "web/nginx.conf",
            policy_source_sha256: createHash("sha256")
              .update(source)
              .digest("hex"),
            production_assets: true,
            document_headers: policies,
            synthetic_sdk_bodies: true,
            vendor_network: false,
            browser,
            probes,
            unapproved_script_requests: probeRequests,
            passed: !error,
            error: error ? String(error.message ?? error) : undefined,
          },
          null,
          2,
        ) + "\n",
      );
    },
  };
}
