import type { Route } from "@playwright/test";
import actual from "./site191-http.json" with { type: "json" };

// Only the newly introduced exact public GET is known here. All business and
// mutation requests continue into each test's original strict handler.
export function publicSiteFixture191(route: Route): Promise<void> | undefined {
  const request = route.request(), url = new URL(request.url());
  if (request.method() !== "GET" || url.pathname !== "/console/v1/public/site" || url.search || url.href.endsWith("?")) return;
  return route.fulfill({ status: 200, contentType: "application/json", body: JSON.stringify({ code: 0, message: "", data: actual.default }) });
}
