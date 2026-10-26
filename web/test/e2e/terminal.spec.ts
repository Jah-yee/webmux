import { setTimeout as delay } from "node:timers/promises";
import type { Page } from "@playwright/test";
import {
  test,
  expect,
  createShell,
  runCommand,
  expectTerminalLine,
  expectConnected,
} from "./fixtures";

type Point = { x: number; y: number };

async function textPoint(
  page: Page,
  line: string,
  text: string,
): Promise<Point> {
  return page.locator(".xterm-rows > div").evaluateAll(
    (rows, target) => {
      const row = rows.find(
        (element) =>
          element.textContent?.replaceAll("\u00a0", " ").trimEnd() ===
          target.line,
      );
      if (!row) throw new Error(`Missing rendered line: ${target.line}`);
      const walker = document.createTreeWalker(row, NodeFilter.SHOW_TEXT);
      const nodes: Node[] = [];
      for (let node = walker.nextNode(); node; node = walker.nextNode()) {
        nodes.push(node);
      }
      const text = nodes.map((node) => node.textContent).join("");
      const start = text.indexOf(target.text);
      if (start < 0) throw new Error(`Missing rendered text: ${target.text}`);
      const end = start + target.text.length;
      const range = document.createRange();
      let offset = 0;
      for (const node of nodes) {
        const length = node.textContent?.length ?? 0;
        if (start >= offset && start < offset + length)
          range.setStart(node, start - offset);
        if (end > offset && end <= offset + length) {
          range.setEnd(node, end - offset);
          break;
        }
        offset += length;
      }
      const bounds = range.getBoundingClientRect();
      return {
        x: bounds.x + bounds.width * 0.75,
        y: bounds.y + bounds.height / 2,
      };
    },
    { line, text },
  );
}

async function longPress(page: Page, start: Point, end?: Point) {
  const cdp = await page.context().newCDPSession(page);
  try {
    await cdp.send("Input.dispatchTouchEvent", {
      type: "touchStart",
      touchPoints: [{ ...start, id: 1 }],
    });
    await delay(650);
    if (end)
      await cdp.send("Input.dispatchTouchEvent", {
        type: "touchMove",
        touchPoints: [{ ...end, id: 1 }],
      });
    await cdp.send("Input.dispatchTouchEvent", {
      type: "touchEnd",
      touchPoints: [],
    });
  } finally {
    await cdp.detach();
  }
}

test("dark theme corrects explicit black ANSI input and survives reload", async ({
  page,
  workDir,
}) => {
  await page.addInitScript(() => localStorage.setItem("webmux.theme", "dark"));
  await createShell(page, "Dark contrast", workDir);
  await runCommand(
    page,
    "printf '\\033[38;2;0;0;0m%s %s\\033[0m\\n' 'dark-input' '中文'",
  );
  await expectTerminalLine(page, "dark-input 中文");
  const contrast = () =>
    page.locator(".xterm-rows > div").evaluateAll((rows) => {
      const row = rows.find(
        (element) =>
          element.textContent?.replaceAll("\u00a0", " ").trimEnd() ===
          "dark-input 中文",
      );
      // Font fallback can make xterm use a span per cell instead of one run.
      const spans = [...(row?.querySelectorAll("span") ?? [])].filter(
        (element) => element.textContent?.trim(),
      );
      if (!spans.length) return 0;
      // Right after a reload the styles may not apply yet; keep polling.
      const backgroundRGB = getComputedStyle(
        document.querySelector(".terminal-area")!,
      )
        .backgroundColor.match(/\d+/g)
        ?.slice(0, 3);
      if (!backgroundRGB) return 0;
      const luminance = (rgb: string[]) => {
        const [red, green, blue] = rgb.map((value) => {
          const channel = Number(value) / 255;
          return channel <= 0.04045
            ? channel / 12.92
            : ((channel + 0.055) / 1.055) ** 2.4;
        });
        return red * 0.2126 + green * 0.7152 + blue * 0.0722;
      };
      const background = luminance(backgroundRGB);
      return Math.min(
        ...spans.map((span) => {
          const foregroundRGB = getComputedStyle(span)
            .color.match(/\d+/g)
            ?.slice(0, 3);
          if (!foregroundRGB) return 0;
          const foreground = luminance(foregroundRGB);
          return (
            (Math.max(foreground, background) + 0.05) /
            (Math.min(foreground, background) + 0.05)
          );
        }),
      );
    });
  await expect.poll(contrast).toBeGreaterThanOrEqual(4.49);
  await page.reload();
  await expectTerminalLine(page, "dark-input 中文");
  await expect.poll(contrast).toBeGreaterThanOrEqual(4.49);
});

test("mobile long press selects wide characters and drag copies multiple lines without typing", async ({
  page,
  context,
  request,
  workDir,
}, testInfo) => {
  test.skip(
    !testInfo.project.use.isMobile,
    "Native touch selection requires the mobile project",
  );
  await context.grantPermissions(["clipboard-read", "clipboard-write"]);
  const id = await createShell(page, "Touch selection", workDir);
  await runCommand(
    page,
    "printf '%s %s %s\\n%s %s\\n' 'prefix' '中文' 'tail' 'second' 'line'",
  );
  await expectTerminalLine(page, "prefix 中文 tail");
  await expectTerminalLine(page, "second line");
  await page
    .locator(".xterm-helper-textarea")
    .evaluate((element) => (element as HTMLTextAreaElement).blur());
  const buffer = await (await request.get(`/api/sessions/${id}/buffer`)).text();
  const start = await textPoint(page, "prefix 中文 tail", "中");
  await longPress(page, start);
  await expect(
    page.getByRole("button", { name: "复制选中", exact: true }),
  ).toBeVisible();
  await expect(page.locator(".xterm-helper-textarea")).not.toBeFocused();
  await page.getByRole("button", { name: "复制选中", exact: true }).click();
  await expect
    .poll(() => page.evaluate(() => navigator.clipboard.readText()))
    .toBe("中文");
  const end = await textPoint(page, "second line", "d");
  await longPress(page, start, end);
  await page.getByRole("button", { name: "复制选中", exact: true }).click();
  await expect
    .poll(() => page.evaluate(() => navigator.clipboard.readText()))
    .toBe("中文 tail\nsecond");
  await testInfo.attach("mobile-selection.png", {
    body: await page.screenshot(),
    contentType: "image/png",
  });
  await expect(page.locator(".xterm-helper-textarea")).not.toBeFocused();
  expect(await (await request.get(`/api/sessions/${id}/buffer`)).text()).toBe(
    buffer,
  );
  await page.getByRole("button", { name: "取消选择", exact: true }).click();
  await expect(
    page.getByRole("toolbar", { name: "终端文字选择" }),
  ).toBeHidden();
});

test("mobile denied clipboard exposes only selected text for native copying", async ({
  page,
  workDir,
}, testInfo) => {
  test.skip(
    !testInfo.project.use.isMobile,
    "Native touch selection requires the mobile project",
  );
  await page.addInitScript(() => {
    Object.defineProperty(navigator.clipboard, "writeText", {
      value: () =>
        Promise.reject(
          new DOMException("Permission denied", "NotAllowedError"),
        ),
    });
  });
  await createShell(page, "Clipboard fallback", workDir);
  await runCommand(page, "printf '%s %s\\n' 'prefix' 'selected_word'");
  await expectTerminalLine(page, "prefix selected_word");
  await longPress(
    page,
    await textPoint(page, "prefix selected_word", "selected_word"),
  );
  await page.getByRole("button", { name: "复制选中", exact: true }).click();
  await expect(
    page.getByRole("dialog", { name: "复制终端文字" }),
  ).toBeVisible();
  await expect(page.getByRole("textbox", { name: "复制内容" })).toHaveValue(
    "selected_word",
  );
});

function osc52Command(text: string) {
  const payload = Buffer.from(text, "utf8").toString("base64");
  return `printf '\\033]52;c;${payload}\\a'`;
}

test("application OSC 52 copy is written to the browser clipboard", async ({
  page,
  context,
  workDir,
}) => {
  await context.grantPermissions(["clipboard-read", "clipboard-write"]);
  await createShell(page, "OSC 52 copy", workDir);
  await runCommand(page, osc52Command("osc52-中文"));
  await expect
    .poll(() => page.evaluate(() => navigator.clipboard.readText()))
    .toBe("osc52-中文");
  await expect(page.locator(".xterm-rows")).toContainText("printf");
  await expect(page.locator(".xterm-rows")).not.toContainText("osc52-中文");
});

test("denied OSC 52 copy exposes the text for manual copying", async ({
  page,
  workDir,
}) => {
  await page.addInitScript(() => {
    Object.defineProperty(navigator.clipboard, "writeText", {
      configurable: true,
      value: () =>
        Promise.reject(
          new DOMException("Permission denied", "NotAllowedError"),
        ),
    });
  });
  await createShell(page, "OSC 52 fallback", workDir);
  await runCommand(page, osc52Command("osc52_fallback"));
  await expect(
    page.getByRole("dialog", { name: "复制终端文字" }),
  ).toBeVisible();
  await expect(page.getByRole("textbox", { name: "复制内容" })).toHaveValue(
    "osc52_fallback",
  );
});

test("closed OSC 52 copy dialog does not reopen when the terminal is entered again", async ({
  page,
  workDir,
}) => {
  await page.addInitScript(() => {
    Object.defineProperty(navigator.clipboard, "writeText", {
      configurable: true,
      value: () =>
        Promise.reject(
          new DOMException("Permission denied", "NotAllowedError"),
        ),
    });
  });
  const name = "OSC 52 re-entry";
  const id = await createShell(page, name, workDir);
  await runCommand(page, `${osc52Command("osc52_first")}; echo first-done`);
  const dialog = page.getByRole("dialog", { name: "复制终端文字" });
  const text = page.getByRole("textbox", { name: "复制内容" });
  await expect(text).toHaveValue("osc52_first");
  await page.getByRole("button", { name: "关闭", exact: true }).click();
  await expect(dialog).toBeHidden();

  await page.getByRole("button", { name: "返回会话列表", exact: true }).click();
  await expect(page).toHaveURL(/\/$/);
  await page.goto(`/s/${id}`);
  await expectConnected(page);
  await expect(page.locator(".restore-overlay")).toBeHidden();
  await expectTerminalLine(page, "first-done");
  await expect(dialog).toBeHidden();

  // Live copies after re-entry still open the dialog.
  await runCommand(page, osc52Command("osc52_second"));
  await expect(text).toHaveValue("osc52_second");
});

test("long denied copy keeps the dialog inside the viewport and scrolls its text", async ({
  page,
  workDir,
}) => {
  await page.addInitScript(() => {
    Object.defineProperty(navigator.clipboard, "writeText", {
      configurable: true,
      value: () =>
        Promise.reject(
          new DOMException("Permission denied", "NotAllowedError"),
        ),
    });
  });
  await createShell(page, "OSC 52 long fallback", workDir);
  await runCommand(
    page,
    `printf '\\033]52;c;%s\\a' "$(seq 1 400 | base64 | tr -d '\\n')"`,
  );
  const dialog = page.getByRole("dialog", { name: "复制终端文字" });
  const text = page.getByRole("textbox", { name: "复制内容" });
  await expect(dialog).toBeVisible();
  await expect(text).toHaveValue(
    Array.from({ length: 400 }, (_, i) => `${i + 1}\n`).join(""),
  );
  await expect(dialog).toBeInViewport({ ratio: 1 });
  await expect(page.getByRole("button", { name: "关闭" })).toBeInViewport({
    ratio: 1,
  });
  const scroll = await text.evaluate((element) => {
    element.scrollTop = element.scrollHeight;
    return {
      overflows: element.scrollHeight > element.clientHeight,
      atEnd:
        element.scrollTop + element.clientHeight >= element.scrollHeight - 1,
    };
  });
  expect(scroll).toEqual({ overflows: true, atEnd: true });
});

test("terminal find searches real output without input or resize, copies and returns to the shell", async ({
  page,
  context,
  request,
  workDir,
}, testInfo) => {
  await context.grantPermissions(["clipboard-read", "clipboard-write"]);
  const frames: Buffer[] = [];
  page.on("websocket", (socket) =>
    socket.on("framesent", ({ payload }) => {
      frames.push(Buffer.isBuffer(payload) ? payload : Buffer.from(payload));
    }),
  );
  const id = await createShell(page, "Find retained output", workDir);
  // Split the marker so the echoed command is not a second match.
  await runCommand(
    page,
    "printf '\\033[31m%s%s\\033[0m\\n' 'UNIQUE_' 'FIND_ERROR'",
  );
  await expectTerminalLine(page, "UNIQUE_FIND_ERROR");
  const retained = await (
    await request.get(`/api/sessions/${id}/buffer`)
  ).text();
  const grid = await page.locator(".xterm-screen").boundingBox();
  const baseline = frames.length;
  if (testInfo.project.use.isMobile) {
    // The shared menu also works while the narrow layout shows code instead.
    await page.getByRole("button", { name: "代码", exact: true }).click();
    await page.getByRole("button", { name: "终端菜单" }).click();
    await page.getByRole("menuitem", { name: "查找", exact: true }).click();
  } else {
    await page.locator(".xterm-helper-textarea").focus();
    await page.keyboard.press("Control+Shift+F");
  }
  const query = page.getByRole("textbox", {
    name: "查找终端输出",
    exact: true,
  });
  await expect(query).toBeFocused();
  await query.fill("UNIQUE_FIND_ERROR");
  await expect(page.getByRole("status", { name: "查找结果" })).toHaveText(
    "1/1",
  );
  await page.getByRole("button", { name: "复制选中", exact: true }).click();
  await expect
    .poll(() => page.evaluate(() => navigator.clipboard.readText()))
    .toBe("UNIQUE_FIND_ERROR");
  await expect(
    page.getByRole("toolbar", { name: "终端文字选择" }),
  ).toBeHidden();
  await page.getByRole("button", { name: "关闭查找" }).click();
  await expect(query).toBeHidden();
  if (testInfo.project.use.isMobile)
    await expect(page.locator(".xterm-helper-textarea")).not.toBeFocused();
  else await expect(page.locator(".xterm-helper-textarea")).toBeFocused();
  expect(await page.locator(".xterm-screen").boundingBox()).toEqual(grid);
  expect(
    frames.slice(baseline).filter((frame) => frame[0] === 0 || frame[0] === 1),
  ).toEqual([]);
  expect(await (await request.get(`/api/sessions/${id}/buffer`)).text()).toBe(
    retained,
  );
  await runCommand(page, "printf '%s%s\\n' 'AFTER_' 'FIND_OK'");
  await expectTerminalLine(page, "AFTER_FIND_OK");
});

test("terminal find denied copy retains only readable selection and restores find focus", async ({
  page,
  workDir,
}, testInfo) => {
  await page.addInitScript(() => {
    Object.defineProperty(navigator.clipboard, "writeText", {
      configurable: true,
      value: () =>
        Promise.reject(
          new DOMException("Permission denied", "NotAllowedError"),
        ),
    });
  });
  await createShell(page, "Find copy fallback", workDir);
  await runCommand(
    page,
    "printf '\\033[31m%s%s\\033[0m\\n' 'READABLE_' 'ERROR'",
  );
  await expectTerminalLine(page, "READABLE_ERROR");
  await page.getByRole("button", { name: "终端菜单" }).click();
  await page.getByRole("menuitem", { name: "查找", exact: true }).click();
  const query = page.getByRole("textbox", {
    name: "查找终端输出",
    exact: true,
  });
  await query.fill("READABLE_ERROR");
  await page.getByRole("button", { name: "复制选中", exact: true }).click();
  const copied = page.getByRole("textbox", { name: "复制内容" });
  await expect(copied).toHaveValue("READABLE_ERROR");
  await expect(copied).toHaveAttribute("readonly");
  await page.getByRole("button", { name: "关闭", exact: true }).click();
  if (!testInfo.project.use.isMobile) await expect(query).toBeFocused();
  await expect(page.locator(".xterm-helper-textarea")).not.toBeFocused();
  await page.getByRole("button", { name: "关闭查找" }).click();
});

test("draft shortcut does not modify or submit the draft", async ({
  page,
  workDir,
}, testInfo) => {
  test.skip(!!testInfo.project.use.isMobile, "Desktop focus restoration");
  await createShell(page, "Draft find isolation", workDir);
  await runCommand(page, "printf '%s%s\\n' 'DRAFT_' 'FIND_TARGET'");
  await expectTerminalLine(page, "DRAFT_FIND_TARGET");
  await page.getByRole("button", { name: "草稿模式", exact: true }).click();
  const draft = page.locator(".draft-pane textarea").first();
  await draft.fill("keep my unsent draft");
  await draft.press("Control+Shift+F");
  const query = page.getByRole("textbox", {
    name: "查找终端输出",
    exact: true,
  });
  await expect(query).toBeFocused();
  await query.fill("DRAFT_FIND_TARGET");
  await expect(page.getByRole("status", { name: "查找结果" })).toHaveText(
    "1/1",
  );
  await query.press("Escape");
  await expect(draft).toHaveValue("keep my unsent draft");
  await expect(draft).toBeFocused();
});
