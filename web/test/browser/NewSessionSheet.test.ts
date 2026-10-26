import { afterEach, beforeEach, expect, test, vi } from "vitest";
import { page, userEvent } from "vitest/browser";
import { cleanup, render } from "vitest-browser-svelte/pure";
import NewSessionSheet from "../../src/components/NewSessionSheet.svelte";
import type { LaunchConfigsState } from "../../src/launchConfigs";
import type { Repository, SessionInfo } from "../../src/schemas";

const config = {
  id: "abcdefg2",
  name: "Claude 恢复",
  cmd: ["claude", "--resume"],
  envNames: ["API_KEY", "BASE_URL"],
  createdAt: "2026-10-23T00:00:00Z",
  updatedAt: "2026-10-23T00:00:00Z",
};
const otherConfig = {
  ...config,
  id: "hijklm23",
  name: "Open Code",
  cmd: ["opencode", "--continue", "a b"],
  envNames: [],
};
const repository = {
  id: "repo-1",
  name: "project",
  commonDir: "/repos/project/.git",
  anchorPath: "/repos/project",
  availability: "available",
  error: "",
  checkedAt: "2026-10-23T00:00:00Z",
  worktrees: [
    {
      id: "main",
      repositoryId: "repo-1",
      path: "/repos/project",
      kind: "main",
      branch: "main",
      head: "a".repeat(40),
      detached: false,
      locked: false,
      lockReason: "",
      prunable: false,
      available: true,
      dirty: false,
      hasUntracked: false,
      hasIgnored: false,
      error: "",
    },
  ],
} satisfies Repository;
const session: SessionInfo = {
  id: "sessio23",
  generation: "0123456789abcdef",
  title: "",
  cwd: "/work",
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
      if (path === "/api/repositories/repo-1/branches") {
        return Response.json({
          branches: [
            {
              name: "main",
              head: "a".repeat(40),
              worktreePaths: ["/repos/project"],
            },
          ],
          bases: [{ name: "refs/heads/main", head: "a".repeat(40) }],
          defaultBase: "a".repeat(40),
          defaultParent: "/repos/project.worktrees",
        });
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

function open(
  launchConfigs: LaunchConfigsState,
  target: {
    repositories?: Repository[];
    initialWorktreeId?: string;
    initialMode?: "direct" | "existing" | "new";
  } = {},
) {
  return render(NewSessionSheet, {
    launchConfigs,
    defaultCwd: "/work",
    ...target,
    onchange: () => {},
    onworktreecreated: () => {},
    onclose: () => {},
  });
}
const submit = () =>
  page.getByRole("button", { name: "创建并进入 →", exact: true });
const commandTab = (name: "预先输入" | "手动填写") =>
  page
    .getByRole("tablist", { name: "启动命令来源", exact: true })
    .getByRole("tab", { name, exact: true });
const locationTab = (name: "直接目录" | "已有工作树" | "新工作树") =>
  page
    .getByRole("tablist", { name: "启动位置", exact: true })
    .getByRole("tab", { name, exact: true });
const configSelect = () =>
  page.getByRole("combobox", { name: "选择启动配置", exact: true });
const command = () => page.getByLabelText("自定义启动命令");
const selected = (name: "预先输入" | "手动填写") =>
  expect.element(commandTab(name)).toHaveAttribute("aria-selected", "true");

async function chooseConfig(name: string) {
  await commandTab("预先输入").click();
  await configSelect().click();
  await page.getByRole("option", { name, exact: true }).click();
}

test("starts a saved config with only configId and displays its command and environment count", async () => {
  open({ status: "ready", configs: [config, otherConfig] });
  await commandTab("预先输入").click();
  await configSelect().click();
  const option = page.getByRole("option", {
    name: config.name,
    exact: true,
  });
  await expect.element(option).toMatchTextContent(/Claude 恢复/);
  await expect.element(option).toMatchTextContent(/claude --resume/);
  await expect.element(option).toMatchTextContent(/2 个环境变量/);
  expect(
    option.element().querySelector('[data-launch-icon="claudecode"] svg'),
  ).not.toBeNull();
  await expect
    .element(page.getByRole("option", { name: otherConfig.name, exact: true }))
    .toMatchTextContent(/opencode --continue 'a b'/);
  await option.click();
  await selected("预先输入");
  await expect.element(configSelect()).toMatchTextContent(/Claude 恢复/);
  await expect.element(configSelect()).toMatchTextContent(/claude --resume/);
  await expect.element(configSelect()).toMatchTextContent(/2 个环境变量/);
  expect(
    configSelect()
      .element()
      .querySelector('[data-launch-icon="claudecode"] svg'),
  ).not.toBeNull();
  await submit().click();
  await vi.waitFor(() => expect(created).toHaveLength(1));
  expect(created[0]).toEqual({ cwd: "/work", configId: "abcdefg2", name: "" });
  expect(localStorage.getItem("webmux.lastLaunch")).toBe("config:abcdefg2");
});

test("defaults to the first saved config only when no launch choice was remembered", async () => {
  open({ status: "ready", configs: [config, otherConfig] });
  await selected("预先输入");
  await expect.element(configSelect()).toMatchTextContent(/Claude 恢复/);
  await expect.element(submit()).toBeEnabled();
});

test("a remembered manual command preserves argument boundaries even when configs exist", async () => {
  localStorage.setItem("webmux.lastLaunch", "custom");
  localStorage.setItem("webmux.customCommand", `htop 'a b' '' '$HOME'`);
  open({ status: "ready", configs: [config] });
  await selected("手动填写");
  await expect.element(command()).toHaveValue(`htop 'a b' '' '$HOME'`);
  await submit().click();
  await vi.waitFor(() => expect(created).toHaveLength(1));
  expect(created[0]).toEqual({
    cwd: "/work",
    cmd: ["htop", "a b", "", "$HOME"],
    name: "",
  });
  expect(localStorage.getItem("webmux.lastLaunch")).toBe("custom");
});

test("command tabs retain both choices but an empty manual command cannot use the retained config", async () => {
  open({ status: "ready", configs: [config, otherConfig] });
  await chooseConfig(otherConfig.name);
  await commandTab("手动填写").click();
  await expect.element(command()).toHaveValue("");
  await expect.element(submit()).toBeDisabled();
  await command().fill("echo retained");
  await commandTab("预先输入").click();
  await expect.element(configSelect()).toMatchTextContent(/Open Code/);
  await expect.element(submit()).toBeEnabled();
  await commandTab("手动填写").click();
  await expect.element(command()).toHaveValue("echo retained");
  await submit().click();
  await vi.waitFor(() => expect(created).toHaveLength(1));
  expect(created[0]).toEqual({
    cwd: "/work",
    cmd: ["echo", "retained"],
    name: "",
  });
});

test("keyboard navigation selects a dropdown option and submits its exact config ID", async () => {
  open({ status: "ready", configs: [config, otherConfig] });
  await commandTab("预先输入").click();
  configSelect().element().focus();
  await userEvent.keyboard("{Enter}");
  await expect
    .element(page.getByRole("option", { name: otherConfig.name, exact: true }))
    .toBeVisible();
  await userEvent.keyboard("{End}{Enter}");
  await expect.element(configSelect()).toHaveFocus();
  await expect.element(configSelect()).toMatchTextContent(/Open Code/);
  submit().element().focus();
  await userEvent.keyboard("{Enter}");
  await vi.waitFor(() => expect(created).toHaveLength(1));
  expect(created[0]).toEqual({
    cwd: "/work",
    configId: otherConfig.id,
    name: "",
  });
});

test("a remembered deleted config falls back to empty manual input instead of another config", async () => {
  localStorage.setItem("webmux.lastLaunch", "config:zzzzzzz2");
  open({ status: "ready", configs: [config] });
  await selected("手动填写");
  await expect.element(command()).toHaveValue("");
  await expect.element(submit()).toBeDisabled();
});

test("deleting the selected config while the sheet is open does not select its replacement", async () => {
  const view = await open({ status: "ready", configs: [config, otherConfig] });
  await chooseConfig(config.name);
  await view.rerender({
    launchConfigs: { status: "ready", configs: [otherConfig] },
  });
  await selected("手动填写");
  await expect.element(command()).toHaveValue("");
  await expect.element(submit()).toBeDisabled();
  await commandTab("预先输入").click();
  await expect.element(configSelect()).not.toMatchTextContent(/Open Code/);
  await expect.element(submit()).toBeDisabled();
  expect(created).toEqual([]);
});

test("an empty config list links to the settings card", async () => {
  open({ status: "ready", configs: [] });
  await commandTab("预先输入").click();
  await expect
    .element(page.getByRole("link", { name: "前往设置创建启动配置" }))
    .toHaveAttribute("href", "/settings#launch-configs");
  await expect.element(submit()).toBeDisabled();
});

test("an unavailable store falls back to explicit manual argv", async () => {
  localStorage.setItem("webmux.lastLaunch", "config:abcdefg2");
  open({
    status: "unavailable",
    error: "启动配置不可用: 文件损坏（/state/launch-configs.json）",
  });
  await selected("手动填写");
  await expect.element(submit()).toBeDisabled();
  await commandTab("预先输入").click();
  await expect
    .element(page.getByText(/启动配置暂不可用：.*launch-configs\.json/))
    .toBeVisible();
  await commandTab("手动填写").click();
  await command().fill("/bin/bash --noprofile --norc -i");
  await submit().click();
  await vi.waitFor(() => expect(created).toHaveLength(1));
  expect(created[0]).toEqual({
    cwd: "/work",
    cmd: ["/bin/bash", "--noprofile", "--norc", "-i"],
    name: "",
  });
});

test("a store becoming unavailable preserves manual text without launching a stale saved config", async () => {
  const view = await open({ status: "ready", configs: [config] });
  await commandTab("手动填写").click();
  await command().fill("echo retained");
  await chooseConfig(config.name);
  await view.rerender({
    launchConfigs: { status: "unavailable", error: "storage offline" },
  });
  await selected("手动填写");
  await expect.element(command()).toHaveValue("echo retained");
  await submit().click();
  await vi.waitFor(() => expect(created).toHaveLength(1));
  expect(created[0]).toEqual({
    cwd: "/work",
    cmd: ["echo", "retained"],
    name: "",
  });
});

test("loading saved configs permits an explicit switch to manual argv", async () => {
  localStorage.setItem("webmux.lastLaunch", "config:abcdefg2");
  open({ status: "loading" });
  await selected("预先输入");
  await expect.element(page.getByText("正在读取启动配置…")).toBeVisible();
  await expect.element(submit()).toBeDisabled();
  await commandTab("手动填写").click();
  await command().fill("echo ready");
  await submit().click();
  await vi.waitFor(() => expect(created).toHaveLength(1));
  expect(created[0]).toEqual({
    cwd: "/work",
    cmd: ["echo", "ready"],
    name: "",
  });
});

test("loading completion does not override an explicit manual choice", async () => {
  localStorage.setItem("webmux.lastLaunch", "config:abcdefg2");
  const view = await open({ status: "loading" });
  await commandTab("手动填写").click();
  await command().fill("echo ready");
  await view.rerender({
    launchConfigs: { status: "ready", configs: [config] },
  });
  await selected("手动填写");
  await expect.element(command()).toHaveValue("echo ready");
  await expect.element(submit()).toBeEnabled();
});

test("a remembered saved config stays selected", async () => {
  localStorage.setItem("webmux.lastLaunch", `config:${otherConfig.id}`);
  open({ status: "ready", configs: [config, otherConfig] });
  await selected("预先输入");
  await expect.element(configSelect()).toMatchTextContent(/Open Code/);
  await expect.element(submit()).toBeEnabled();
});

test("empty or malformed manual argv cannot create a session despite a retained valid config", async () => {
  open({ status: "ready", configs: [config] });
  await chooseConfig(config.name);
  await commandTab("手动填写").click();
  for (const value of ["", "   ", "''", "echo 'unfinished"]) {
    await command().fill(value);
    await expect.element(submit()).toBeDisabled();
  }
  expect(created).toEqual([]);
});

test("an unknown remembered launch choice degrades to the manual command", async () => {
  localStorage.setItem("webmux.lastLaunch", "obsolete-choice");
  localStorage.setItem("webmux.customCommand", "echo retained");
  open({ status: "ready", configs: [config] });
  await selected("手动填写");
  await expect.element(command()).toHaveValue("echo retained");
  await expect.element(submit()).toBeEnabled();
});

test("a deleted config retains an explicitly remembered manual command", async () => {
  localStorage.setItem("webmux.lastLaunch", "config:zzzzzzz2");
  localStorage.setItem("webmux.customCommand", "echo retained");
  open({ status: "ready", configs: [] });
  await selected("手动填写");
  await expect.element(command()).toHaveValue("echo retained");
  await expect.element(submit()).toBeEnabled();
});

test("location tabs preserve the direct directory, selected worktree and new-worktree fields", async () => {
  open(
    { status: "ready", configs: [config] },
    { repositories: [repository], initialWorktreeId: "main" },
  );
  await expect
    .element(locationTab("直接目录"))
    .toHaveAttribute("aria-selected", "true");
  await page.getByLabelText("工作目录").fill("/work/retained");
  await page.getByLabelText("会话名称", { exact: false }).fill("Retained name");
  await locationTab("已有工作树").click();
  await expect
    .element(page.getByRole("tabpanel", { name: "已有工作树", exact: true }))
    .toBeVisible();
  await expect
    .element(page.getByLabelText("工作树", { exact: true }))
    .toMatchTextContent("主工作区 · main");
  await expect.element(page.getByLabelText("工作目录")).not.toBeVisible();
  await locationTab("新工作树").click();
  const branch = page.getByLabelText("新分支名称", { exact: true });
  await expect.element(branch).not.toHaveValue("");
  await branch.fill("feature/retained");
  await page.getByLabelText("新工作树完整路径").fill("/repos/custom-retained");
  await locationTab("直接目录").click();
  await expect
    .element(page.getByLabelText("工作目录"))
    .toHaveValue("/work/retained");
  await expect
    .element(page.getByLabelText("会话名称", { exact: false }))
    .toHaveValue("Retained name");
  await locationTab("已有工作树").click();
  await expect
    .element(page.getByLabelText("工作树", { exact: true }))
    .toMatchTextContent("主工作区 · main");
  await locationTab("新工作树").click();
  await expect.element(branch).toHaveValue("feature/retained");
  await expect
    .element(page.getByLabelText("新工作树完整路径"))
    .toHaveValue("/repos/custom-retained");
  await locationTab("直接目录").click();
  await submit().click();
  await vi.waitFor(() => expect(created).toHaveLength(1));
  expect(created[0]).toEqual({
    cwd: "/work/retained",
    configId: config.id,
    name: "Retained name",
  });
});

test("worktree tabs are disabled without repositories", async () => {
  open({ status: "ready", configs: [config] });
  await expect.element(locationTab("已有工作树")).toBeDisabled();
  await expect.element(locationTab("新工作树")).toBeDisabled();
  await expect.element(locationTab("直接目录")).toBeEnabled();
});

test("unavailable and bare worktrees cannot be selected or submitted", async () => {
  const main = repository.worktrees[0];
  open(
    { status: "ready", configs: [config] },
    {
      initialMode: "existing",
      initialWorktreeId: "missing",
      repositories: [
        {
          ...repository,
          worktrees: [
            { ...main, id: "missing", available: false },
            { ...main, id: "bare", kind: "bare", branch: "" },
          ],
        },
      ],
    },
  );
  const trees = page.getByRole("combobox", { name: "工作树", exact: true });
  await trees.click();
  await expect
    .element(page.getByRole("option", { name: /main/ }))
    .toHaveAttribute("aria-disabled", "true");
  await expect
    .element(page.getByRole("option", { name: /无提交|aaaa/ }))
    .toHaveAttribute("aria-disabled", "true");
  await expect.element(submit()).toBeDisabled();
  expect(created).toEqual([]);
});

test("changing repositories clears the old worktree and starts only the newly selected tree", async () => {
  const secondRepository: Repository = {
    ...repository,
    id: "repo-2",
    name: "other project",
    anchorPath: "/repos/other",
    worktrees: [
      {
        ...repository.worktrees[0],
        id: "other-main",
        repositoryId: "repo-2",
        path: "/repos/other",
      },
    ],
  };
  await open(
    { status: "ready", configs: [config] },
    {
      repositories: [repository, secondRepository],
      initialMode: "existing",
      initialWorktreeId: "main",
    },
  );
  await page.getByRole("combobox", { name: "仓库", exact: true }).click();
  await page
    .getByRole("option", { name: "other project", exact: true })
    .click();
  const tree = page.getByRole("combobox", { name: "工作树", exact: true });
  await expect.element(tree).toMatchTextContent("选择工作树");
  await expect.element(submit()).toBeDisabled();
  await tree.click();
  await page
    .getByRole("option", { name: "主工作区 · main", exact: true })
    .click();
  await expect.element(tree).toMatchTextContent("/repos/other");
  await submit().click();
  await vi.waitFor(() => expect(created).toHaveLength(1));
  expect(created[0]).toEqual({
    workspace: { repositoryId: "repo-2", worktreeId: "other-main" },
    configId: config.id,
    name: "",
  });
});

test("the mobile dropdown and drawer fit long names and commands without horizontal overflow", async () => {
  await page.viewport(390, 844);
  try {
    const longName = "很长的启动配置名称".repeat(6);
    open({
      status: "ready",
      configs: [
        {
          ...config,
          name: longName,
          cmd: ["bash", "-lc", "x".repeat(300)],
        },
      ],
    });
    await commandTab("预先输入").click();
    const trigger = configSelect();
    await expect.element(trigger).toMatchTextContent(longName);
    await trigger.click();
    const option = page.getByRole("option", { name: longName, exact: true });
    const listbox = page.getByRole("listbox");
    const dialog = page.getByRole("dialog");
    await expect.element(option).toBeVisible();
    await expect.element(option).toMatchTextContent("x".repeat(300));
    for (const element of [
      trigger.element(),
      option.element(),
      listbox.element(),
      dialog.element(),
    ]) {
      const bounds = element.getBoundingClientRect();
      expect(bounds.left).toBeGreaterThanOrEqual(0);
      expect(bounds.right).toBeLessThanOrEqual(window.innerWidth);
      expect(element.scrollWidth).toBeLessThanOrEqual(element.clientWidth);
    }
    expect(document.documentElement.scrollWidth).toBeLessThanOrEqual(
      window.innerWidth,
    );
    await option.click();
    await expect.element(trigger).toMatchTextContent(longName);
    expect(trigger.element().scrollWidth).toBeLessThanOrEqual(
      trigger.element().clientWidth,
    );
  } finally {
    await page.viewport(1024, 900);
  }
});
