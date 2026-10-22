import {
  afterEach,
  beforeEach,
  describe,
  expect,
  it,
  vi,
  type Mock,
} from "vitest";
import {
  advise,
  detectSupport,
  enable,
  sendTest,
  sync,
  urlBase64ToUint8Array,
  type PushSupport,
} from "../src/push";

const ready: PushSupport = {
  secure: true,
  serviceWorker: true,
  pushManager: true,
  notification: true,
  ios: false,
  standalone: false,
  permission: "default",
};
const status = (extra = {}) => ({
  enabled: true,
  available: true,
  subscriptions: 0,
  ...extra,
});

describe("advise", () => {
  it.each<[string, Partial<PushSupport>, object | undefined, string]>([
    [
      "insecure page hides the APIs",
      { secure: false, serviceWorker: false },
      undefined,
      "insecure",
    ],
    [
      "iOS Safari tab",
      { ios: true, pushManager: false },
      status(),
      "ios-home-screen",
    ],
    ["iOS home screen app", { ios: true, standalone: true }, status(), "ready"],
    ["missing Push API", { pushManager: false }, status(), "unsupported"],
    [
      "quick tunnel",
      {},
      status({ available: false, reason: "quick_tunnel" }),
      "quick_tunnel",
    ],
    [
      "other origin",
      {},
      status({ available: false, reason: "unsupported_origin" }),
      "unsupported_origin",
    ],
    [
      "disabled",
      {},
      status({ enabled: false, available: false, reason: "disabled" }),
      "disabled",
    ],
    [
      "server reason before permission",
      { permission: "denied" },
      status({ available: false, reason: "storage" }),
      "storage",
    ],
    ["permission denied", { permission: "denied" }, status(), "denied"],
    ["ready", {}, status(), "ready"],
  ])("%s", (_, support, server, want) => {
    expect(advise({ ...ready, ...support }, server as never)).toBe(want);
  });
});

it("decodes base64url VAPID keys", () => {
  expect([...urlBase64ToUint8Array("BP4z-_8")]).toEqual([4, 254, 51, 251, 255]);
  expect(urlBase64ToUint8Array("A".repeat(87))).toHaveLength(65);
});

// A fake browser: storage, fetch, Notification and a service worker whose
// push manager hands out one subscription.
function fakeBrowser(permission: NotificationPermission = "granted") {
  const storage = new Map<string, string>();
  const calls: { method: string; path: string; body?: unknown }[] = [];
  const responses: Record<string, { status: number; body?: unknown }> = {};
  const key = urlBase64ToUint8Array("BP4z" + "A".repeat(83));
  let subscription: {
    options: { applicationServerKey: ArrayBuffer };
    unsubscribe: Mock;
    toJSON: () => object;
  } | null = null;
  const subscribe = vi.fn(async () => {
    subscription = {
      options: { applicationServerKey: key.buffer },
      unsubscribe: vi.fn(async () => {
        subscription = null;
        return true;
      }),
      toJSON: () => ({ endpoint: "https://push.example/x", keys: {} }),
    };
    return subscription;
  });
  const registration = {
    pushManager: { getSubscription: async () => subscription, subscribe },
  };
  const serviceWorker = {
    registered: false,
    register: vi.fn(async () => {
      serviceWorker.registered = true;
      return registration;
    }),
    getRegistration: vi.fn(async () =>
      serviceWorker.registered ? registration : undefined,
    ),
    ready: Promise.resolve(registration),
  };
  vi.stubGlobal("localStorage", {
    getItem: (k: string) => storage.get(k) ?? null,
    setItem: (k: string, v: string) => storage.set(k, v),
  });
  vi.stubGlobal("window", { dispatchEvent: () => true });
  vi.stubGlobal("navigator", { serviceWorker, userAgent: "", platform: "" });
  vi.stubGlobal("Notification", {
    permission,
    requestPermission: vi.fn(async () => permission),
  });
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string, init: RequestInit) => {
      const path = url.replace(/^\/api/, "");
      calls.push({
        method: init.method ?? "GET",
        path,
        body: init.body ? JSON.parse(String(init.body)) : undefined,
      });
      const reply = responses[`${init.method} ${path}`] ?? { status: 204 };
      return new Response(
        reply.body === undefined ? null : JSON.stringify(reply.body),
        { status: reply.status },
      );
    }),
  );
  return {
    storage,
    calls,
    responses,
    serviceWorker,
    publicKey: "BP4z" + "A".repeat(83),
    subscription: () => subscription,
  };
}

describe("push client", () => {
  beforeEach(() => vi.unstubAllGlobals());
  afterEach(() => vi.unstubAllGlobals());

  it("registers the subscription and keeps its ID", async () => {
    const b = fakeBrowser();
    b.responses["POST /push/subscriptions"] = {
      status: 200,
      body: { id: "sub1" },
    };
    await enable(b.publicKey);
    expect(b.serviceWorker.register).toHaveBeenCalledWith("/sw.js", {
      scope: "/",
    });
    expect(b.storage.get("webmux.push.subscriptionId")).toBe("sub1");
    expect(b.subscription()).not.toBeNull();
  });

  it("rolls back the browser subscription when the server refuses it", async () => {
    const b = fakeBrowser();
    b.responses["POST /push/subscriptions"] = {
      status: 409,
      body: { error: "需要 Named Tunnel", reason: "quick_tunnel" },
    };
    await expect(enable(b.publicKey)).rejects.toMatchObject({
      status: 409,
      reason: "quick_tunnel",
    });
    expect(b.subscription()).toBeNull();
    expect(b.storage.get("webmux.push.subscriptionId") ?? "").toBe("");
  });

  it("refuses without notification permission", async () => {
    const b = fakeBrowser("denied");
    await expect(enable(b.publicKey)).rejects.toThrow("通知权限");
    expect(b.serviceWorker.register).not.toHaveBeenCalled();
  });

  it("clears the saved ID when the test push reports 410", async () => {
    const b = fakeBrowser();
    b.responses["POST /push/subscriptions"] = {
      status: 200,
      body: { id: "sub1" },
    };
    await enable(b.publicKey);
    b.responses["POST /push/test"] = { status: 410, body: { error: "gone" } };
    await expect(sendTest()).rejects.toMatchObject({ status: 410 });
    expect(b.storage.get("webmux.push.subscriptionId")).toBe("");
    expect(b.subscription()).toBeNull();
  });

  it("sync without a registration registers nothing", async () => {
    const b = fakeBrowser("granted");
    expect(await sync()).toBe("none");
    expect(b.serviceWorker.register).not.toHaveBeenCalled();
    expect(b.calls).toEqual([]);
  });

  it("sync re-registers an existing subscription or drops a stale key", async () => {
    const b = fakeBrowser();
    b.responses["POST /push/subscriptions"] = {
      status: 200,
      body: { id: "sub1" },
    };
    await enable(b.publicKey);
    b.responses["GET /push"] = {
      status: 200,
      body: status({ publicKey: b.publicKey }),
    };
    b.responses["POST /push/subscriptions"] = {
      status: 200,
      body: { id: "sub2" },
    };
    expect(await sync()).toBe("synced");
    expect(b.storage.get("webmux.push.subscriptionId")).toBe("sub2");

    b.responses["GET /push"] = {
      status: 200,
      body: status({ publicKey: "B" + "Q".repeat(86) }),
    };
    expect(await sync()).toBe("key-changed");
    expect(b.subscription()).toBeNull();
    expect(b.storage.get("webmux.push.subscriptionId")).toBe("");
  });

  it("detects iOS Safari outside the home screen", () => {
    fakeBrowser();
    vi.stubGlobal("navigator", {
      userAgent: "Mozilla/5.0 (iPhone; CPU iPhone OS 18_0 like Mac OS X)",
      platform: "iPhone",
      maxTouchPoints: 5,
      standalone: false,
    });
    vi.stubGlobal("isSecureContext", true);
    vi.stubGlobal("matchMedia", () => ({ matches: false }));
    const support = detectSupport();
    expect(support).toMatchObject({
      ios: true,
      standalone: false,
      secure: true,
      serviceWorker: false,
    });
    expect(advise(support)).toBe("ios-home-screen");
  });
});
