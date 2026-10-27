import { vi } from "vitest";
import { reloadSettings, stopSettings } from "../../src/settings.svelte";
import { emptySettings } from "../../src/settings-intent";
import type { SettingsValues } from "../../src/schemas";

// Component fixtures use the same GET/PATCH boundary as the application.
export async function seedSettings(values: Partial<SettingsValues> = {}) {
  stopSettings();
  let snapshot = {
    revision: 0,
    updatedAt: "",
    values: { ...emptySettings, ...values },
  };
  vi.stubGlobal(
    "fetch",
    vi.fn<typeof fetch>(async (input, init) => {
      if (String(input) !== "/api/settings")
        throw new Error(`Unexpected request ${String(input)}`);
      if (init?.method === "PATCH") {
        const patch = JSON.parse(String(init.body));
        if (patch.baseRevision !== snapshot.revision)
          return Response.json({ error: "conflict" }, { status: 409 });
        snapshot = {
          revision: snapshot.revision + 1,
          updatedAt: new Date().toISOString(),
          values: { ...snapshot.values, ...patch.changes },
        };
      }
      return Response.json(snapshot);
    }),
  );
  await reloadSettings();
  vi.mocked(fetch).mockClear();
}
