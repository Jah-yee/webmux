import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { connect, type SocketConnection } from "../src/ws";

class Socket {
  static OPEN = 1;
  static instances: Socket[] = [];
  readyState = 1;
  onopen?: () => void;
  onmessage?: (event: { data: string | ArrayBuffer }) => void;
  onclose?: (event: { code: number }) => void;
  onerror?: () => void;
  constructor(_url: string) {
    Socket.instances.push(this);
  }
  close() {
    this.readyState = 3;
  }
  fail(code = 4408) {
    this.readyState = 3;
    this.onclose?.({ code });
  }
}
let connection: SocketConnection;
beforeEach(() => {
  vi.useFakeTimers();
  vi.spyOn(Math, "random").mockReturnValue(0);
  Socket.instances = [];
  vi.stubGlobal("WebSocket", Socket);
  vi.stubGlobal("location", { protocol: "http:", host: "localhost" });
  vi.stubGlobal("localStorage", { getItem: () => null });
  vi.stubGlobal("document", new EventTarget());
  const events = new EventTarget();
  vi.stubGlobal("window", {
    setTimeout,
    addEventListener: events.addEventListener.bind(events),
    removeEventListener: events.removeEventListener.bind(events),
    dispatchEvent: events.dispatchEvent.bind(events),
  });
  connection = connect(
    "/ws/s/example",
    () => {},
    () => {},
    () => {},
  );
  Socket.instances[0].onopen?.();
});
afterEach(() => {
  connection.close();
  vi.useRealTimers();
  vi.restoreAllMocks();
  vi.unstubAllGlobals();
});

it("backs off slow-client closes instead of replaying immediately", () => {
  Socket.instances[0].fail();
  vi.advanceTimersByTime(0);
  expect(Socket.instances).toHaveLength(1);
  vi.advanceTimersByTime(250);
  expect(Socket.instances).toHaveLength(2);
});

it("keeps failure history across short opens and resets after a stable connection", () => {
  Socket.instances[0].fail();
  vi.advanceTimersByTime(250);
  const second = Socket.instances[1];
  expect(second).toBeDefined();
  second.onopen?.();
  second.fail();
  vi.advanceTimersByTime(250);
  expect(Socket.instances).toHaveLength(2);
  vi.advanceTimersByTime(250);
  const third = Socket.instances[2];
  third.onopen?.();
  vi.advanceTimersByTime(10000);
  third.fail();
  vi.advanceTimersByTime(250);
  expect(Socket.instances).toHaveLength(4);
});

it("does not reconnect after permanent close or disposal", () => {
  Socket.instances[0].fail(4410);
  vi.advanceTimersByTime(60000);
  expect(Socket.instances).toHaveLength(1);
  connection.close();
  connection = connect(
    "/ws/s/example",
    () => {},
    () => {},
    () => {},
  );
  Socket.instances[1].fail();
  connection.close();
  vi.advanceTimersByTime(60000);
  expect(Socket.instances).toHaveLength(2);
});

it("ignores queued open and message callbacks after disposal", () => {
  connection.close();
  const message = vi.fn();
  const state = vi.fn();
  connection = connect("/ws/s/example", message, state, vi.fn());
  const socket = Socket.instances.at(-1)!;
  state.mockClear();
  connection.close();
  socket.onopen?.();
  socket.onmessage?.({ data: "late message" });
  connection.reconnect();
  expect(message).not.toHaveBeenCalled();
  expect(state).not.toHaveBeenCalled();
  expect(vi.getTimerCount()).toBe(0);
  expect(Socket.instances).toHaveLength(2);
});

it("ignores queued callbacks from a closed connection during backoff", () => {
  connection.close();
  const message = vi.fn();
  const state = vi.fn();
  connection = connect("/ws/s/example", message, state, vi.fn());
  const socket = Socket.instances.at(-1)!;
  socket.fail();
  state.mockClear();
  socket.onopen?.();
  socket.onmessage?.({ data: "late message" });
  expect(message).not.toHaveBeenCalled();
  expect(state).not.toHaveBeenCalled();
  vi.advanceTimersByTime(250);
  expect(Socket.instances).toHaveLength(3);
});

it("does not replace a healthy connection on an ordinary pageshow", () => {
  window.dispatchEvent(new Event("pageshow"));
  expect(Socket.instances).toHaveLength(1);
});

it("reattaches on a bfcache restoration or foreground event", () => {
  const restored = new Event("pageshow");
  Object.defineProperty(restored, "persisted", { value: true });
  window.dispatchEvent(restored);
  expect(Socket.instances).toHaveLength(2);
  document.dispatchEvent(new Event("visibilitychange"));
  expect(Socket.instances).toHaveLength(3);
});
