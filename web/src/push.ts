import { api, store, stored } from "./api";
import {
  NoContentSchema,
  PushStatusSchema,
  PushSubscriptionResultSchema,
  PushTestResultSchema,
  type PushReason,
  type PushStatus,
  type PushTestResult,
} from "./schemas";

// The server-side ID of this browser's subscription, under webmux.push.subscriptionId.
const SUBSCRIPTION_KEY = "push.subscriptionId";
const WORKER = "/sw.js";

export type PushSupport = {
  secure: boolean;
  serviceWorker: boolean;
  pushManager: boolean;
  notification: boolean;
  ios: boolean;
  standalone: boolean;
  permission: NotificationPermission | "unsupported";
};

export function detectSupport(): PushSupport {
  const nav = navigator as Navigator & { standalone?: boolean };
  const ios =
    /iPad|iPhone|iPod/.test(nav.userAgent) ||
    (nav.platform === "MacIntel" && nav.maxTouchPoints > 1);
  const notification = typeof Notification !== "undefined";
  return {
    secure: globalThis.isSecureContext === true,
    serviceWorker: "serviceWorker" in nav,
    pushManager: typeof PushManager !== "undefined",
    notification,
    ios,
    standalone:
      nav.standalone === true ||
      (typeof matchMedia === "function" &&
        matchMedia("(display-mode: standalone)").matches),
    permission: notification ? Notification.permission : "unsupported",
  };
}

// What the settings card shows. Insecure pages hide the push APIs entirely
// and iOS exposes them only to home screen apps, so both are diagnosed before
// missing capabilities.
export type PushAdvice =
  | "insecure"
  | "ios-home-screen"
  | "unsupported"
  | PushReason
  | "denied"
  | "ready";

export function advise(support: PushSupport, status?: PushStatus): PushAdvice {
  if (!support.secure) return "insecure";
  if (support.ios && !support.standalone) return "ios-home-screen";
  if (!support.serviceWorker || !support.pushManager || !support.notification)
    return "unsupported";
  if (status?.reason) return status.reason;
  if (support.permission === "denied") return "denied";
  return "ready";
}

export function urlBase64ToUint8Array(value: string): Uint8Array<ArrayBuffer> {
  const base64 = value.replaceAll("-", "+").replaceAll("_", "/");
  const binary = atob(base64 + "=".repeat((4 - (base64.length % 4)) % 4));
  return Uint8Array.from(binary, (c) => c.charCodeAt(0));
}

function sameKey(current: ArrayBuffer | null, publicKey: string): boolean {
  if (!current) return false;
  const a = new Uint8Array(current);
  const b = urlBase64ToUint8Array(publicKey);
  return a.length === b.length && a.every((byte, i) => byte === b[i]);
}

export const pushStatus = () => api("/push", PushStatusSchema);

export const savedSubscriptionId = () => stored(SUBSCRIPTION_KEY);

// The browser subscription, if this page registered one before. It never
// registers the service worker.
export async function currentSubscription(): Promise<PushSubscription | null> {
  if (!("serviceWorker" in navigator)) return null;
  const registration = await navigator.serviceWorker.getRegistration("/");
  return (await registration?.pushManager.getSubscription()) ?? null;
}

async function register(subscription: PushSubscription): Promise<string> {
  const { id } = await api(
    "/push/subscriptions",
    PushSubscriptionResultSchema,
    "POST",
    subscription.toJSON(),
  );
  store(SUBSCRIPTION_KEY, id);
  return id;
}

// requestPermission resolves once the user answers the browser's prompt. With
// Chrome's quiet permission UI that answer waits on a click on the address-bar
// bell, so callers should tell the user where to look while it is pending.
export async function requestPermission(): Promise<void> {
  const permission = await Notification.requestPermission();
  if (permission !== "granted")
    throw new Error(
      "未获得通知权限：请点击地址栏中的铃铛图标或在浏览器网站设置中允许本站通知，然后重试",
    );
}

// enable must run inside the click that asked for it: browsers only show the
// permission prompt for a user gesture.
export async function enable(publicKey: string): Promise<void> {
  await requestPermission();
  const registration = await navigator.serviceWorker.register(WORKER, {
    scope: "/",
  });
  await navigator.serviceWorker.ready;
  let subscription = await registration.pushManager.getSubscription();
  if (
    subscription &&
    !sameKey(subscription.options.applicationServerKey, publicKey)
  ) {
    await subscription.unsubscribe();
    subscription = null;
  }
  subscription ??= await registration.pushManager.subscribe({
    userVisibleOnly: true,
    applicationServerKey: urlBase64ToUint8Array(publicKey),
  });
  try {
    await register(subscription);
  } catch (error) {
    // A browser subscription the server refused would never be used.
    await subscription.unsubscribe().catch(() => undefined);
    store(SUBSCRIPTION_KEY, "");
    throw error;
  }
}

// disable always drops the browser subscription, even if the server cannot
// forget it; the server deletes it later when the push service reports 410.
export async function disable(): Promise<void> {
  const subscription = await currentSubscription();
  const id = savedSubscriptionId();
  store(SUBSCRIPTION_KEY, "");
  await subscription?.unsubscribe();
  if (id)
    await api(
      `/push/subscriptions/${encodeURIComponent(id)}`,
      NoContentSchema,
      "DELETE",
    );
}

export type SyncResult = "none" | "unavailable" | "synced" | "key-changed";

// sync re-registers an existing subscription idempotently, for example after
// the server rebuilt its store. Without one it does nothing: no worker
// registration and no permission prompt.
export async function sync(): Promise<SyncResult> {
  if (
    typeof Notification === "undefined" ||
    Notification.permission !== "granted"
  )
    return "none";
  const subscription = await currentSubscription();
  if (!subscription) return "none";
  const status = await pushStatus();
  if (!status.available || !status.publicKey) return "unavailable";
  if (!sameKey(subscription.options.applicationServerKey, status.publicKey)) {
    await subscription.unsubscribe();
    store(SUBSCRIPTION_KEY, "");
    return "key-changed";
  }
  await register(subscription);
  return "synced";
}

export type TestOutcome = PushTestResult & { ownRemoved: boolean };

// sendTest pushes a test notification to every registered device, so one
// device can check what the others show in the background. When the push
// service dropped this browser's own subscription, it is forgotten here too.
export async function sendTest(): Promise<TestOutcome> {
  const result = await api("/push/test", PushTestResultSchema, "POST");
  const id = savedSubscriptionId();
  const ownRemoved = !!id && result.removed.includes(id);
  if (ownRemoved) {
    store(SUBSCRIPTION_KEY, "");
    await (await currentSubscription())?.unsubscribe().catch(() => undefined);
  }
  return { ...result, ownRemoved };
}

// describeTest turns a test outcome into one line for the settings card.
export function describeTest(outcome: TestOutcome): {
  failed: boolean;
  message: string;
} {
  const { devices, accepted, removed, errors, ownRemoved } = outcome;
  if (devices === 0) return { failed: true, message: "还没有设备开启推送" };
  if (accepted === devices)
    return {
      failed: false,
      message: `已提交给全部 ${devices} 台设备的推送服务，若数秒内未收到，请检查各设备的系统通知设置`,
    };
  const parts = [`已提交给 ${accepted}/${devices} 台设备的推送服务`];
  if (removed.length) parts.push(`${removed.length} 台订阅已失效并已删除`);
  if (errors.length)
    parts.push(`${errors.length} 台发送失败：${errors.join("；")}`);
  if (ownRemoved) parts.push("本设备需要重新开启推送");
  return { failed: errors.length > 0 || ownRemoved, message: parts.join("；") };
}
