# Console form layout review

Reviewed admin and merchant component source for form sizing: 208 components,
including 101 with forms or selects, 78 form declarations and 132 select
declarations. Shared layout rules cover native controls and KitSelect; this is
a source inventory, not a claim that every conditional business state was
opened in a browser.

| Context | Layout |
| --- | --- |
| Standalone page select | At most 320px; fit the available width |
| Configuration page | Editing region at most 960px; direct field groups bounded |
| Numeric configuration | Short 200px controls; labels remain visible |
| Filter grid | Tracks at most 320px, wrapping as space requires |
| Existing filter bars | Retain dedicated compact search layout |
| Modal / drawer | Retain dedicated label columns and available dialog width |
| URLs, credentials, notes, rich editors | Retain space for long values |
| Referral policy cards | Two columns from 1200px, capped at 1760px; otherwise stacked |

Order and membership referral policies use the same field spacing and label
columns. Mobile field rows stack. Native selects keep their native interaction;
KitSelect retains keyboard selection and shows the full selected text on hover.
Long options wrap inside the existing scrollable menu.

No API payloads, field bindings, authorization, saving, clearing or cancellation
semantics were changed. Local browser coverage includes all four widths (375,
768, 1440, 3840) in both themes, selection, review/cancel retaining the draft,
absence of unintended writes, horizontal overflow, filter width, dialog width
and long editor space. Existing admin and merchant interaction regressions are
included. Validation: 2036 unit tests, 144 browser cases and production build.
All APIs in browser acceptance were intercepted synthetic fixtures; no new
container or production transaction was used.
