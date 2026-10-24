<script lang="ts">
  import {
    IconArrowRight,
    IconPlus,
    IconSettings,
    IconTerminal2,
  } from "@tabler/icons-svelte";
  import { Button } from "$lib/components/ui/button";
  import { Badge } from "$lib/components/ui/badge";
  import { Input } from "$lib/components/ui/input";
  import { Textarea } from "$lib/components/ui/textarea";
  import { Label } from "$lib/components/ui/label";
  import { Switch } from "$lib/components/ui/switch";
  import { Skeleton } from "$lib/components/ui/skeleton";
  import * as Card from "$lib/components/ui/card";
  import * as Empty from "$lib/components/ui/empty";
  import * as Tabs from "$lib/components/ui/tabs";
  import * as DropdownMenu from "$lib/components/ui/dropdown-menu";
  import * as AlertDialog from "$lib/components/ui/alert-dialog";
  import InlineNotice from "../src/components/InlineNotice.svelte";
  import SessionStatus from "../src/components/SessionStatus.svelte";
  import SessionRow from "../src/components/SessionRow.svelte";
  import ResponsiveDialog from "../src/components/ResponsiveDialog.svelte";
  import { session, now } from "./fixtures";
  import type { SessionInfo } from "../src/schemas";

  let {
    section = "foundations",
  }: {
    section?:
      | "foundations"
      | "actions"
      | "forms"
      | "feedback"
      | "surfaces"
      | "overlays"
      | "sessions";
  } = $props();
  let notice = $state("");
  let dialogOpen = $state(false);
  let confirmOpen = $state(false);
  let name = $state("检查构建");
  const sections = {
    foundations: [
      "设计基础",
      "中性色承载内容，绿色标识主要操作，清晰层级帮助你继续工作。",
    ],
    actions: [
      "操作层级",
      "每个任务区域保留一个主要操作；次要操作降低强调，危险操作明确标识。",
    ],
    forms: [
      "表单与输入",
      "标签、帮助和错误紧邻字段；输入内容可读，状态清楚，操作可以预期。",
    ],
    feedback: [
      "状态与反馈",
      "颜色始终配合文字；区分等待、错误和正常退出，并说明下一步。",
    ],
    surfaces: [
      "容器与导航",
      "用留白和边界组织信息；阴影留给浮层，内容区保持安静。",
    ],
    overlays: [
      "弹窗与确认",
      "桌面居中弹窗，窄屏使用抽屉；取消可见，关闭后焦点回到触发按钮。",
    ],
    sessions: [
      "会话状态",
      "同一种信息使用同一种表达，先看任务名称，再看状态和最近输出。",
    ],
  };
  const colors = [
    ["background", "页面"],
    ["card", "内容"],
    ["muted", "次级区域"],
    ["primary", "主要操作"],
    ["success", "运行 / 成功"],
    ["warning", "等待 / 注意"],
    ["destructive", "失败 / 危险"],
    ["info", "提示"],
  ];
  const sessions: SessionInfo[] = [
    {
      ...session,
      id: "demorunx",
      title: "Codex · 检查构建与测试",
      status: "running",
      clients: 1,
      preview: "$ pnpm check\n正在检查 Svelte 与 TypeScript…",
      exitCode: null,
    },
    {
      ...session,
      id: "demowait",
      title: "Shell · 等待你的输入",
      status: "waiting",
      clients: 0,
      preview: "$ git status\nOn branch main\nworking tree clean",
      exitCode: null,
    },
    {
      ...session,
      id: "demoidle",
      title: "Shell · 暂无活动",
      status: "idle",
      clients: 0,
      preview: "",
      exitCode: null,
    },
    {
      ...session,
      id: "demodone",
      title: "构建完成",
      status: "exited",
      clients: 0,
      preview: "Build completed successfully.",
      exitCode: 0,
    },
    {
      ...session,
      id: "demofail",
      title: "测试进程已退出",
      status: "exited",
      clients: 0,
      preview: "1 test failed. See test report for details.",
      exitCode: 1,
    },
  ];
</script>

<main
  class="mx-auto max-w-5xl space-y-8 px-4 py-8 sm:px-8 sm:py-12"
  data-testid="design-system"
>
  <header class="space-y-4 border-b pb-6">
    <div class="brand flex-wrap">
      <span class="brand-mark">WM</span> WebMux
      <span
        class="ml-2 text-xs font-normal tracking-normal text-muted-foreground"
        >Design system</span
      >
    </div>
    <div class="space-y-2">
      <h1 class="text-2xl font-semibold tracking-tight sm:text-3xl">
        {sections[section][0]}
      </h1>
      <p class="max-w-2xl text-sm leading-relaxed text-muted-foreground">
        {sections[section][1]}
      </p>
    </div>
  </header>

  {#if section === "foundations"}
    <div class="grid gap-6 sm:grid-cols-2">
      <Card.Root>
        <Card.Header><Card.Title>设计核心</Card.Title></Card.Header>
        <Card.Content>
          <ol class="space-y-4 text-sm leading-relaxed">
            <li>
              <strong>01 · 内容优先</strong>
              <p class="mt-1 text-muted-foreground">
                终端和任务是主角，装饰不与工作内容争夺注意。
              </p>
            </li>
            <li>
              <strong>02 · 层级明确</strong>
              <p class="mt-1 text-muted-foreground">
                名称 → 状态 → 路径与时间，主要操作保持稳定位置。
              </p>
            </li>
            <li>
              <strong>03 · 操作有把握</strong>
              <p class="mt-1 text-muted-foreground">
                等待与失败说明原因；删除、清空、终止需要明确确认。
              </p>
            </li>
            <li>
              <strong>04 · 紧凑而可读</strong>
              <p class="mt-1 text-muted-foreground">
                默认控件 36px，正文 14px，触屏常规按钮至少 44px。
              </p>
            </li>
          </ol>
        </Card.Content>
      </Card.Root>
      <Card.Root>
        <Card.Header
          ><Card.Title>语义颜色</Card.Title><Card.Description
            >工具栏可切换明暗主题，业务含义保持一致。</Card.Description
          ></Card.Header
        >
        <Card.Content class="grid grid-cols-2 gap-4">
          {#each colors as [token, label]}
            <div class="min-w-0 space-y-1">
              <div
                class="h-10 rounded-md border"
                style:background={`var(--${token})`}
              ></div>
              <p class="text-sm">{label}</p>
              <p class="break-all font-mono text-xs text-muted-foreground">
                --{token}
              </p>
            </div>
          {/each}
        </Card.Content>
      </Card.Root>
      <Card.Root>
        <Card.Header><Card.Title>文字层级</Card.Title></Card.Header>
        <Card.Content class="space-y-4">
          <p class="text-2xl font-semibold tracking-tight">
            继续你的工作 <span class="text-xs font-normal text-muted-foreground"
              >24 / 30</span
            >
          </p>
          <p class="text-base font-semibold">
            工作区与任务 <span class="text-xs font-normal text-muted-foreground"
              >16</span
            >
          </p>
          <p class="text-sm">清晰的标签、正文和操作说明 · 14</p>
          <p class="text-xs text-muted-foreground">
            路径、时间、状态补充信息 · 12
          </p>
          <p class="break-all font-mono text-xs">
            ~/projects/webmux · git status
          </p>
        </Card.Content>
      </Card.Root>
      <Card.Root>
        <Card.Header><Card.Title>间距与形状</Card.Title></Card.Header>
        <Card.Content class="space-y-4">
          <div class="flex flex-wrap items-end gap-4">
            {#each [4, 8, 12, 16, 24, 32] as space}
              <div class="space-y-2 text-xs text-muted-foreground">
                <div
                  class="w-6 rounded-sm bg-primary/20"
                  style:height={`${space}px`}
                ></div>
                {space}
              </div>
            {/each}
          </div>
          <p class="text-sm text-muted-foreground">
            4px 间距基准；控件圆角 6px、内容容器 8px、浮层
            12px。单行路径可截断，错误和说明必须可读。
          </p>
        </Card.Content>
      </Card.Root>
    </div>
  {:else if section === "actions"}
    <Card.Root>
      <Card.Header
        ><Card.Title>按钮层级</Card.Title><Card.Description
          >主操作使用绿色，其余按钮根据语义降低强调。</Card.Description
        ></Card.Header
      >
      <Card.Content class="flex flex-wrap gap-3">
        <Button onclick={() => (notice = "示例会话已准备好。")}
          ><IconPlus aria-hidden="true" />新建会话</Button
        >
        <Button variant="outline">添加仓库</Button>
        <Button variant="secondary">查看详情</Button>
        <Button variant="ghost">刷新状态</Button>
        <Button variant="destructive" onclick={() => (confirmOpen = true)}
          >关闭会话</Button
        >
        <Button variant="link"
          >使用说明<IconArrowRight aria-hidden="true" /></Button
        >
      </Card.Content>
    </Card.Root>
    <Card.Root>
      <Card.Header><Card.Title>密度与状态</Card.Title></Card.Header>
      <Card.Content class="flex flex-wrap items-center gap-3">
        <Button size="xs" variant="outline">28 · 工具</Button>
        <Button size="sm" variant="outline">32 · 紧凑</Button>
        <Button variant="outline">36 · 默认</Button>
        <Button size="lg">44 · 主要入口</Button>
        <Button size="icon" variant="ghost" aria-label="设置示例"
          ><IconSettings aria-hidden="true" /></Button
        >
        <Button disabled>不可用</Button>
        <Button disabled aria-busy="true">正在保存…</Button>
      </Card.Content>
    </Card.Root>
    <InlineNotice
      >用 Tab 键检查焦点；禁用状态保留标签，处理中明确显示动作。</InlineNotice
    >
  {:else if section === "forms"}
    <Card.Root>
      <Card.Header
        ><Card.Title>快捷指令</Card.Title><Card.Description
          >表单宽度随容器变化，字段名称始终保留。</Card.Description
        ></Card.Header
      >
      <Card.Content class="grid gap-5 sm:grid-cols-2">
        <div class="space-y-2">
          <Label for="ds-name">名称</Label><Input
            id="ds-name"
            bind:value={name}
            aria-describedby="ds-name-help"
          />
          <p id="ds-name-help" class="text-xs text-muted-foreground">
            使用容易识别的任务名称。
          </p>
        </div>
        <div class="space-y-2">
          <Label for="ds-invalid">工作目录</Label><Input
            id="ds-invalid"
            value="/missing/workspace"
            aria-invalid="true"
            aria-describedby="ds-invalid-help"
          />
          <p id="ds-invalid-help" class="text-xs text-destructive">
            目录不存在，请选择可访问的路径。
          </p>
        </div>
        <div class="space-y-2 sm:col-span-2">
          <Label for="ds-command">指令</Label><Textarea
            id="ds-command"
            value="pnpm check"
            class="font-mono"
            rows={3}
          />
          <p class="text-xs text-muted-foreground">
            指令以原文发送，执行前请检查目标会话。
          </p>
        </div>
        <div class="space-y-2">
          <Label for="ds-readonly">只读信息</Label><Input
            id="ds-readonly"
            readonly
            value="当前浏览器"
          />
        </div>
        <div class="space-y-2">
          <Label for="ds-disabled">暂不可用</Label><Input
            id="ds-disabled"
            disabled
            value="等待连接"
          />
        </div>
        <div class="flex items-center gap-3 sm:col-span-2">
          <Switch id="ds-enter" /><Label for="ds-enter">发送后自动回车</Label>
        </div>
        <div class="flex flex-wrap gap-2 sm:col-span-2">
          <Button onclick={() => (notice = "示例已保存，不影响实际快捷指令。")}
            >保存示例</Button
          ><Button variant="outline" onclick={() => (name = "检查构建")}
            >重置名称</Button
          >
        </div>
      </Card.Content>
    </Card.Root>
  {:else if section === "feedback"}
    <Card.Root>
      <Card.Header
        ><Card.Title>会话状态</Card.Title><Card.Description
          >正常退出使用中性色，非零退出码使用危险色。</Card.Description
        ></Card.Header
      >
      <Card.Content class="flex flex-wrap gap-3">
        {#each sessions as item}<SessionStatus session={item} />{/each}
      </Card.Content>
    </Card.Root>
    <div class="space-y-3">
      <InlineNotice
        >草稿仅保存在当前浏览器，点击发送后才会提交到终端。</InlineNotice
      >
      <InlineNotice tone="success">设置已保存在当前浏览器。</InlineNotice>
      <InlineNotice tone="warning"
        >连接已断开，正在重连。你的草稿已保留。</InlineNotice
      >
      <InlineNotice tone="danger"
        >无法访问工作区。请检查目录是否存在，然后重新选择路径。</InlineNotice
      >
    </div>
    <div class="flex flex-wrap gap-2">
      <Badge variant="secondary">主工作区</Badge><Badge variant="outline"
        >已锁定</Badge
      ><Badge variant="warning">有修改</Badge><Badge variant="info"
        >未跟踪文件</Badge
      >
    </div>
  {:else if section === "surfaces"}
    <Tabs.Root value="workspace">
      <Tabs.List aria-label="示例内容"
        ><Tabs.Trigger value="workspace">工作区</Tabs.Trigger><Tabs.Trigger
          value="activity">最近活动</Tabs.Trigger
        ></Tabs.List
      >
      <Tabs.Content value="workspace">
        <Empty.Root class="mt-4 rounded-lg border border-dashed bg-card">
          <Empty.Header
            ><Empty.Media variant="icon"
              ><IconTerminal2 aria-hidden="true" /></Empty.Media
            ><Empty.Title>还没有会话</Empty.Title><Empty.Description
              >启动 Shell 或 agent，随时回到正在进行的工作。</Empty.Description
            ></Empty.Header
          >
          <Empty.Content
            ><Button onclick={() => (notice = "此示例不会启动实际进程。")}
              ><IconPlus aria-hidden="true" />新建会话</Button
            ></Empty.Content
          >
        </Empty.Root>
      </Tabs.Content>
      <Tabs.Content value="activity"
        ><InlineNotice class="mt-4"
          >暂无最近活动。创建会话后，回到这里查看。</InlineNotice
        ></Tabs.Content
      >
    </Tabs.Root>
    <Card.Root aria-label="加载状态示例" aria-busy="true">
      <Card.Header><Card.Title>正在加载工作区…</Card.Title></Card.Header>
      <Card.Content class="space-y-3"
        ><Skeleton class="h-4 w-2/3" /><Skeleton class="h-4 w-1/3" /><Skeleton
          class="h-24 w-full"
        /></Card.Content
      >
    </Card.Root>
    <DropdownMenu.Root>
      <DropdownMenu.Trigger
        >{#snippet child({ props })}<Button {...props} variant="outline"
            >工作区操作</Button
          >{/snippet}</DropdownMenu.Trigger
      >
      <DropdownMenu.Content align="start"
        ><DropdownMenu.Item onSelect={() => (notice = "已选择查看工作区。")}
          >查看工作区</DropdownMenu.Item
        ><DropdownMenu.Separator /><DropdownMenu.Item
          variant="destructive"
          onSelect={() => (confirmOpen = true)}
          >删除示例工作区</DropdownMenu.Item
        ></DropdownMenu.Content
      >
    </DropdownMenu.Root>
  {:else if section === "overlays"}
    <div class="flex flex-wrap gap-3">
      <Button onclick={() => (dialogOpen = true)}>打开编辑弹窗</Button><Button
        variant="outline"
        onclick={() => (confirmOpen = true)}>打开危险操作确认</Button
      >
    </div>
    <InlineNotice
      >编辑弹窗使用生产组件
      ResponsiveDialog。调整窗口宽度，可检查桌面弹窗和手机抽屉。</InlineNotice
    >
  {:else if section === "sessions"}
    <div role="list" aria-label="会话状态示例" class="space-y-3">
      {#each sessions as item}<SessionRow
          session={item}
          {now}
          onclose={() => (confirmOpen = true)}
        />{/each}
    </div>
  {/if}
  {#if notice}<InlineNotice tone="success">{notice}</InlineNotice>{/if}
</main>

{#if dialogOpen}
  <ResponsiveDialog
    title="编辑工作区"
    description="让名称易于识别，路径保持完整可查。"
    onclose={() => (dialogOpen = false)}
  >
    <div class="space-y-4">
      <div class="space-y-2">
        <Label for="ds-workspace-name">工作区名称</Label><Input
          id="ds-workspace-name"
          value="WebMux"
        />
      </div>
      <div class="space-y-2">
        <Label for="ds-workspace-path">工作目录</Label><Input
          id="ds-workspace-path"
          value="/home/developer/projects/webmux"
        />
      </div>
      <div class="flex flex-wrap justify-end gap-2">
        <Button variant="outline" onclick={() => (dialogOpen = false)}
          >取消</Button
        ><Button
          onclick={() => {
            dialogOpen = false;
            notice = "示例已保存。";
          }}>保存</Button
        >
      </div>
    </div>
  </ResponsiveDialog>
{/if}
<AlertDialog.Root bind:open={confirmOpen}>
  <AlertDialog.Content>
    <AlertDialog.Header
      ><AlertDialog.Title>关闭示例会话？</AlertDialog.Title
      ><AlertDialog.Description
        >真实操作会终止进程及子进程。此处只演示确认流程，不会执行终端操作。</AlertDialog.Description
      ></AlertDialog.Header
    >
    <AlertDialog.Footer
      ><AlertDialog.Cancel>取消</AlertDialog.Cancel><AlertDialog.Action
        variant="destructive"
        onclick={() => (notice = "示例操作已完成。")}
        >确认关闭</AlertDialog.Action
      ></AlertDialog.Footer
    >
  </AlertDialog.Content>
</AlertDialog.Root>
