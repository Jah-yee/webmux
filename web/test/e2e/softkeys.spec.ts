import { test, expect, createShell, expectTerminalLine } from "./fixtures";

test("mobile soft keys stay in one row with paste visible and all keys reachable", async ({
  page,
  workDir,
}, testInfo) => {
  test.skip(
    !testInfo.project.use.isMobile,
    "Soft keys are shown on touch devices",
  );
  await page.addInitScript(() => {
    Object.defineProperty(navigator.clipboard, "readText", {
      value: () =>
        Promise.reject(
          new DOMException("Permission denied", "NotAllowedError"),
        ),
    });
  });
  await createShell(page, "Single-row soft keys", workDir);
  const bar = page.getByRole("complementary", { name: "终端软键" });
  const paste = bar.getByRole("button", { name: "粘贴", exact: true });
  const keyboard = bar.getByRole("button", { name: "唤起或收起键盘" });
  const quick = bar.getByRole("button", { name: "快捷指令", exact: true });
  for (const width of [320, 390, 844]) {
    await page.setViewportSize({ width, height: 844 });
    await expect(paste).toBeInViewport({ ratio: 1 });
    await expect(keyboard).toBeInViewport({ ratio: 1 });
    await expect(quick).toBeInViewport({ ratio: 1 });
    await expect(
      page
        .locator(".terminal-header")
        .getByRole("button", { name: "快捷指令" }),
    ).toBeHidden();
    const edges = await bar.evaluate((element) => ({
      row: element.querySelector(".soft-row")!.getBoundingClientRect().right,
      quick: element
        .querySelector(".soft-trailing button")!
        .getBoundingClientRect().left,
    }));
    expect(
      edges.quick,
      "Quick commands sit right of the soft keys",
    ).toBeGreaterThanOrEqual(edges.row);
    const layout = await bar.evaluate((element) => {
      const buttons = [...element.querySelectorAll("button")].map((button) =>
        button.getBoundingClientRect(),
      );
      return {
        height: element.getBoundingClientRect().height,
        verticalSpread:
          Math.max(...buttons.map((bounds) => bounds.top)) -
          Math.min(...buttons.map((bounds) => bounds.top)),
        pageOverflow: document.documentElement.scrollWidth - innerWidth,
      };
    });
    expect(
      layout.verticalSpread,
      "All soft keys share one row",
    ).toBeLessThanOrEqual(1);
    expect(
      layout.height,
      "The toolbar must not consume two button rows",
    ).toBeLessThan(64);
    expect(layout.pageOverflow).toBe(0);
  }
  await page.setViewportSize({ width: 320, height: 844 });
  const before = await quick.boundingBox();
  await bar
    .getByRole("button", { name: "Ctrl+R", exact: true })
    .scrollIntoViewIfNeeded();
  expect(await quick.boundingBox()).toEqual(before);
  await expect(quick).toBeInViewport({ ratio: 1 });
  // Scroll offsets snap to device pixels (DPR 2.625 here), so the last key can
  // stay clipped by a fraction of a CSS pixel at maximum scroll.
  await expect(
    bar.getByRole("button", { name: "Ctrl+R", exact: true }),
  ).toBeInViewport({ ratio: 0.99 });
  await paste.scrollIntoViewIfNeeded();
  await expect(paste).toBeInViewport({ ratio: 1 });
  await keyboard.click();
  await expect(page.locator(".xterm-helper-textarea")).toBeFocused();
  await paste.click();
  await expect(page.getByRole("dialog", { name: "粘贴到终端" })).toBeVisible();
});

test("mobile terminal taps leave input unfocused and only the keyboard button toggles focus", async ({
  page,
  workDir,
}, testInfo) => {
  test.skip(!testInfo.project.use.isMobile, "Requires native touch input");
  await createShell(page, "Explicit mobile keyboard", workDir);
  const input = page.locator(".xterm-helper-textarea");
  const screen = page.locator(".xterm-screen");
  const bar = page.getByRole("complementary", { name: "终端软键" });
  const keyboard = bar.getByRole("button", { name: "唤起或收起键盘" });

  await expect(input).not.toBeFocused();
  // Locator.tap uses the browser's native touch path, including compatibility
  // mouse events; dispatching just pointerup would miss accidental tap focus.
  await screen.tap();
  await expect(input).not.toBeFocused();
  await keyboard.tap();
  await expect(input).toBeFocused();
  await page.keyboard.type("printf 'mobile-keyboard-%s\\n' ready");
  await page.keyboard.press("Enter");
  await expectTerminalLine(page, "mobile-keyboard-ready");
  // Soft keys must not dismiss an open keyboard: the direction key sends its
  // sequence without moving focus off the terminal input.
  await bar.getByRole("button", { name: "←", exact: true }).tap();
  await expect(input).toBeFocused();
  await keyboard.tap();
  await expect(input).not.toBeFocused();
  await screen.tap();
  await expect(input).not.toBeFocused();
});

test("touch tablets with a fine primary pointer only raise the keyboard from the keyboard button", async ({
  page,
  workDir,
}, testInfo) => {
  test.skip(!testInfo.project.use.isMobile, "Requires a touch screen");
  // Many tablets (stylus, keyboard covers, Android desktop modes) report a
  // fine primary pointer while still raising a virtual keyboard on focus.
  await page.addInitScript(() => {
    const native = window.matchMedia.bind(window);
    window.matchMedia = (query: string) => {
      const result = native(query);
      if (
        !query.includes("any-pointer") &&
        /pointer: (fine|coarse)/.test(query)
      )
        Object.defineProperty(result, "matches", {
          value: query.includes("pointer: fine"),
        });
      return result;
    };
  });
  const id = await createShell(page, "Tablet keyboard", workDir);
  const input = page.locator(".xterm-helper-textarea");
  const keyboard = page
    .getByRole("complementary", { name: "终端软键" })
    .getByRole("button", { name: "唤起或收起键盘" });

  await expect(keyboard).toBeVisible();
  await expect(input).not.toBeFocused();
  await page.goto("/");
  await page.locator(`a[href="/s/${id}"]`).first().tap();
  await expect(page.locator(".restore-overlay")).toBeHidden();
  await expect(input).not.toBeFocused();

  // A stylus tap arrives as a pen pointer, not a touch pointer.
  const bounds = (await page.locator(".xterm-screen").boundingBox())!;
  const cdp = await page.context().newCDPSession(page);
  const pen = {
    x: bounds.x + bounds.width / 2,
    y: bounds.y + bounds.height / 2,
    button: "left" as const,
    clickCount: 1,
    pointerType: "pen" as const,
  };
  await cdp.send("Input.dispatchMouseEvent", { type: "mousePressed", ...pen });
  await cdp.send("Input.dispatchMouseEvent", { type: "mouseReleased", ...pen });
  await cdp.detach();
  await expect(input).not.toBeFocused();

  await page.getByRole("button", { name: "终端菜单", exact: true }).tap();
  await page.getByRole("menuitem", { name: "字体大小", exact: true }).tap();
  const dialog = page.getByRole("dialog", { name: "字体大小" });
  await expect(dialog).toBeVisible();
  await page.keyboard.press("Escape");
  await expect(dialog).toBeHidden();
  await expect(input).not.toBeFocused();

  await keyboard.tap();
  await expect(input).toBeFocused();
});

test("desktop devices can force the soft-key toolbar on and off through the terminal menu", async ({
  page,
  workDir,
}, testInfo) => {
  test.skip(testInfo.project.use.isMobile, "Exercises a non-coarse pointer");
  await createShell(page, "Manual soft-key toolbar", workDir);
  const bar = page.getByRole("complementary", { name: "终端软键" });
  const menu = page.getByRole("button", { name: "终端菜单", exact: true });
  const override = page.getByRole("menuitemcheckbox", {
    name: "强制显示底部工具栏",
    exact: true,
  });
  const input = page.locator(".xterm-helper-textarea");
  const keyboard = bar.getByRole("button", { name: "唤起或收起键盘" });

  await expect(bar).toBeHidden();
  await menu.click();
  await expect(override).not.toBeChecked();
  await override.click();
  await page.keyboard.press("Escape");
  await expect(bar).toBeVisible();
  await expect(keyboard).toBeInViewport({ ratio: 1 });
  await keyboard.click();
  await expect(input).toBeFocused();
  await page.keyboard.type("printf 'manual-toolbar-%s\\n' ready");
  await page.keyboard.press("Enter");
  await expectTerminalLine(page, "manual-toolbar-ready");
  await keyboard.click();
  await expect(input).not.toBeFocused();

  await menu.click();
  await expect(override).toBeChecked();
  await override.click();
  await page.keyboard.press("Escape");
  await expect(bar).toBeHidden();
  await menu.click();
  await expect(override).not.toBeChecked();
  await page.keyboard.press("Escape");
});
