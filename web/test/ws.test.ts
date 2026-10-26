import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { connect, protocols, type SocketConnection } from "../src/ws";

const sendBudget = 1024 * 1024;

class MockWebSocket {
  static OPEN = 1;
  static instances: MockWebSocket[] = [];
  readyState = MockWebSocket.OPEN;
  bufferedAmount = 0;
  binaryType = "";
  onopen: (() => void) | null = null;
  onmessage: ((event: { data: string | ArrayBuffer }) => void) | null = null;
  onclose: ((event: { code: number }) => void) | null = null;
  onerror: (() => void) | null = null;
  sent: Uint8Array[] = [];

  constructor(
    readonly url: string,
    readonly protocols: string[],
  ) {
    MockWebSocket.instances.push(this);
  }
  send(frame: Uint8Array) {
    this.sent.push(frame.slice());
    this.bufferedAmount += frame.byteLength;
  }
  close() {
    this.readyState = 3;
  }
}

describe("WebSocket upstream send budget", () => {
  let connection: SocketConnection;
  let socket: MockWebSocket;

  beforeEach(() => {
    vi.useFakeTimers();
    MockWebSocket.instances = [];
    vi.stubGlobal("WebSocket", MockWebSocket);
    vi.stubGlobal("location", { protocol: "http:", host: "localhost" });
    vi.stubGlobal("localStorage", { getItem: () => "tok en" });
    vi.stubGlobal("document", new EventTarget());
    vi.stubGlobal(
      "window",
      Object.assign(new EventTarget(), { setTimeout: globalThis.setTimeout }),
    );
    connection = connect("/ws/s/test", vi.fn(), vi.fn(), vi.fn());
    socket = MockWebSocket.instances[0]!;
  });

  afterEach(() => {
    connection.close();
    vi.unstubAllGlobals();
    vi.useRealTimers();
  });

  it("authenticates through a subprotocol without putting the token in the URL", () => {
    expect(socket.url).toBe("ws://localhost/ws/s/test");
    expect(socket.protocols).toEqual(["webmux", "webmux.token.dG9rIGVu"]);
  });

  it("encodes any token as a valid base64url subprotocol", () => {
    expect(protocols("")).toEqual(["webmux"]);
    // UTF-8 bytes; "+" "/" become "-" "_" and "==" padding is dropped.
    expect(protocols("终")).toEqual(["webmux", "webmux.token.57uI"]);
    expect(protocols("~~~???a")).toEqual(["webmux", "webmux.token.fn5-Pz8_YQ"]);
  });

  it("rejects input when the socket queue is saturated", () => {
    socket.bufferedAmount = sendBudget;

    expect(connection.send(0, new Uint8Array([65]))).toBe(false);
    expect(socket.sent).toEqual([]);
    expect(socket.bufferedAmount).toBe(sendBudget);
  });

  it("counts the kind byte and rejects a frame that would cross the limit", () => {
    socket.bufferedAmount = sendBudget - 2;

    expect(connection.send(0, new Uint8Array([65, 66]))).toBe(false);
    expect(socket.sent).toEqual([]);
    expect(socket.bufferedAmount).toBe(sendBudget - 2);
  });

  it("accepts a frame that exactly fills the remaining budget", () => {
    socket.bufferedAmount = sendBudget - 3;

    expect(connection.send(0, new Uint8Array([65, 66]))).toBe(true);
    expect(socket.sent).toEqual([new Uint8Array([0, 65, 66])]);
    expect(socket.bufferedAmount).toBe(sendBudget);
    expect(connection.send(0, new Uint8Array([67]))).toBe(false);
    expect(socket.sent).toHaveLength(1);
  });

  it("rejects a single oversized frame even with an empty socket queue", () => {
    expect(connection.send(0, new Uint8Array(sendBudget))).toBe(false);
    expect(socket.sent).toEqual([]);
    expect(socket.bufferedAmount).toBe(0);
  });

  it("preserves input and control frame encoding on a healthy socket", () => {
    const payload = new TextEncoder().encode("你好");

    expect(connection.send(0, payload)).toBe(true);
    expect(connection.send(1, new Uint8Array([123, 125]))).toBe(true);
    expect(connection.send(2, new Uint8Array())).toBe(true);
    expect(socket.sent).toEqual([
      new Uint8Array([0, ...payload]),
      new Uint8Array([1, 123, 125]),
      new Uint8Array([2]),
    ]);
  });

  it("accepts new input after draining without replaying rejected input", () => {
    socket.bufferedAmount = sendBudget;
    expect(connection.send(0, new Uint8Array([65]))).toBe(false);

    socket.bufferedAmount = 0;
    expect(connection.send(0, new Uint8Array([66]))).toBe(true);
    expect(socket.sent).toEqual([new Uint8Array([0, 66])]);
    expect(MockWebSocket.instances).toHaveLength(1);
  });

  it("coalesces controls until capacity returns without replaying rejected input", () => {
    socket.bufferedAmount = sendBudget;
    const latest = new Uint8Array([100, 40]);
    expect(connection.send(1, new Uint8Array([80, 24]))).toBe(true);
    expect(connection.send(1, latest)).toBe(true);
    latest[0] = 0;
    expect(connection.send(2, new Uint8Array())).toBe(true);
    expect(connection.send(2, new Uint8Array())).toBe(true);
    expect(connection.send(0, new Uint8Array([65]))).toBe(false);
    vi.advanceTimersByTime(200);
    expect(socket.sent).toEqual([]);
    expect(vi.getTimerCount()).toBe(1);

    socket.bufferedAmount = sendBudget - 4;
    vi.advanceTimersByTime(50);
    expect(socket.sent).toEqual([
      new Uint8Array([1, 100, 40]),
      new Uint8Array([2]),
    ]);
    expect(socket.bufferedAmount).toBe(sendBudget);
    expect(vi.getTimerCount()).toBe(0);
    socket.bufferedAmount = 0;
    vi.advanceTimersByTime(200);
    expect(socket.sent).toHaveLength(2);
  });

  it("replaces a deferred resize when a newer size can be sent immediately", () => {
    socket.bufferedAmount = sendBudget;
    connection.send(1, new Uint8Array([80, 24]));
    socket.bufferedAmount = 0;
    expect(connection.send(1, new Uint8Array([100, 40]))).toBe(true);
    vi.advanceTimersByTime(100);
    expect(socket.sent).toEqual([new Uint8Array([1, 100, 40])]);
    expect(vi.getTimerCount()).toBe(0);
  });

  it.each(["close", "reconnect"] as const)(
    "discards deferred controls on %s",
    (action) => {
      socket.bufferedAmount = sendBudget;
      connection.send(1, new Uint8Array([80, 24]));
      connection.send(2, new Uint8Array());
      connection[action]();
      socket.bufferedAmount = 0;
      vi.advanceTimersByTime(100);
      expect(socket.sent).toEqual([]);
      expect(
        MockWebSocket.instances.flatMap((instance) => instance.sent),
      ).toEqual([]);
      expect(vi.getTimerCount()).toBe(0);
    },
  );

  it("stops control retries when the server closes the session", () => {
    socket.bufferedAmount = sendBudget;
    connection.send(1, new Uint8Array([80, 24]));
    socket.readyState = 3;
    socket.onclose?.({ code: 4410 });
    vi.advanceTimersByTime(100);
    expect(socket.sent).toEqual([]);
    expect(vi.getTimerCount()).toBe(0);
  });

  it("returns false without sending when the socket is closed", () => {
    connection.close();

    expect(connection.send(0, new Uint8Array([65]))).toBe(false);
    expect(socket.sent).toEqual([]);
  });
});
