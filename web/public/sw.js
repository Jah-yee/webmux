// WebMux push service worker. It only shows attention notifications and
// opens the session on click: no fetch handling, no caching, no network
// requests, and no token. Kept as a classic script without a build step;
// test/sw.test.ts evaluates the pure functions below.

// Same pattern as SessionInfoSchema.id in src/schemas.ts.
const SESSION_ID = /^[a-z2-7]{8}$/;
// Keep in sync with internal/push/alert.go (guarded by
// TestServiceWorkerFallback): a payload that shows nothing can make the
// browser revoke the subscription.
const FALLBACK = { title: "WebMux", body: "有会话需要关注", tag: "webmux" };

// Reads the notification fields and session ID from a push payload. Anything
// unexpected falls back to a generic notification, because a push that shows
// nothing can make the browser revoke the subscription.
function parsePush(text) {
  let message;
  try {
    message = JSON.parse(text);
  } catch {
    return { ...FALLBACK, sessionId: "" };
  }
  const n =
    message && typeof message === "object" ? message.notification : null;
  const meta = message && typeof message === "object" ? message.webmux : null;
  const nonEmpty = (value, fallback) =>
    typeof value === "string" && value ? value : fallback;
  const sessionId =
    meta &&
    typeof meta.sessionId === "string" &&
    SESSION_ID.test(meta.sessionId)
      ? meta.sessionId
      : "";
  return {
    title: nonEmpty(n && n.title, FALLBACK.title),
    body: nonEmpty(n && n.body, FALLBACK.body),
    tag: nonEmpty(n && n.tag, FALLBACK.tag),
    sessionId,
  };
}

// The page to open for a clicked notification: only this origin, and only
// /s/<valid id> or /. A payload's navigate URL is never followed.
function clickPath(data) {
  const id = data && typeof data.sessionId === "string" ? data.sessionId : "";
  return SESSION_ID.test(id) ? "/s/" + id : "/";
}

// Picks the window to reuse: one already showing the target path, else any
// same-origin window, else none (open a new one).
function chooseClient(clients, origin, path) {
  const same = clients.filter((c) => new URL(c.url).origin === origin);
  const exact = same.find((c) => new URL(c.url).pathname === path);
  if (exact) return { client: exact, navigate: false };
  if (same.length > 0) return { client: same[0], navigate: true };
  return { client: null, navigate: false };
}

// Opens the session of a clicked notification. Pages opened before this
// worker activated are claimed (it has no fetch handler, so claiming changes
// no requests) so that navigate() can reuse their window.
async function openNotification(notification) {
  notification.close();
  const path = clickPath(notification.data);
  const all = await self.clients.matchAll({
    type: "window",
    includeUncontrolled: true,
  });
  const { client, navigate } = chooseClient(all, self.location.origin, path);
  if (!client) {
    await self.clients.openWindow(path);
    return;
  }
  // focus() may be refused without a user gesture; navigating still helps.
  const focused = (await client.focus().catch(() => null)) || client;
  if (!navigate) return;
  try {
    await focused.navigate(path);
  } catch {
    await self.clients.openWindow(path);
  }
}

if (typeof ServiceWorkerGlobalScope !== "undefined") {
  self.addEventListener("install", () => self.skipWaiting());
  self.addEventListener("activate", (event) =>
    event.waitUntil(self.clients.claim()),
  );

  self.addEventListener("push", (event) => {
    const data = parsePush(event.data ? event.data.text() : "");
    event.waitUntil(
      self.registration.showNotification(data.title, {
        body: data.body,
        tag: data.tag,
        icon: "/icon-192.png",
        data,
      }),
    );
  });

  self.addEventListener("notificationclick", (event) =>
    event.waitUntil(openNotification(event.notification)),
  );
}
