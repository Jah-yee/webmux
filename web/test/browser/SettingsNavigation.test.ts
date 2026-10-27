import { reloadSettings, stopSettings } from "../../src/settings.svelte";
import { emptySettings } from "../../src/settings-intent";
import { afterEach, expect, test, vi } from "vitest";
import { page, userEvent } from "vitest/browser";
import { cleanup, render } from "vitest-browser-svelte/pure";
import Settings from "../../src/pages/Settings.svelte";

const originalURL = location.href;

afterEach(async () => {
  vi.restoreAllMocks();
  await cleanup();
  await userEvent.cleanup();
  vi.unstubAllGlobals();
  history.replaceState(null, "", originalURL);
  localStorage.clear();
  await page.viewport(1024, 900);
});

function stubBackend() {
  vi.stubGlobal(
    "fetch",
    vi.fn<typeof fetch>(async (input) => {
      const path = new URL(String(input), location.href).pathname;
      if (path === "/api/settings")
        return Response.json({
          revision: 0,
          updatedAt: "",
          values: emptySettings,
        });
      if (path === "/api/launch-configs") return Response.json([]);
      if (path === "/api/config") return Response.json({ defaultCwd: "/tmp" });
      return Response.json({ error: "unavailable" }, { status: 503 });
    }),
  );
}

async function openSettings() {
  stubBackend();
  stopSettings();
  await reloadSettings();
  stopSettings();
  await reloadSettings();
  render(Settings);
}

for (const width of [1280, 393]) {
  test(`section links stay reachable and preserve editing at ${width}px`, async () => {
    await page.viewport(width, 851);
    await openSettings();
    const nav = page.getByRole("navigation", { name: "设置分区" });
    const current = () =>
      nav.element().querySelector("a[data-active]")?.getAttribute("href");
    const first = nav.getByRole("link", { name: "快捷指令" });
    await expect.element(first).toBeVisible();
    expect(first.element().getBoundingClientRect().left).toBeGreaterThanOrEqual(
      0,
    );

    const input = page.getByRole("spinbutton", {
      name: "显示行数",
      exact: true,
    });
    const originalInput = input.element();
    await input.fill("");
    await nav.getByRole("link", { name: "公网访问", exact: true }).click();
    await expect.poll(current).toBe("#access");
    await expect.poll(() => document.activeElement?.id).toBe("access");
    expect(input.element()).toBe(originalInput);
    await expect.element(input).toHaveValue(null);
    if (width < 1024) {
      expect(
        document.getElementById("access")!.getBoundingClientRect().top,
      ).toBeGreaterThanOrEqual(nav.element().getBoundingClientRect().bottom);
    }
    expect(document.documentElement.scrollWidth).toBeLessThanOrEqual(
      innerWidth,
    );
    expect(
      document.querySelector<HTMLElement>(".settings-navigation")!.style.top,
    ).toBe("");

    document.getElementById("preview")!.scrollIntoView();
    await expect.poll(current).toBe("#preview");
    await nav.getByRole("link", { name: "快捷指令" }).click();
    await expect.poll(current).toBe("#quick-commands");
  });
}

test("standalone navigation follows the measured header height", async () => {
  await page.viewport(393, 851);
  const nativeMatchMedia = window.matchMedia.bind(window);
  vi.spyOn(window, "matchMedia").mockImplementation(
    (query) =>
      ({
        matches:
          query === "(display-mode: standalone)"
            ? true
            : nativeMatchMedia(query).matches,
        media: query,
        onchange: null,
        addEventListener: () => {},
        removeEventListener: () => {},
        addListener: () => {},
        removeListener: () => {},
        dispatchEvent: () => false,
      }) as MediaQueryList,
  );
  await openSettings();
  const nav = page.getByRole("navigation", { name: "设置分区" });
  await expect
    .element(nav.getByRole("link", { name: "快捷指令" }))
    .toBeVisible();
  const header = document.querySelector<HTMLElement>(
    ".safe-area-page > .app-header",
  )!;
  const navigation = document.querySelector<HTMLElement>(
    ".settings-navigation",
  )!;
  await expect
    .poll(() =>
      Math.round(
        parseFloat(navigation.style.top) -
          header.getBoundingClientRect().height,
      ),
    )
    .toBe(0);
});
