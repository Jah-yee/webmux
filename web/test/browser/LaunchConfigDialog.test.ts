import { afterEach, expect, test, vi } from "vitest";
import { page, userEvent } from "vitest/browser";
import { cleanup, render } from "vitest-browser-svelte/pure";
import LaunchConfigDialog from "../../src/components/LaunchConfigDialog.svelte";
import type { LaunchConfig } from "../../src/schemas";

const saved: LaunchConfig = {
  id: "abcdefg2",
  name: "Claude 恢复",
  cmd: ["claude", "--resume"],
  env: { API_KEY: "secret-value" },
  createdAt: "2026-10-23T00:00:00Z",
  updatedAt: "2026-10-23T00:00:00Z",
};

type Handler = (
  method: string,
  body: unknown,
) => Response | Promise<Response> | undefined;

function serve(routes: Record<string, Handler>) {
  const requests: { method: string; path: string; body: unknown }[] = [];
  vi.stubGlobal(
    "fetch",
    vi.fn<typeof fetch>(async (input, init) => {
      const path = new URL(String(input), location.href).pathname;
      const method = init?.method ?? "GET";
      const body = init?.body ? JSON.parse(String(init.body)) : undefined;
      requests.push({ method, path, body });
      const response = await routes[path]?.(method, body);
      if (!response) throw new Error(`Unexpected request: ${method} ${path}`);
      return response;
    }),
  );
  return requests;
}

afterEach(async () => {
  try {
    await cleanup();
    await userEvent.cleanup();
  } finally {
    vi.unstubAllGlobals();
    vi.restoreAllMocks();
  }
});

const save = () => page.getByRole("button", { name: "保存", exact: true });

test.each([
  ["Gemini CLI", "geminicli"],
  ["自动匹配", undefined],
])(
  "edits the icon to %s while preserving the command and env",
  async (label, icon) => {
    const onsaved = vi.fn();
    const requests = serve({
      "/api/launch-configs/abcdefg2": (method, body) =>
        method === "GET"
          ? Response.json({ ...saved, icon: "codex" })
          : Response.json({ ...saved, ...(body as object) }),
    });
    render(LaunchConfigDialog, { open: true, configId: saved.id, onsaved });
    const picker = page.getByRole("combobox", { name: "图标", exact: true });
    await expect.element(picker).toMatchTextContent(/Codex/);
    await picker.click();
    await page.getByRole("option", { name: label, exact: true }).click();
    await save().click();
    await vi.waitFor(() => expect(onsaved).toHaveBeenCalledOnce());
    expect(requests.at(-1)).toMatchObject({
      method: "PUT",
      body: { cmd: saved.cmd, env: saved.env },
    });
    expect((requests.at(-1)!.body as { icon?: string }).icon).toBe(icon);
  },
);

test("masks env values by default and validates variable names", async () => {
  serve({});
  render(LaunchConfigDialog, {
    open: true,
    configId: undefined,
    onsaved: () => {},
  });
  await page.getByLabelText("名称").fill("Agent");
  await page.getByLabelText("启动命令").fill("claude");
  await page.getByRole("button", { name: "添加环境变量" }).click();
  const value = page.getByLabelText("变量值 1");
  await expect.element(value).toHaveAttribute("type", "password");
  const toggle = page.getByRole("button", { name: "显示变量值 1" });
  await expect.element(toggle).toHaveAttribute("aria-pressed", "false");
  await toggle.click();
  await expect.element(value).toHaveAttribute("type", "text");
  await expect.element(toggle).toHaveAttribute("aria-pressed", "true");
  await toggle.click();
  await expect.element(value).toHaveAttribute("type", "password");

  const name = page.getByLabelText("变量名 1");
  await name.fill("WEBMUX_SESSION_ID");
  await expect
    .element(page.getByText("WEBMUX_SESSION_ID 由 WebMux 保留"))
    .toBeVisible();
  await expect.element(save()).toBeDisabled();
  await name.fill("1BAD");
  await expect
    .element(page.getByText(/变量名只能包含字母、数字和下划线/))
    .toBeVisible();
  await expect.element(save()).toBeDisabled();
  await name.fill("API_KEY");
  await expect.element(save()).toBeEnabled();
});

test("the Bashrc checkbox saves a normal command without expanding its arguments", async () => {
  const onsaved = vi.fn();
  const requests = serve({
    "/api/launch-configs": (method, body) =>
      method === "POST"
        ? Response.json({ ...saved, ...(body as object) }, { status: 201 })
        : undefined,
  });
  render(LaunchConfigDialog, { open: true, configId: undefined, onsaved });
  await page.getByLabelText("名称").fill("Open Code");
  await page
    .getByLabelText("启动命令")
    .fill(`opencode --continue 'a b' '' '$HOME'`);
  await page
    .getByRole("checkbox", { name: "加载 ~/.bashrc", exact: true })
    .click();
  await save().click();
  await vi.waitFor(() => expect(onsaved).toHaveBeenCalledOnce());
  expect(requests.at(-1)).toEqual({
    method: "POST",
    path: "/api/launch-configs",
    body: {
      name: "Open Code",
      cmd: [
        "bash",
        "-ic",
        'exec -- "$@"',
        "webmux",
        "opencode",
        "--continue",
        "a b",
        "",
        "$HOME",
      ],
      env: {},
    },
  });
});

test.each([true, false])(
  "editing a wrapped config keeps Bash loading %s without losing argv or env",
  async (enabled) => {
    const argv = ["codex", "--resume", "a b", "", "$HOME"];
    const wrapped = ["bash", "-ic", 'exec -- "$@"', "webmux", ...argv];
    const onsaved = vi.fn();
    const requests = serve({
      "/api/launch-configs/abcdefg2": (method, body) =>
        method === "GET"
          ? Response.json({ ...saved, cmd: wrapped })
          : Response.json({ ...saved, ...(body as object) }),
    });
    await render(LaunchConfigDialog, {
      open: true,
      configId: saved.id,
      onsaved,
    });
    const checkbox = page.getByRole("checkbox", {
      name: "加载 ~/.bashrc",
      exact: true,
    });
    await expect.element(checkbox).toBeChecked();
    await expect
      .element(page.getByLabelText("启动命令"))
      .toHaveValue("codex --resume 'a b' '' '$HOME'");
    if (!enabled) await checkbox.click();
    await save().click();
    await vi.waitFor(() => expect(onsaved).toHaveBeenCalledOnce());
    expect(requests.at(-1)).toMatchObject({
      method: "PUT",
      body: { cmd: enabled ? wrapped : argv, env: saved.env },
    });
  },
);

test("editing only the name keeps every env variable in the PUT body", async () => {
  const loaded = Promise.withResolvers<Response>();
  const onsaved = vi.fn();
  const requests = serve({
    "/api/launch-configs/abcdefg2": (method, body) =>
      method === "GET"
        ? loaded.promise
        : Response.json({ ...saved, ...(body as object) }),
  });
  render(LaunchConfigDialog, { open: true, configId: "abcdefg2", onsaved });
  await expect.element(page.getByText("正在读取启动配置…")).toBeVisible();
  await expect.element(save()).toBeDisabled();
  loaded.resolve(Response.json(saved));
  const name = page.getByLabelText("名称");
  await expect.element(name).toHaveValue("Claude 恢复");
  await expect
    .element(page.getByLabelText("启动命令"))
    .toHaveValue("claude --resume");
  await expect
    .element(page.getByLabelText("变量值 1"))
    .toHaveAttribute("type", "password");
  await name.fill("Claude 新名称");
  await save().click();
  await vi.waitFor(() => expect(onsaved).toHaveBeenCalledOnce());
  expect(requests.at(-1)).toEqual({
    method: "PUT",
    path: "/api/launch-configs/abcdefg2",
    body: {
      name: "Claude 新名称",
      cmd: ["claude", "--resume"],
      env: { API_KEY: "secret-value" },
    },
  });
});

test("a failed load shows the error instead of a savable form", async () => {
  serve({
    "/api/launch-configs/abcdefg2": () =>
      Response.json({ error: "启动配置不存在" }, { status: 404 }),
  });
  render(LaunchConfigDialog, {
    open: true,
    configId: "abcdefg2",
    onsaved: () => {},
  });
  await expect
    .element(page.getByRole("alert"))
    .toHaveTextContent("无法读取启动配置：启动配置不存在");
  expect(save().elements()).toHaveLength(0);
  expect(page.getByLabelText("名称").elements()).toHaveLength(0);
});

test("save errors stay inline and keep the input", async () => {
  let status = 409;
  serve({
    "/api/launch-configs": () =>
      Response.json(
        {
          error:
            status === 409
              ? "启动配置名称已存在"
              : "启动配置不可用: 文件损坏（/state/launch-configs.json）",
        },
        { status },
      ),
  });
  const onsaved = vi.fn();
  render(LaunchConfigDialog, { open: true, configId: undefined, onsaved });
  await page.getByLabelText("名称").fill("Agent");
  await page.getByLabelText("启动命令").fill("claude");
  await save().click();
  await expect
    .element(page.getByRole("alert"))
    .toMatchTextContent(/已有同名启动配置“Agent”/);
  await expect.element(page.getByLabelText("名称")).toHaveValue("Agent");
  status = 503;
  await save().click();
  await expect
    .element(page.getByRole("alert"))
    .toMatchTextContent(/\/state\/launch-configs\.json/);
  await expect.element(page.getByLabelText("启动命令")).toHaveValue("claude");
  expect(onsaved).not.toHaveBeenCalled();
});
