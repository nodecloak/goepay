import type { Router } from "vue-router";

type Decision202 =
  | { kind: "stay" }
  | { kind: "block" }
  | { kind: "assign" | "replace"; href: string };

function documentKind202(path: string): string {
  if (path === "/merchant/scanpay") return "native-scan";
  if (path === "/test" || path === "/test/") return "public-testpay";
  if (path === "/merchant/login") return "merchant-login";
  return "console";
}

export function documentBoundaryDecision202(
  to: string,
  from: string,
  current: string,
  initial: boolean,
): Decision202 {
  if (initial) return { kind: "stay" };
  try {
    const address = new URL(current),
      target = new URL(to, address.origin),
      previous = new URL(from, address.origin);
    if (
      !to.startsWith("/") ||
      to.startsWith("//") ||
      /[\u0000-\u0020\u007f\\]/.test(to) ||
      target.origin !== address.origin
    )
      return { kind: "block" };
    if (documentKind202(target.pathname) === documentKind202(previous.pathname))
      return { kind: "stay" };
    return {
      kind:
        target.href === address.href ||
        documentKind202(target.pathname) === "merchant-login" ||
        documentKind202(previous.pathname) === "merchant-login"
          ? "replace"
          : "assign",
      href: target.href,
    };
  } catch {
    return { kind: "block" };
  }
}

export function installDocumentBoundary202(
  router: Pick<Router, "beforeEach">,
): void {
  router.beforeEach((to, from) => {
    const decision = documentBoundaryDecision202(
      to.fullPath,
      from.fullPath,
      window.location.href,
      from.matched.length === 0,
    );
    if (decision.kind === "stay") return true;
    if (decision.kind === "block") return false;
    window.location[decision.kind](decision.href);
    return false;
  });
}
