import { afterEach, expect, test, vi } from "vitest";
import { page, userEvent } from "vitest/browser";
import { cleanup, render } from "vitest-browser-svelte/pure";
import LaunchConfigDialog from "../../src/components/LaunchConfigDialog.svelte";
import type { LaunchConfig, Preset } from "../../src/schemas";

const presets: Preset[] = [
  {
    name: "shell",
    label: "Shell",
    cmd: ["bash", "-l", "-i"],
    allowArgs: false,
  },
  { name: "claude", label: "Claude Code", cmd: ["claude"], allowArgs: true },
];
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
      if (path === "/api/presets" && !routes[path])
        return Response.json(presets);
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

test("fills the command from a built-in preset and its label", async () => {
  const onsaved = vi.fn();
  const requests = serve({
    "/api/launch-configs": (method, body) =>
      method === "POST"
        ? Response.json({ ...saved, ...(body as object) }, { status: 201 })
        : undefined,
  });
  render(LaunchConfigDialog, { open: true, configId: undefined, onsaved });
  await page.getByRole("button", { name: "Claude Code", exact: true }).click();
  await page.getByLabelText("预设追加参数").fill(`--resume "a b" ''`);
  await page.getByRole("button", { name: "填入命令" }).click();
  await expect
    .element(page.getByLabelText("启动命令"))
    .toHaveValue("claude --resume 'a b' ''");
  await expect.element(page.getByLabelText("名称")).toHaveValue("Claude Code");
  await save().click();
  await vi.waitFor(() => expect(onsaved).toHaveBeenCalledOnce());
  expect(requests.at(-1)).toEqual({
    method: "POST",
    path: "/api/launch-configs",
    body: {
      name: "Claude Code",
      cmd: ["claude", "--resume", "a b", ""],
      env: {},
    },
  });
});

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
