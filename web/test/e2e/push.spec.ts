import type { APIRequestContext, BrowserContext, Page } from "@playwright/test";
import { expect, test } from "./fixtures";

// chrome-headless-shell reports Notification.permission as "denied" even
// after grantPermissions; new headless Chromium grants it.
test.use({ channel: "chromium" });

// Chromium cannot reach a real push service in CI, so PushManager hands out
// a subscription with real P-256 keys and an endpoint that never resolves.
// Registration, the server's delivery attempt, the service worker's display
// and its click routing all run for real.
async function fakePushManager(context: BrowserContext) {
  await context.addInitScript(() => {
    const encode = (bytes: Uint8Array) =>
      btoa(String.fromCharCode(...bytes))
        .replaceAll("+", "-")
        .replaceAll("/", "_")
        .replace(/=+$/, "");
    const key = "webmux.e2e.subscription";
    type Fake = {
      endpoint: string;
      p256dh: string;
      auth: string;
      server: number[];
    };
    const build = (fake: Fake) => ({
      endpoint: fake.endpoint,
      options: {
        userVisibleOnly: true,
        applicationServerKey: new Uint8Array(fake.server).buffer,
      },
      // Same shape as a real PushSubscription.toJSON().
      toJSON: () => ({
        endpoint: fake.endpoint,
        expirationTime: null,
        keys: { p256dh: fake.p256dh, auth: fake.auth },
      }),
      unsubscribe: async () => {
        localStorage.removeItem(key);
        return true;
      },
    });
    PushManager.prototype.getSubscription = async function () {
      const saved = localStorage.getItem(key);
      return saved ? (build(JSON.parse(saved)) as never) : null;
    };
    PushManager.prototype.subscribe = async function (options) {
      const pair = await crypto.subtle.generateKey(
        { name: "ECDH", namedCurve: "P-256" },
        true,
        ["deriveBits"],
      );
      const raw = new Uint8Array(
        await crypto.subtle.exportKey("raw", pair.publicKey),
      );
      const fake: Fake = {
        endpoint: "https://push.invalid/e2e",
        p256dh: encode(raw),
        auth: encode(crypto.getRandomValues(new Uint8Array(16))),
        server: [
          ...new Uint8Array(options?.applicationServerKey as ArrayBuffer),
        ],
      };
      localStorage.setItem(key, JSON.stringify(fake));
      return build(fake) as never;
    };
  });
}

async function serverSubscriptions(request: APIRequestContext) {
  const response = await request.get("/api/push");
  expect(response.ok()).toBe(true);
  return ((await response.json()) as { subscriptions: number }).subscriptions;
}

async function registrationId(page: Page) {
  const cdp = await page.context().newCDPSession(page);
  const registrations = new Promise<string>((resolve) =>
    cdp.on("ServiceWorker.workerRegistrationUpdated", ({ registrations }) => {
      const match = registrations.find((r) => !r.isDeleted);
      if (match) resolve(match.registrationId);
    }),
  );
  await cdp.send("ServiceWorker.enable");
  return { cdp, id: await registrations };
}

// Opens the settings card with notifications granted and turns push on.
async function enablePush(
  page: Page,
  context: BrowserContext,
  request: APIRequestContext,
  baseURL: string,
) {
  await fakePushManager(context);
  await context.grantPermissions(["notifications"], { origin: baseURL });
  await page.goto("/settings#push");
  const card = page.locator("#push");
  await expect(card.getByText("本设备未开启")).toBeVisible();
  await card.getByRole("switch", { name: "在本设备开启推送" }).click();
  await expect(card.getByText("本设备已开启")).toBeVisible();
  expect(await serverSubscriptions(request)).toBe(1);
  return card;
}

test("enable push, send a test through the server, then disable", async ({
  page,
  context,
  request,
  baseURL,
}) => {
  const card = await enablePush(page, context, request, baseURL!);

  // The endpoint does not resolve: the server answers 502 naming only the host.
  await card.getByRole("button", { name: "发送测试通知" }).click();
  const failure = card.getByRole("alert");
  await expect(failure).toContainText("push.invalid");
  await expect(failure).not.toContainText("/e2e");
  expect(await serverSubscriptions(request)).toBe(1);

  await card.getByRole("switch", { name: "在本设备开启推送" }).click();
  await expect(card.getByText("本设备未开启")).toBeVisible();
  expect(await serverSubscriptions(request)).toBe(0);
});

test("the service worker shows a pushed alert and opens its session", async ({
  page,
  context,
  request,
  baseURL,
}) => {
  test.skip(
    process.platform === "darwin",
    "Headless Chromium on macOS never lists the worker's notification.",
  );
  const created = await request.post("/api/sessions", {
    data: { preset: "shell" },
  });
  const sessionId = ((await created.json()) as { id: string }).id;
  await enablePush(page, context, request, baseURL!);

  // A trusted push event through DevTools, carrying a server-shaped payload.
  const worker =
    context.serviceWorkers()[0] ??
    (await context.waitForEvent("serviceworker"));
  const { cdp, id } = await registrationId(page);
  await cdp.send("ServiceWorker.deliverPushMessage", {
    origin: new URL(baseURL!).origin,
    registrationId: id,
    data: JSON.stringify({
      web_push: 8030,
      notification: {
        title: "deploy",
        body: "等待授权",
        navigate: "https://evil.example/",
        tag: `webmux-${sessionId}`,
      },
      webmux: { v: 1, sessionId, kind: "permission", source: "agent" },
    }),
  });
  await expect
    .poll(() =>
      worker.evaluate(async () =>
        (await self.registration.getNotifications()).map(
          (n) => `${n.title}|${n.body}|${n.tag}`,
        ),
      ),
    )
    .toEqual([`deploy|等待授权|webmux-${sessionId}`]);

  // Run the worker's click routing on the real notification: the open
  // settings window navigates to the session, never to `navigate`.
  await worker.evaluate(async () => {
    const [notification] = await self.registration.getNotifications();
    await (
      self as unknown as {
        openNotification(n: Notification): Promise<void>;
      }
    ).openNotification(notification);
  });
  await expect(page).toHaveURL(`${baseURL}/s/${sessionId}`);
  expect(
    await worker.evaluate(
      async () => (await self.registration.getNotifications()).length,
    ),
  ).toBe(0);
});
