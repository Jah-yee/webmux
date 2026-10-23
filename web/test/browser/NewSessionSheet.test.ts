import { afterEach, beforeEach, expect, test, vi } from "vitest";
import { page, userEvent } from "vitest/browser";
import { cleanup, render } from "vitest-browser-svelte/pure";
import NewSessionSheet from "../../src/components/NewSessionSheet.svelte";
import type { LaunchConfigsState } from "../../src/launchConfigs";
import type { Preset, SessionInfo } from "../../src/schemas";

const presets: Preset[] = [
  {
    name: "shell",
    label: "Shell",
    cmd: ["bash", "-l", "-i"],
    allowArgs: false,
  },
  { name: "codex", label: "Codex", cmd: ["codex"], allowArgs: true },
];
const config = {
  id: "abcdefg2",
  name: "Claude 恢复",
  cmd: ["claude", "--resume"],
  envNames: ["API_KEY", "BASE_URL"],
  createdAt: "2026-10-23T00:00:00Z",
  updatedAt: "2026-10-23T00:00:00Z",
};
const session: SessionInfo = {
  id: "sessio23",
  generation: "0123456789abcdef",
  title: "",
  cwd: "/work",
  preset: "custom",
  cmd: ["claude", "--resume"],
  status: "running",
  exitCode: null,
  preview: "",
  createdAt: "2026-10-23T00:00:00Z",
  lastActiveAt: "2026-10-23T00:00:00Z",
  cols: 80,
  rows: 24,
  clients: 0,
  workspace: null,
  attention: null,
  reportedCwd: null,
};

let created: unknown[] = [];
const origin = location.pathname + location.search;

beforeEach(() => {
  localStorage.clear();
  created = [];
  vi.stubGlobal(
    "fetch",
    vi.fn<typeof fetch>(async (input, init) => {
      const path = new URL(String(input), location.href).pathname;
      if (path === "/api/sessions" && init?.method === "POST") {
        created.push(JSON.parse(String(init.body)));
        return Response.json(session, { status: 201 });
      }
      // Directory suggestions are irrelevant to these launch-choice tests.
      return Response.json({ error: "not found" }, { status: 404 });
    }),
  );
});

afterEach(async () => {
  try {
    await cleanup();
    await userEvent.cleanup();
  } finally {
    vi.unstubAllGlobals();
    vi.restoreAllMocks();
    localStorage.clear();
    history.replaceState(null, "", origin);
  }
});

function open(launchConfigs: LaunchConfigsState) {
  return render(NewSessionSheet, {
    presets,
    launchConfigs,
    defaultCwd: "/work",
    onchange: () => {},
    onworktreecreated: () => {},
    onclose: () => {},
  });
}
const submit = () =>
  page.getByRole("button", { name: "创建并进入 →", exact: true });
const pressed = (name: string) =>
  expect
    .element(page.getByRole("button", { name, exact: true }))
    .toHaveAttribute("aria-pressed", "true");

test("shows both groups and starts a saved config with only configId", async () => {
  open({ status: "ready", configs: [config] });
  await expect.element(page.getByText("内置预设")).toBeVisible();
  await expect.element(page.getByText("我的启动配置")).toBeVisible();
  await pressed("Shell");
  const option = page.getByRole("button", { name: "启动配置 Claude 恢复" });
  await expect.element(option).toMatchTextContent(/claude --resume/);
  await expect.element(option).toMatchTextContent(/2 个环境变量/);
  await option.click();
  await pressed("启动配置 Claude 恢复");
  await submit().click();
  await vi.waitFor(() => expect(created).toHaveLength(1));
  expect(created[0]).toEqual({ cwd: "/work", configId: "abcdefg2", name: "" });
  expect(localStorage.getItem("webmux.lastLaunch")).toBe("config:abcdefg2");
});

test("built-in presets still submit preset and args", async () => {
  open({ status: "ready", configs: [config] });
  await page.getByRole("button", { name: "Codex", exact: true }).click();
  await page.getByLabelText("追加参数").fill("--model 'o 3'");
  await submit().click();
  await vi.waitFor(() => expect(created).toHaveLength(1));
  expect(created[0]).toEqual({
    cwd: "/work",
    preset: "codex",
    args: ["--model", "o 3"],
    name: "",
  });
  expect(localStorage.getItem("webmux.lastLaunch")).toBe("preset:codex");
});

test("migrates lastPreset to lastLaunch and removes the old key", async () => {
  localStorage.setItem("webmux.lastPreset", "codex");
  open({ status: "ready", configs: [] });
  await pressed("Codex");
  expect(localStorage.getItem("webmux.lastLaunch")).toBe("preset:codex");
  expect(localStorage.getItem("webmux.lastPreset")).toBeNull();
});

test("a legacy custom choice opens the temporary command", async () => {
  localStorage.setItem("webmux.lastPreset", "custom");
  localStorage.setItem("webmux.customCommand", "htop -d 5");
  open({ status: "ready", configs: [] });
  await pressed("自定义");
  await expect
    .element(page.getByLabelText("自定义启动命令"))
    .toHaveValue("htop -d 5");
  await submit().click();
  await vi.waitFor(() => expect(created).toHaveLength(1));
  expect(created[0]).toEqual({
    cwd: "/work",
    preset: "custom",
    args: [],
    cmd: ["htop", "-d", "5"],
    name: "",
  });
  expect(localStorage.getItem("webmux.lastLaunch")).toBe("custom");
});

test("a remembered config that no longer exists falls back to the first preset", async () => {
  localStorage.setItem("webmux.lastLaunch", "config:zzzzzzz2");
  open({ status: "ready", configs: [config] });
  await pressed("Shell");
});

test("an empty config list links to the settings card", async () => {
  open({ status: "ready", configs: [] });
  await expect
    .element(page.getByRole("link", { name: "前往设置创建启动配置" }))
    .toHaveAttribute("href", "/settings#launch-configs");
});

test("an unavailable store shows the reason and presets still start", async () => {
  localStorage.setItem("webmux.lastLaunch", "config:abcdefg2");
  open({
    status: "unavailable",
    error: "启动配置不可用: 文件损坏（/state/launch-configs.json）",
  });
  await expect
    .element(page.getByText(/启动配置暂不可用：.*launch-configs\.json/))
    .toBeVisible();
  await pressed("Shell");
  await submit().click();
  await vi.waitFor(() => expect(created).toHaveLength(1));
  expect(created[0]).toEqual({
    cwd: "/work",
    preset: "shell",
    args: [],
    name: "",
  });
});

test("the config group is not selectable while loading", async () => {
  open({ status: "loading" });
  await expect.element(page.getByText("正在读取启动配置…")).toBeVisible();
  expect(
    page.getByRole("button", { name: /^启动配置 / }).elements(),
  ).toHaveLength(0);
  await expect.element(submit()).toBeEnabled();
});

test("the mobile drawer does not overflow horizontally", async () => {
  await page.viewport(390, 844);
  try {
    open({
      status: "ready",
      configs: [
        {
          ...config,
          name: "很长的启动配置名称".repeat(6),
          cmd: ["bash", "-lc", "x".repeat(300)],
        },
      ],
    });
    const option = page.getByRole("button", { name: /^启动配置 / });
    await expect.element(option).toBeVisible();
    const scroller = option.element().closest(".overflow-y-auto")!;
    const drawer = option.element().closest("[data-slot=drawer-content]")!;
    expect(drawer).toBeTruthy();
    expect(scroller.scrollWidth).toBeLessThanOrEqual(scroller.clientWidth);
    expect(drawer.scrollWidth).toBeLessThanOrEqual(drawer.clientWidth);
  } finally {
    await page.viewport(1024, 900);
  }
});
