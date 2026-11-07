# Console form browser audit — 2026-11-07

The authenticated online administrator and merchant consoles were inspected through the browser. Production forms were opened and cancelled without saving. No payments, refunds, settlements, messages or other financial operations were submitted. No containers were created.

## Coverage

| Surface | Desktop | Narrow viewport |
| --- | --- | --- |
| Administrator sidebar routes | 61 | 61 |
| Merchant sidebar routes | 29 | 29 |
| System settings categories | 17 | 17 |

The route inventory came from the authenticated sidebar links. Desktop checks used approximately 1440–1592 CSS pixels; narrow checks used 360–375 CSS pixels after accounting for scrollbars/browser scaling. This was viewport emulation, not testing on physical phones. Measurements checked rendered controls, page overflow and field boundaries; screenshots were also inspected for configuration forms, notification choices and long-text editors.

Twelve administrator editors were opened and cancelled: merchants, groups, payment channels, blacklist, authorized domains, payment methods, channel polling, WeChat applications, application channels, administrators, order subject policies and announcement HTML. No values were entered into credential fields. Disabled external-plugin actions remain deferred and are not counted as successful operation tests. Financial creation/confirmation actions were not exercised online.

## Findings and fixes

1. Merchant notification radio buttons inherited a later full-width input rule, deforming the indicators. Exclude radio and checkbox inputs from that rule and retain the 16×16 indicator size. Keep the original group names, selection bindings and disabled conditions.
2. The notification matrix squeezed availability explanations into narrow columns on phones. At widths up to 600px, render each event as a labelled two-column card with visible channel names, wrapping explanations and at least 44px choice targets. Desktop retains the table and sufficient minimum width.
3. Business SMS templates required sideways scrolling to reach the complaint and membership templates. At widths up to 600px, stack the three labelled fields under each provider. All nine existing inputs and bindings remain unchanged.
4. Settlement batch filters stretched with the entire row. Limit each field to a flexible 240px column; allow wrapping on smaller screens.

## Local validation

- New browser regression: `web/e2e/notification-form-layout.spec.ts`, 375/768/1440px and light/dark themes. It checks real rendered radio dimensions, mobile target size, explanation width, all nine SMS fields, field boundaries and settlement filter width. All API responses are local mocks; it asserts zero writes.
- The six new cases failed against the previous production build on the malformed radio dimensions, then passed against the corrected build.
- 2036 frontend unit tests passed; no failed or skipped unit tests.
- 158 browser regression tests passed, including merchant notification saves, original settlement retry semantics, drawer/dialog interaction, settings drafts, credential boundaries and policy editing, all against the loopback static fixture with mocked APIs.
- Production frontend build passed. The fixture reported zero client errors, server errors and unmocked API requests. It was stopped after the run.
- Independent source review found no blocking issue. All three modified Vue script blocks remain byte-for-byte unchanged.

The deployment changes only frontend static resources. Existing API/worker services, database and configuration are preserved. Deployment receipts and screenshots are retained locally under `.tools/live-form-review/`; they are not published with production data.
