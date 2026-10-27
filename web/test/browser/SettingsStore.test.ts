import { afterEach, expect, test, vi } from "vitest";
import { seedSettings } from "./settings-fixture";
import {
  settings,
  saveSettings,
  stopSettings,
  startSettings,
} from "../../src/settings.svelte";
import { emptySettings } from "../../src/settings-intent";

afterEach(() => {
  stopSettings();
  vi.unstubAllGlobals();
  localStorage.clear();
});

test("409 rebases only the local preview field and caches confirmed data", async () => {
  await seedSettings({ preview: { lines: 10, skip: 1 } });
  const requests: unknown[] = [];
  let revision = 1;
  vi.stubGlobal(
    "fetch",
    vi.fn<typeof fetch>(async (_input, init) => {
      const snapshot = {
        revision,
        updatedAt: "2026-10-26T00:00:00Z",
        values: { ...emptySettings, preview: { lines: 10, skip: 4 } },
      };
      if (init?.method !== "PATCH") return Response.json(snapshot);
      const body = JSON.parse(String(init.body));
      requests.push(body);
      if (body.baseRevision !== revision)
        return Response.json({ error: "conflict" }, { status: 409 });
      revision++;
      return Response.json({
        ...snapshot,
        revision,
        values: { ...snapshot.values, ...body.changes },
      });
    }),
  );
  await saveSettings({ preview: { lines: 20, skip: 1 } });
  expect(requests).toEqual([
    { baseRevision: 0, changes: { preview: { lines: 20, skip: 1 } } },
    { baseRevision: 1, changes: { preview: { lines: 20, skip: 4 } } },
  ]);
  expect(settings.values.preview).toEqual({ lines: 20, skip: 4 });
  expect(
    JSON.parse(localStorage.getItem("webmux.settingsCache")!).revision,
  ).toBe(2);
});

test("a failed optimistic save rolls back without caching or replaying it", async () => {
  await seedSettings({ theme: "light" });
  const cache = localStorage.getItem("webmux.settingsCache");
  const response = Promise.withResolvers<Response>();
  const fetch = vi.fn<typeof window.fetch>(() => response.promise);
  vi.stubGlobal("fetch", fetch);
  const task = saveSettings({ theme: "dark" });
  expect(settings.values.theme).toBe("dark");
  expect(localStorage.getItem("webmux.settingsCache")).toBe(cache);
  response.resolve(
    Response.json({ error: "storage /state/settings.json" }, { status: 503 }),
  );
  await expect(task).rejects.toThrow("settings.json");
  expect(settings.values.theme).toBe("light");
  expect(settings.ready).toBe(false);
  expect(fetch).toHaveBeenCalledTimes(1);
  expect(localStorage.getItem("webmux.settingsCache")).toBe(cache);
});

test("a second conflict stops automatic retries and preserves the latest snapshot", async () => {
  await seedSettings();
  const fetch = vi.fn<typeof window.fetch>(async (_url, init) =>
    init?.method === "PATCH"
      ? Response.json({ error: "conflict" }, { status: 409 })
      : Response.json({
          revision: 1,
          updatedAt: "2026-10-26T00:00:00Z",
          values: { ...emptySettings, theme: "light" },
        }),
  );
  vi.stubGlobal("fetch", fetch);
  await expect(saveSettings({ theme: "dark" })).rejects.toThrow("conflict");
  expect(fetch).toHaveBeenCalledTimes(3);
  expect(settings.values.theme).toBe("light");
});

test("a reconnected snapshot replaces stale revisions after a server reset", async () => {
  await seedSettings();
  await saveSettings({ theme: "dark" });
  const sockets: FakeSocket[] = [];
  class FakeSocket {
    static OPEN = 1;
    readyState = 1;
    bufferedAmount = 0;
    binaryType = "";
    onmessage?: (event: { data: string }) => void;
    constructor() {
      sockets.push(this);
    }
    close() {}
    send() {}
  }
  vi.stubGlobal("WebSocket", FakeSocket);
  startSettings();
  await expect.poll(() => settings.ready).toBe(true);
  expect(settings.revision).toBe(1);
  sockets[0].onmessage?.({
    data: JSON.stringify({
      type: "settings",
      settings: { revision: 0, updatedAt: "", values: emptySettings },
    }),
  });
  expect(settings.revision).toBe(0);
  expect(settings.values.theme).toBeNull();
  expect(
    JSON.parse(localStorage.getItem("webmux.settingsCache")!).revision,
  ).toBe(0);
});
