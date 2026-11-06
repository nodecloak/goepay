import type { Route } from "@playwright/test";

// Exact anonymous metadata returned by the real205 service when its original
// merchant password switch is off. Other methods or queries remain unhandled.
export function closedLoginCaptchaFixture205(route: Route) {
  const request = route.request(),
    url = new URL(request.url());
  if (
    request.method() !== "GET" ||
    url.pathname !== "/console/v1/public/login-captcha" ||
    url.search !== "?role=merchant&mode=password"
  )
    return undefined;
  return route.fulfill({
    status: 200,
    contentType: "application/json",
    body: JSON.stringify({ code: 0, message: "", data: { enabled: false } }),
  });
}
