---
phase: 4
slug: query-api-analyst-dashboard
status: approved
reviewed_at: "2026-10-21T16:13:59Z"
shadcn_initialized: false
preset: none
created: 2026-10-21
---

# Phase 4 — UI Design Contract

## Scope and defaults

Sources: spec§9; AUTH/API/LIVE/UI requirements; `04-RESEARCH.md`. No user-locked visual preferences or existing design system. Under explicit `--auto`, choose the following restrained defaults; these are engineering defaults, not a claimed user review. Four authenticated analyst pages and dev sign-in, not a marketing site. System/consumer-lag integration belongs to Phase5.

UI/UX Pro Max search `security analyst dashboard` and one narrower retry `internal analytics dashboard` returned suitable minimal/data-dense styles but off-topic marketing conversion layouts. Reject those layout recommendations. Console layout below is a documented fallback from product requirements and built-in accessibility guidance. Targeted `keyboard focus modal` returned visible-focus/unobscured-focus guidance; React stack query returned controlled-form guidance.

## Design System

| Property | Value |
|---|---|
| Tool / preset | none / not applicable |
| Components | native HTML, React, local CSS; no registry/library installation |
| Icons | local simple SVG with labels, decorative aria-hidden; no emoji icons |
| Font | system-ui, -apple-system, BlinkMacSystemFont, Segoe UI, sans-serif; monospace for identifiers/JSON |
| Motion | opacity/color150ms only, disabled by prefers-reduced-motion; no data entrance animation |

shadcn gate: no components.json exists. Auto default declines initialization/preset acquisition; coherent native components avoid adding a design stack solely for planning. No remote fonts, image assets, CDN or third-party registry blocks.

## Layout and navigation

Desktop>=1024px: 224px sidebar,64px header, content max1440px with24px padding. Sidebar text links Overview `/`, Alerts `/alerts`, Events `/events`, Rules `/rules`, current link aria-current=page. Header shows verified tenant/role, stream status and Sign out; never a credential. No System link until Phase5. Page title plus concise description, toolbar, results; no unnecessary dashboard tiles.

At<1024px, use a compact top nav that wraps without covering content; at375px single-column panels/filter form. No page-level horizontal scrolling; wide tables may use one labelled keyboard-scrollable region with sticky row headings and an accessible mobile list alternative. Touch controls44px minimum;8px gap. Header/footer cannot obscure focused controls. Browser history, refresh deep links, back/forward and drawer state work.

Overview: real event-rate/open-alert cards, severity counts,24h trend, top rules. Open-alert count is the primary visual anchor: first position,28px value, adjacent severity breakdown; event rate and trend are subordinate. Trend SVG has title/description and adjacent tabular values. No fake historical data, rounded invented counts or decorative chart points. Consumer lag panel says “Consumer lag — available in Phase 5”, never0/healthy.

Alerts: connection indicator, severity/status/rule/time/search filters, result count/page controls, table with created time, severity text, rule, summary, status and details button. Live arrivals show “New alerts available” + “Refresh alerts”; retain active filters/page until action, never append unbounded items. Detail dialog/drawer contains immutable detection, version/status, rule explanation, retained/missing evidence and history. Use native dialog.showModal(), visible close, Escape, initial heading/close focus, focus trap and trigger restoration. Phone uses full-width dialog.

Events: labelled UTC time range/IP/type/outcome form, results and event JSON detail. Render JSON as escaped React text in pre/code with wrap/scroll; malicious HTML stays text. Rules: read-only cards/table showing name, description, threshold/window, version and explicit Enabled/Disabled text.

## Spacing Scale

| Token | Value | Usage |
|---|---|---|
| xs | 4px | icon/text gap |
| sm | 8px | compact controls |
| md | 16px | card interior/form rows |
| lg | 24px | desktop page padding |
| xl | 32px | section gaps |
| 2xl | 48px | large section break |
| 3xl | 64px | header height |

All spacing multiples4. Exceptions:1px borders,2px focus outline with2px offset (not spacing tokens). Interactive height44px, not smaller dense targets. Table cell vertical padding8px.

## Typography

Exactly four sizes and two weights (400/600); no other weights/sizes in components.

| Role | Size | Weight | Line height |
|---|---|---|---|
| Label / compact metadata | 14px | 400 or600 | 1.5 |
| Body / input / JSON | 16px | 400 | 1.5 |
| Section heading | 20px | 600 | 1.2 |
| Page title / metric value | 28px | 600 | 1.2 |

## Color

Light, high-contrast console by default; no theme toggle scope. Semantic CSS vars, no arbitrary per-component colors.

| Role | Value | Usage |
|---|---|---|
| Dominant60% | #F8FAFC | canvas |
| Secondary30% | #FFFFFF | nav/cards/forms |
| Accent<=10% | #1D4ED8 | primary CTA fill, selected nav marker, focus ring, explicit links |
| Text | #0F172A | body/headings |
| Muted text | #475569 | timestamps/descriptions |
| Border | #CBD5E1 | decorative separators; not sole control boundary |
| Control boundary | #64748B | input outline, visible on white |
| Destructive | #B91C1C | destructive actions/errors only |
| Warning | #92400E on #FEF3C7 | high/medium severity and reconnect warning with text |
| Success | #166534 on #DCFCE7 | connected/resolved text badges |
| Critical | #991B1B on #FEE2E2 | critical severity text badge |

White text on accent/destructive buttons. Severity always text+shape, never color alone. Low/disabled muted badges #475569 on #F1F5F9. Critical is a separate semantic role, not destructive action color reuse. Contrast target normal text>=4.5:1, control/focus>=3:1; automated computed checks required, not assumed certification.

## Copywriting Contract

| Element | Exact copy |
|---|---|
| Sign-in title / CTA | “Sign in to Sentinel” / “Sign in” |
| Token label / help | “Development access token” / “Generate a short-lived token with the local development CLI. Tokens stay in memory.” |
| Primary investigation CTA | “Apply filters” |
| Triage CTA | “Acknowledge alert” / “Resolve alert” |
| Sign-out | “Sign out” |
| Loading | “Loading alerts…” / “Loading events…” / “Loading overview…” / “Loading rules…” |
| Alerts empty | “No alerts match these filters” / “Broaden the time range or clear filters to search again.” |
| Events empty | “No events match these filters” / “Broaden the time range or clear filters to search again.” |
| Overview empty | “No activity in this period” / “Send synthetic events with the documented seed command, then refresh.” |
| Rules empty | “No active rules are available” / “Check the configured rule file and restart the service.” |
| Filter reset | “Clear filters” |
| Read error | “Could not load alerts. Try again.” (substitute events/overview/rules) + “Retry loading alerts” (substitute events/overview/rules) |
| Validation | “Choose a valid UTC time range of at most 7 days.” / “Enter a valid IP address.” |
| Connected / connecting | “Live” / “Connecting…” |
| Disconnected | “Reconnecting… Missed alerts will be recovered.” |
| New arrivals | “New alerts available” / “Refresh alerts” |
| Expired | “Your session expired. Sign in again to continue.” |
| Invalid token | “This access token could not be verified. Check it or generate a new token.” |
| Viewer | “Read-only access. An analyst or admin can change alert status.” |
| Stale conflict | “This alert changed. Review its latest status before trying again.” |
| Evidence expired | “Some evidence is no longer retained.” |
| Historic rule unavailable | “This rule version is unavailable. The detection retains its recorded rule ID and version.” |
| Resolve confirmation | title “Resolve alert?”; body “This closes the alert. Reopening is not supported in v1.”; buttons “Return to alert” / “Resolve alert” |
| Navigation / pagination | “Previous page” / “Next page” / “Close alert details” / “Close event details” |

Acknowledgement is a forward workflow action, not deletion; no extra confirmation. Resolve has reversible-cancel confirmation before the server transition; while pending disable duplicate submission, announce result via role=status. No deleting events/rules/config exists in this phase.

## Interaction, security and states

All data areas implement loading, empty, error+retry and populated states independently. Filters have visible labels/help/field errors,300ms search debounce and cancel superseded requests; explicit Apply controls all other filters. Default50/max200 page, finite previous-cursor history<=20pages/session; reset on filter/session change. No infinite scroll. Never clear existing data to fabricated zero on fetch errors.

JWT held only in memory, password-type token input cleared after bootstrap, no localStorage/sessionStorage/query URLs. Identity is displayed only after server `/session` verification. Abort outstanding fetch/SSE and clear QueryClient/cursor history on signout,expiry or identity change; query keys include verified session generation. Viewer controls hidden, server403 separately tested.401 stops retries and returns sign-in;403 readable explanation;409 refetches without repeating PATCH automatically. Dependency503 shows retry; no raw SQL/internal exception text.

Keyboard: skip link, landmarks, semantic headings/tables, buttons not clickable divs, accessible names, visible focus. Status live region polite; severity feed must not steal focus or announce every row. Native dialog handles Escape/focus restore. Charts have equivalent tables. Test375/768/1024/1440px,200% zoom,reduced motion,keyboard-only. Safe wrapping for long IDs,IPv6,principals and JSON.

## Registry Safety

| Registry | Blocks used | Safety gate |
|---|---|---|
| none | none | Not applicable: no downloaded blocks or shadcn installation |

## Checker Sign-Off

- [x] Dimension1 Copywriting — PASS
- [x] Dimension2 Visuals — PASS
- [x] Dimension3 Color — PASS
- [x] Dimension4 Typography — PASS
- [x] Dimension5 Spacing — PASS
- [x] Dimension6 Registry Safety — PASS

Approval: independent gsd-ui-checker PASS6/6 after revision1,2026-10-21. See04-UI-CHECK.md. This document specifies design, not implemented accessibility or screenshot acceptance.
