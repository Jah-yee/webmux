<script lang="ts">
  import { untrack } from "svelte";
  import { Button } from "$lib/components/ui/button";
  import { Input } from "$lib/components/ui/input";
  import { Label } from "$lib/components/ui/label";
  import * as NativeSelect from "$lib/components/ui/native-select";
  import * as Tabs from "$lib/components/ui/tabs";
  import {
    TailscaleFunnelPorts,
    type TailscaleSettingsUpdate,
    type TailscaleStatus,
  } from "../schemas";

  let {
    current,
    disabled,
    onsave,
  }: {
    current: TailscaleStatus;
    disabled: boolean;
    onsave: (draft: TailscaleSettingsUpdate) => void;
  } = $props();
  let mode = $state(untrack(() => current.mode));
  let provider = $state(untrack(() => current.configuredProvider));
  // Serve takes any port; empty or 0 selects 443, 8443 or 10000 automatically.
  let servePort = $state(
    untrack(() => (current.port === 0 ? "" : String(current.port))),
  );
  let funnelPort = $state(
    untrack(() =>
      (TailscaleFunnelPorts as readonly number[]).includes(current.port)
        ? String(current.port)
        : "0",
    ),
  );
  let attempted = $state(false);
  const parsedServePort = $derived(
    servePort.trim() === "" ? 0 : Number(servePort),
  );
  const portError = $derived(
    mode === "serve" &&
      !(
        Number.isInteger(parsedServePort) &&
        parsedServePort >= 0 &&
        parsedServePort <= 65535
      )
      ? "端口必须是 1–65535 的整数，留空表示自动。"
      : "",
  );
  const port = $derived(
    mode === "serve" ? parsedServePort : mode === "funnel" ? +funnelPort : 0,
  );

  function submit(event: SubmitEvent) {
    event.preventDefault();
    attempted = true;
    if (disabled || portError) return;
    onsave({ mode, port, provider });
  }
</script>

<form onsubmit={submit} class="space-y-4" aria-label="Tailscale 配置">
  <fieldset {disabled} class="min-w-0 space-y-4">
    <Tabs.Root bind:value={mode}>
      <Tabs.List size="form" aria-label="Tailscale 模式">
        <Tabs.Trigger value="off">关闭</Tabs.Trigger>
        <Tabs.Trigger value="serve">Serve</Tabs.Trigger>
        <Tabs.Trigger value="funnel">Funnel</Tabs.Trigger>
      </Tabs.List>
      <Tabs.Content value="off" class="pt-3">
        <p class="text-sm text-muted-foreground">
          不创建入口；保存后会移除 WebMux 自己记录的入口，其他入口保持不变。
        </p>
      </Tabs.Content>
      <Tabs.Content value="serve" class="space-y-4 pt-3">
        <p class="text-sm text-muted-foreground">
          仅 tailnet 内已登录的设备可访问，证书由 Tailscale 自动签发。
        </p>
        <div class="grid gap-2">
          <Label for="tailscale-serve-port">Serve HTTPS 端口</Label>
          <Input
            id="tailscale-serve-port"
            inputmode="numeric"
            bind:value={servePort}
            placeholder="自动（443 / 8443 / 10000）"
            autocomplete="off"
            aria-invalid={attempted && !!portError}
            aria-describedby="tailscale-serve-port-help"
          />
          <p
            id="tailscale-serve-port-help"
            class="text-xs text-muted-foreground"
          >
            {attempted && portError
              ? portError
              : "留空自动选择第一个没有其他入口的端口；显式端口被占用时不会覆盖。"}
          </p>
        </div>
      </Tabs.Content>
      <Tabs.Content value="funnel" class="space-y-4 pt-3">
        <p class="text-sm text-warning">
          Funnel 会把 WebMux 公开到互联网：任何拿到链接与 Token
          的人都能以服务用户身份执行命令。保存前需要再次确认。
        </p>
        <div class="grid gap-2">
          <Label for="tailscale-funnel-port">Funnel HTTPS 端口</Label>
          <NativeSelect.Root id="tailscale-funnel-port" bind:value={funnelPort}>
            <NativeSelect.Option value="0">自动</NativeSelect.Option>
            {#each TailscaleFunnelPorts as option (option)}
              <NativeSelect.Option value={String(option)}
                >{option}</NativeSelect.Option
              >
            {/each}
          </NativeSelect.Root>
        </div>
      </Tabs.Content>
    </Tabs.Root>
    <div class="grid gap-2">
      <Label for="tailscale-provider">Provider</Label>
      <NativeSelect.Root
        id="tailscale-provider"
        bind:value={provider}
        aria-describedby="tailscale-provider-help"
      >
        <NativeSelect.Option value="auto">自动</NativeSelect.Option>
        <NativeSelect.Option value="linux"
          >Linux / macOS / WSL 内</NativeSelect.Option
        >
        {#if current.wsl}
          <NativeSelect.Option value="windows"
            >Windows 主机（需 mirrored 网络）</NativeSelect.Option
          >
        {/if}
      </NativeSelect.Root>
      <p id="tailscale-provider-help" class="text-xs text-muted-foreground">
        自动永不代管 Windows 节点。CLI 路径只能在配置文件的 [tailscale] command
        中设置。
      </p>
    </div>
    <Button type="submit">保存并应用</Button>
  </fieldset>
</form>
