// Real-browser Phase 4 acceptance. Every read uses identifiers produced by the
// actual ingest -> detector -> writers -> query pipeline for the current run;
// routes are intercepted only to inject faults (delay/503), never to supply data.
import { createHmac } from "node:crypto";
import { execFileSync } from "node:child_process";
import fs from "node:fs";
import path from "node:path";
import { expect, test } from "@playwright/test";
import type { APIRequestContext, Page } from "@playwright/test";

type Tenant = {
  name: string;
  alertIds: string[];
  xssEventId: string;
};
type Fixture = {
  runId: string;
  tenants: { a: Tenant; b: Tenant };
  replicas: Record<string, string>;
  seeder: { python: string; script: string };
};

function need(name: string): string {
  const value = process.env[name];
  if (!value)
    throw new Error(
      `${name} is required (run scripts/check-phase4-browser.sh)`,
    );
  return value;
}

const fixture: Fixture = JSON.parse(
  fs.readFileSync(need("PHASE4_FIXTURE"), "utf8"),
);
const secret = need("SENTINEL_AUTH_JWT_SECRET");
const artifacts = need("PHASE4_ARTIFACT_DIR");
const negativeOrigin = need("PHASE4_NEGATIVE_URL");
const sorted = (values: string[]) => [...values].sort();

function mint(tenant: string, role: string, lifetimeSeconds = 880): string {
  const encode = (value: object) =>
    Buffer.from(JSON.stringify(value)).toString("base64url");
  const now = Math.floor(Date.now() / 1000);
  const unsigned = `${encode({ alg: "HS256", typ: "JWT" })}.${encode({
    iss: "sentinel-dev",
    aud: "sentinel-dashboard",
    sub: `e2e-${role}`,
    tenant,
    role,
    iat: now - 5,
    nbf: now - 5,
    exp: now + lifetimeSeconds,
  })}`;
  return `${unsigned}.${createHmac("sha256", secret).update(unsigned).digest("base64url")}`;
}

async function signIn(page: Page, token: string) {
  await page
    .getByRole("heading", { name: "Sign in to Sentinel", exact: true })
    .waitFor();
  await page.getByLabel("Development access token").fill(token);
  await page.getByRole("button", { name: "Sign in", exact: true }).click();
  await expect(
    page.getByRole("button", { name: "Sign out", exact: true }),
  ).toBeVisible();
}

// Built-in windows end after the newest event, so some real alerts carry a
// created_at slightly ahead of wall clock. Deep links therefore pin a wide,
// explicit UTC range instead of relying on the default "up to now" window.
function alertsUrl(extra: Record<string, string> = {}) {
  const query = new URLSearchParams({
    from: new Date(Date.now() - 24 * 3600_000).toISOString(),
    to: new Date(Date.now() + 2 * 3600_000).toISOString(),
    ...extra,
  });
  return `/alerts?${query.toString()}`;
}

async function shot(page: Page, name: string) {
  const dir = path.join(artifacts, "screens");
  fs.mkdirSync(dir, { recursive: true, mode: 0o700 });
  await page.screenshot({ path: path.join(dir, `${name}.png`) });
}

// Collect the alert identifiers of every list response this page receives.
function watchAlertLists(page: Page) {
  const state = { seen: new Set<string>(), latest: [] as string[] };
  page.on("response", async (response) => {
    const url = new URL(response.url());
    if (url.pathname !== "/api/v1/alerts" || response.status() !== 200) return;
    const body = (await response.json()) as { items: { alert_id: string }[] };
    state.latest = body.items.map((item) => item.alert_id);
    for (const id of state.latest) state.seen.add(id);
  });
  return state;
}

async function firstFrame(
  origin: string,
  token: string,
  milliseconds: number,
): Promise<boolean> {
  const controller = new AbortController();
  const timer = setTimeout(() => controller.abort(), milliseconds);
  try {
    const response = await fetch(`${origin}/api/v1/alerts/stream`, {
      headers: { Authorization: `Bearer ${token}`, "Last-Event-ID": "0" },
      signal: controller.signal,
    });
    if (response.status !== 200 || !response.body)
      throw new Error(`stream status ${response.status}`);
    const first = await response.body.getReader().read();
    return new TextDecoder().decode(first.value).includes(": connected");
  } catch (failure) {
    if (controller.signal.aborted) return false;
    throw failure;
  } finally {
    clearTimeout(timer);
    controller.abort();
  }
}

const readerA = () => mint(fixture.tenants.a.name, "viewer");
const analystA = () => mint(fixture.tenants.a.name, "analyst");
const adminA = () => mint(fixture.tenants.a.name, "admin");
const analystB = () => mint(fixture.tenants.b.name, "analyst");

async function api(
  request: APIRequestContext,
  token: string,
  pathname: string,
) {
  const response = await request.get(pathname, {
    headers: { Authorization: `Bearer ${token}` },
  });
  return { status: response.status(), body: (await response.json()) as never };
}

test.describe.configure({ mode: "serial" });

test("proxy forwards the first SSE frame within five seconds and a buffering fixture does not", async ({
  baseURL,
}) => {
  // A tenant with no alerts: the stream sends only its ": connected" comment, so a
  // buffering hop cannot be masked by a replay large enough to fill its buffer.
  const token = mint(`p4e2e-${fixture.runId}-probe`, "viewer");
  expect(
    await firstFrame(baseURL!, token, 5000),
    "pinned positive proxy config",
  ).toBe(true);
  expect(
    await firstFrame(negativeOrigin, token, 5000),
    "negative control: the buffering proxy must miss the 5s first-frame deadline",
  ).toBe(false);
});

test("proxy passes bearer auth, drops spoofed identity headers and adds no CORS grant", async ({
  request,
}) => {
  const token = analystA();
  const response = await request.get("/api/v1/session", {
    headers: {
      Authorization: `Bearer ${token}`,
      "X-Tenant-ID": fixture.tenants.b.name,
      "X-Forwarded-For": "203.0.113.77",
      Origin: "https://evil.example",
    },
  });
  expect(response.status()).toBe(200);
  const session = (await response.json()) as {
    tenant_id: string;
    role: string;
  };
  expect(session.tenant_id).toBe(fixture.tenants.a.name);
  expect(session.role).toBe("analyst");
  expect(response.headers()["access-control-allow-origin"]).toBeUndefined();
  expect(response.headers()["cache-control"]).toContain("no-store");
  expect((await request.get("/api/v1/session")).status()).toBe(401);
});

test("two tenants each see exactly their own actual detections on all four pages", async ({
  browser,
}) => {
  for (const [mine, theirs, token] of [
    [fixture.tenants.a, fixture.tenants.b, analystA()],
    [fixture.tenants.b, fixture.tenants.a, analystB()],
  ] as const) {
    const context = await browser.newContext({
      viewport: { width: 1440, height: 1000 },
    });
    const page = await context.newPage();
    const lists = watchAlertLists(page);
    await page.goto(alertsUrl());
    await signIn(page, token);
    await expect(page.getByText(`${mine.name} · analyst`)).toBeVisible();
    await expect(
      page.getByRole("status").filter({ hasText: /alerts on this page/ }),
    ).toBeVisible();
    await expect
      .poll(() => sorted(lists.latest))
      .toEqual(sorted(mine.alertIds));
    for (const id of theirs.alertIds) expect(lists.seen.has(id)).toBe(false);
    await expect(page.getByText(theirs.name)).toHaveCount(0);
    // Overview: open alerts are the tenant's real count, never a global one.
    await page.getByRole("link", { name: "Overview", exact: true }).click();
    await expect(page.getByLabel("Open alerts")).toHaveText(
      String(mine.alertIds.length),
    );
    await page.getByRole("link", { name: "Rules", exact: true }).click();
    await expect(
      page.getByRole("heading", { name: "Rules", exact: true }),
    ).toBeVisible();
    for (const rule of [
      "AUTH_BRUTE_FORCE",
      "PORT_SCAN",
      "REQUEST_FLOOD",
      "DENY_SPIKE",
    ])
      await expect(
        page
          .getByRole("article")
          .filter({ hasText: rule })
          .or(page.locator(`#rule-${rule}`)),
      ).not.toHaveCount(0);
    await page.getByRole("link", { name: "Events", exact: true }).click();
    await expect(
      page.getByRole("heading", { name: "Events", exact: true }),
    ).toBeVisible();
    await expect(
      page.getByRole("status").filter({ hasText: /events on this page/ }),
    ).toBeVisible();
    await context.close();
  }
});

test("alert detail shows real evidence and refuses another tenant's alert", async ({
  browser,
  request,
}) => {
  const mine = fixture.tenants.a;
  const id = mine.alertIds[0];
  const context = await browser.newContext({
    viewport: { width: 1440, height: 1000 },
  });
  const page = await context.newPage();
  await page.goto(alertsUrl({ alert_id: id }));
  await signIn(page, analystA());
  const dialog = page.getByRole("dialog", {
    name: "Alert details",
    exact: true,
  });
  await expect(dialog.getByText("Recorded rule explanation")).toBeVisible();
  const expected = await api(request, analystA(), `/api/v1/alerts/${id}`);
  expect(expected.status).toBe(200);
  const detail = expected.body as {
    evidence: { event_id: string }[];
    detection: { alert_id: string };
  };
  expect(detail.detection.alert_id).toBe(id);
  expect(detail.evidence.length).toBeGreaterThan(0);
  for (const event of detail.evidence)
    await expect(dialog.getByText(event.event_id).first()).toBeVisible();
  await shot(page, "alert-detail-1440");
  await context.close();

  const foreign = await browser.newContext();
  const other = await foreign.newPage();
  await other.goto(alertsUrl({ alert_id: id }));
  await signIn(other, analystB());
  await expect(
    other
      .getByRole("dialog", { name: "Alert details", exact: true })
      .getByText("This alert was not found for your tenant."),
  ).toBeVisible();
  await expect(other.getByText(id, { exact: false })).toHaveCount(0);
  await foreign.close();
});

test("event JSON injection is rendered inert and matches the API", async ({
  browser,
  request,
}) => {
  const mine = fixture.tenants.a;
  const expected = await api(
    request,
    analystA(),
    `/api/v1/events/${mine.xssEventId}`,
  );
  expect(expected.status).toBe(200);
  const context = await browser.newContext({
    viewport: { width: 1440, height: 1000 },
  });
  const page = await context.newPage();
  await page.goto(`/events?limit=200&event_id=${mine.xssEventId}`);
  await signIn(page, analystA());
  const dialog = page.getByRole("dialog", {
    name: "Event details",
    exact: true,
  });
  await dialog.locator("pre code").waitFor();
  expect(
    JSON.parse((await dialog.locator("pre code").textContent()) ?? ""),
  ).toEqual(expected.body);
  expect(await dialog.locator("img,script").count()).toBe(0);
  expect(
    await page.evaluate(
      () => (globalThis as Record<string, unknown>).__eventXSS,
    ),
  ).toBeUndefined();
  await page.keyboard.press("Escape");
  await expect(dialog).toHaveCount(0);
  await context.close();

  const foreign = await browser.newContext();
  const other = await foreign.newPage();
  await other.goto(`/events?limit=200&event_id=${mine.xssEventId}`);
  await signIn(other, analystB());
  await expect(
    other
      .getByRole("dialog", { name: "Event details", exact: true })
      .getByRole("alert"),
  ).toBeVisible();
  await expect(other.getByRole("dialog").locator("pre code")).toHaveCount(0);
  await foreign.close();
});

test("viewers cannot mutate; analysts acknowledge and resolve with stale conflicts and focus handling", async ({
  browser,
  request,
}) => {
  const id = fixture.tenants.a.alertIds[1];
  // Server-side role enforcement is independent of the UI.
  const denied = await request.patch(`/api/v1/alerts/${id}`, {
    headers: { Authorization: `Bearer ${readerA()}` },
    data: { status: "acknowledged", version: 1 },
  });
  expect(denied.status()).toBe(403);
  const hidden = await request.patch(`/api/v1/alerts/${id}`, {
    headers: { Authorization: `Bearer ${analystB()}` },
    data: { status: "acknowledged", version: 1 },
  });
  expect(hidden.status()).toBe(404);

  const viewerContext = await browser.newContext();
  const viewer = await viewerContext.newPage();
  await viewer.goto(alertsUrl({ alert_id: id }));
  await signIn(viewer, readerA());
  const viewerDialog = viewer.getByRole("dialog", {
    name: "Alert details",
    exact: true,
  });
  await expect(
    viewerDialog.getByText(/Status: open · Version: 1/),
  ).toBeVisible();
  await expect(
    viewerDialog.getByRole("button", { name: "Acknowledge alert" }),
  ).toHaveCount(0);
  await viewerContext.close();

  const analystContext = await browser.newContext({
    viewport: { width: 1440, height: 1000 },
  });
  const analyst = await analystContext.newPage();
  await analyst.goto(alertsUrl({ alert_id: id }));
  await signIn(analyst, analystA());
  const dialog = analyst.getByRole("dialog", {
    name: "Alert details",
    exact: true,
  });
  await expect(dialog.getByText(/Status: open · Version: 1/)).toBeVisible();

  // A second operator acknowledges first, making the analyst's version stale.
  const adminContext = await browser.newContext();
  const admin = await adminContext.newPage();
  await admin.goto(alertsUrl({ alert_id: id }));
  await signIn(admin, adminA());
  const adminDialog = admin.getByRole("dialog", {
    name: "Alert details",
    exact: true,
  });
  await adminDialog.getByRole("button", { name: "Acknowledge alert" }).click();
  await expect(adminDialog.getByText("Alert acknowledged.")).toBeVisible();
  await adminContext.close();

  await dialog.getByRole("button", { name: "Acknowledge alert" }).click();
  await expect(
    dialog.getByText(
      "This alert changed. Review the latest details before acting again.",
    ),
  ).toBeVisible();
  await expect(
    dialog.getByText(/Status: acknowledged · Version: 2/),
  ).toBeVisible();

  // Resolve confirmation traps and restores focus.
  const resolve = dialog.getByRole("button", {
    name: "Resolve alert",
    exact: true,
  });
  await resolve.focus();
  await resolve.click();
  const confirm = analyst.getByRole("dialog", { name: "Resolve alert?" });
  await expect(
    confirm.getByRole("button", { name: "Return to alert" }),
  ).toBeFocused();
  await analyst.keyboard.press("Escape");
  await expect(confirm).toHaveCount(0);
  await expect(resolve).toBeFocused();
  await resolve.click();
  await confirm
    .getByRole("button", { name: "Resolve alert", exact: true })
    .click();
  await expect(dialog.getByText("Alert resolved.")).toBeVisible();
  await expect(dialog.getByText(/Status: resolved · Version: 3/)).toBeVisible();
  const detail = await api(request, analystA(), `/api/v1/alerts/${id}`);
  const history = (
    detail.body as { history: { to_status: string; actor: string }[] }
  ).history;
  expect(history.map((change) => change.to_status)).toEqual([
    "acknowledged",
    "resolved",
  ]);
  expect(history.map((change) => change.actor)).toEqual([
    "e2e-admin",
    "e2e-analyst",
  ]);
  await analystContext.close();
});

test("keyboard dialog focus, Back/Forward and reduced motion", async ({
  browser,
}) => {
  const context = await browser.newContext({
    viewport: { width: 1440, height: 1000 },
    reducedMotion: "reduce",
  });
  const page = await context.newPage();
  await page.goto(alertsUrl());
  await signIn(page, analystA());
  expect(
    await page.evaluate(
      () => matchMedia("(prefers-reduced-motion: reduce)").matches,
    ),
  ).toBe(true);
  expect(
    await page.evaluate(
      () =>
        getComputedStyle(document.querySelector("button")!).transitionDuration,
    ),
  ).toMatch(/^0s$/);
  const trigger = page
    .getByRole("button", { name: "Open alert details" })
    .first();
  await trigger.focus();
  await page.keyboard.press("Enter");
  const dialog = page.getByRole("dialog", {
    name: "Alert details",
    exact: true,
  });
  await expect(dialog).toBeVisible();
  await expect(
    dialog.getByRole("button", { name: "Close alert details" }),
  ).toBeFocused();
  expect(new URL(page.url()).searchParams.get("alert_id")).toMatch(
    /^[0-9a-f]{64}$/,
  );
  // Focus stays inside the modal while tabbing.
  for (let n = 0; n < 6; n++) {
    await page.keyboard.press("Tab");
    // A native modal makes the page inert: focus is either inside the dialog or
    // has left the document for browser chrome (activeElement falls back to body).
    expect(
      await dialog.evaluate(
        (node) =>
          node.contains(document.activeElement) ||
          document.activeElement === document.body,
      ),
    ).toBe(true);
  }
  await page.keyboard.press("Escape");
  await expect(dialog).toHaveCount(0);
  await expect(trigger).toBeFocused();
  await trigger.click();
  await expect(dialog).toBeVisible();
  await page.goBack();
  await expect(dialog).toHaveCount(0);
  await page.goForward();
  await expect(dialog).toBeVisible();
  await context.close();
});

for (const viewport of [
  { label: "1440", width: 1440, height: 900, scale: 1 },
  { label: "375", width: 375, height: 800, scale: 1 },
  { label: "200pct-zoom", width: 720, height: 450, scale: 2 },
]) {
  test(`all four pages reflow without horizontal overflow at ${viewport.label}`, async ({
    browser,
  }) => {
    const context = await browser.newContext({
      viewport: { width: viewport.width, height: viewport.height },
      deviceScaleFactor: viewport.scale,
      reducedMotion: "reduce",
    });
    const page = await context.newPage();
    const problems: string[] = [];
    page.on("pageerror", () => problems.push("pageerror"));
    page.on("console", (message) => {
      if (["error", "warning"].includes(message.type()))
        problems.push(message.type());
    });
    await page.goto(alertsUrl());
    await signIn(page, analystA());
    for (const [name, heading] of [
      ["Alerts", "Alerts"],
      ["Overview", "Overview"],
      ["Events", "Events"],
      ["Rules", "Rules"],
    ] as const) {
      if (name !== "Alerts")
        await page.getByRole("link", { name, exact: true }).click();
      await expect(
        page.getByRole("heading", { name: heading, exact: true }),
      ).toBeVisible();
      await expect(
        page.getByRole("status").filter({ hasText: /^Loading/ }),
      ).toHaveCount(0);
      if (name === "Alerts") {
        if (viewport.width < 768)
          await expect(
            page.getByRole("list", { name: "Tenant alerts" }),
          ).toBeVisible();
        else
          await expect(
            page.getByRole("region", { name: "Tenant alerts table" }),
          ).toBeVisible();
      }
      const geometry = await page.evaluate(() => ({
        scroll: document.documentElement.scrollWidth,
        width: innerWidth,
      }));
      expect(
        geometry.scroll,
        `${name} horizontal overflow`,
      ).toBeLessThanOrEqual(geometry.width);
      await shot(page, `${name.toLowerCase()}-${viewport.label}`);
    }
    expect(problems).toEqual([]);
    await context.close();
  });
}

test("loading, empty and error states recover against the real API", async ({
  browser,
}) => {
  const context = await browser.newContext({
    viewport: { width: 1440, height: 1000 },
  });
  const page = await context.newPage();
  let delayed = false;
  await page.route("**/api/v1/alerts?*", async (route) => {
    if (!delayed) {
      delayed = true;
      await new Promise((resolve) => setTimeout(resolve, 1500));
    }
    await route.continue();
  });
  await page.goto(alertsUrl());
  await signIn(page, analystB());
  await expect(page.getByText("Loading alerts…")).toBeVisible();
  await expect(
    page.getByRole("region", { name: "Tenant alerts table" }),
  ).toBeVisible();
  await page.unroute("**/api/v1/alerts?*");

  // Empty: a real filter matching no actual alert for this tenant.
  await page.getByLabel("Status").fill("resolved");
  await page.getByRole("button", { name: "Apply filters" }).click();
  await expect(
    page.getByRole("heading", { name: "No alerts match these filters" }),
  ).toBeVisible();
  await page.getByLabel("Status").fill("");
  await page.getByRole("button", { name: "Apply filters" }).click();
  await expect(
    page.getByRole("region", { name: "Tenant alerts table" }),
  ).toBeVisible();

  // Error: one injected dependency failure, then the real retry succeeds.
  let failed = false;
  await page.route("**/api/v1/alerts?*", async (route) => {
    if (!failed) {
      failed = true;
      await route.fulfill({
        status: 503,
        contentType: "application/json",
        body: '{"error":{"code":"SERVICE_UNAVAILABLE"}}',
      });
      return;
    }
    await route.continue();
  });
  // A new filter value is a new query; an unchanged one would be served from cache.
  await page.getByLabel("Rule ID").fill("PORT_SCAN");
  await page.getByRole("button", { name: "Apply filters" }).click();
  await expect(
    page.getByText("Could not load alerts. Try again."),
  ).toBeVisible();
  await page.getByRole("button", { name: "Retry loading alerts" }).click();
  await expect(
    page.getByRole("region", { name: "Tenant alerts table" }),
  ).toBeVisible();
  await context.close();
});

test("cursor, tenant switch and logout leave no stale tenant UI", async ({
  browser,
}) => {
  const a = fixture.tenants.a;
  const b = fixture.tenants.b;
  const context = await browser.newContext({
    viewport: { width: 1440, height: 1000 },
  });
  const page = await context.newPage();
  const lists = watchAlertLists(page);
  await page.goto(alertsUrl({ limit: "2" }));
  await signIn(page, analystA());
  await expect(
    page.getByRole("status").filter({ hasText: "2 alerts on this page" }),
  ).toBeVisible();
  await page.getByRole("button", { name: "Next page" }).click();
  await expect
    .poll(() => new URL(page.url()).searchParams.has("cursor"))
    .toBe(true);
  await page.getByRole("button", { name: "Sign out", exact: true }).click();
  await page
    .getByRole("heading", { name: "Sign in to Sentinel", exact: true })
    .waitFor();
  expect(new URL(page.url()).searchParams.has("cursor")).toBe(false);
  expect(await page.getByLabel("Development access token").inputValue()).toBe(
    "",
  );
  await expect(page.getByText(a.name)).toHaveCount(0);
  lists.seen.clear();
  await signIn(page, analystB());
  await expect(page.getByText(`${b.name} · analyst`)).toBeVisible();
  await page.goBack();
  if (new URL(page.url()).pathname === "/alerts") {
    await expect(page.getByText(a.name)).toHaveCount(0);
  }
  for (const id of a.alertIds) expect(lists.seen.has(id)).toBe(false);

  // Expiry clears the tenant UI as well.
  await page.goto(alertsUrl());
  await signIn(page, mint(a.name, "analyst", 8));
  await expect(
    page.getByText("Your session expired. Sign in again to continue."),
  ).toBeVisible({ timeout: 20000 });
  await expect(
    page.getByRole("heading", { name: "Sign in to Sentinel", exact: true }),
  ).toBeVisible();
  await expect(page.getByText(a.name)).toHaveCount(0);
  await context.close();
});

async function bothReplicas(action: "stop" | "start", only?: string) {
  for (const [service, container] of Object.entries(fixture.replicas)) {
    if (only && service !== only) continue;
    execFileSync("docker", [action, container], {
      stdio: "ignore",
      timeout: 90000,
    });
  }
}

async function replicaReady(request: APIRequestContext) {
  await expect
    .poll(
      async () =>
        (
          await request
            .get("/api/v1/session", {
              headers: { Authorization: `Bearer ${analystA()}` },
            })
            .catch(() => null)
        )?.status(),
      {
        timeout: 90000,
      },
    )
    .toBe(200);
}

function burst(round: string): string[] {
  const out = execFileSync(
    fixture.seeder.python,
    [fixture.seeder.script, "burst", need("PHASE4_FIXTURE"), "a", round],
    { encoding: "utf8", timeout: 240000 },
  );
  return (JSON.parse(out) as { alertIds: string[] }).alertIds;
}

test("live reconnect recovers exact missed commits through each query replica", async ({
  browser,
  request,
}) => {
  test.setTimeout(480000);
  const context = await browser.newContext({
    viewport: { width: 1440, height: 1000 },
  });
  const page = await context.newPage();
  const lists = watchAlertLists(page);
  const served: string[] = [];
  page.on("response", (response) => {
    if (
      new URL(response.url()).pathname === "/api/v1/alerts/stream" &&
      response.status() === 200
    )
      served.push(response.headers()["x-phase4-replica"] ?? "unknown");
  });
  await page.goto(alertsUrl());
  await signIn(page, analystA());
  await expect(page.getByLabel("Alert connection")).toHaveText("Live", {
    timeout: 30000,
  });
  await expect
    .poll(() => sorted(lists.latest))
    .toEqual(sorted(fixture.tenants.a.alertIds));
  // Acknowledge the initial catch-up so later notices come from missed commits only.
  await page.getByRole("button", { name: "Refresh alerts" }).click();
  await expect(page.getByText("New alerts available")).toHaveCount(0);
  let expected = [...fixture.tenants.a.alertIds];
  const services = Object.keys(fixture.replicas);
  const rounds: [string, string][] = [
    ["port-scan", services[0]],
    ["deny-spike", services[1]],
  ];
  try {
    for (const [scenario, survivor] of rounds) {
      await bothReplicas("stop");
      await expect(
        page.getByText("Reconnecting… Missed alerts will be recovered."),
      ).toBeVisible({ timeout: 30000 });
      // Commits happen while neither replica nor any stream exists.
      const missed = burst(scenario);
      expect(missed.length).toBeGreaterThan(0);
      expected = [...expected, ...missed];
      served.length = 0;
      await bothReplicas("start", survivor);
      await replicaReady(request);
      await expect(page.getByLabel("Alert connection")).toHaveText("Live", {
        timeout: 60000,
      });
      // Catch-up delivered the missed commits over the recovered stream.
      await expect(page.getByText("New alerts available")).toBeVisible({
        timeout: 60000,
      });
      await expect
        .poll(
          async () => {
            const refresh = page.getByRole("button", {
              name: "Refresh alerts",
            });
            if (await refresh.isEnabled().catch(() => false))
              await refresh.click();
            await page.waitForTimeout(500);
            return sorted(lists.latest);
          },
          { timeout: 60000 },
        )
        .toEqual(sorted(expected));
      expect(new Set(served), `stream served only by ${survivor}`).toEqual(
        new Set([survivor]),
      );
      await bothReplicas("start");
    }
  } finally {
    await bothReplicas("start");
  }
  await shot(page, "reconnect-recovered-1440");
  await context.close();
});
