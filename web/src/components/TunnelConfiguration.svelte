<script lang="ts">
  import { untrack } from "svelte";
  import IconEye from "~icons/tabler/eye";
  import IconEyeOff from "~icons/tabler/eye-off";
  import { Button } from "$lib/components/ui/button";
  import { Input } from "$lib/components/ui/input";
  import { Label } from "$lib/components/ui/label";
  import * as Tabs from "$lib/components/ui/tabs";
  import type { TunnelSettings, TunnelSettingsUpdate } from "../schemas";

  let {
    current,
    disabled,
    onsave,
  }: {
    current: TunnelSettings;
    disabled: boolean;
    onsave: (draft: TunnelSettingsUpdate) => void;
  } = $props();
  let mode = $state(untrack(() => current.mode));
  let hostname = $state(untrack(() => current.hostname));
  let token = $state("");
  let revealed = $state(false);
  let attempted = $state(false);
  const normalizedHost = $derived(hostname.toLowerCase().replace(/\.+$/, ""));
  const hostnameValid = $derived(
    normalizedHost.length > 0 &&
      normalizedHost.length <= 253 &&
      normalizedHost
        .split(".")
        .every(
          (label) =>
            label.length <= 63 &&
            /^[a-z0-9](?:[a-z0-9-]*[a-z0-9])?$/.test(label),
        ),
  );
  const hostnameError = $derived(
    mode === "named" && !hostnameValid
      ? "填写纯 ASCII/punycode 主机名，不含协议、端口、路径、通配符或空白。"
      : "",
  );
  const tokenError = $derived(
    mode === "named" && !token && !current.tokenConfigured
      ? "请填写 Cloudflare 隧道凭据。"
      : "",
  );

  function submit(event: SubmitEvent) {
    event.preventDefault();
    attempted = true;
    if (disabled || hostnameError || tokenError) return;
    onsave({
      mode,
      hostname: mode === "named" ? normalizedHost : "",
      ...(mode === "named" && token ? { token } : {}),
    });
  }
</script>

<form onsubmit={submit} class="space-y-4" aria-label="隧道配置">
  <fieldset {disabled} class="min-w-0 space-y-4">
    <Tabs.Root bind:value={mode}>
      <Tabs.List size="form">
        <Tabs.Trigger value="quick">Quick Tunnel</Tabs.Trigger>
        <Tabs.Trigger value="named">Named Tunnel</Tabs.Trigger>
      </Tabs.List>
      <Tabs.Content value="quick" class="space-y-2 pt-3">
        <p class="text-sm text-muted-foreground">
          无需 Cloudflare
          账号，适合临时访问。每次启动地址可能变化，不支持持续手机推送。
        </p>
        {#if current.tokenConfigured}<p class="text-sm text-warning">
            保存 Quick 模式会删除已保存的 Named 凭据与主机名。
          </p>{/if}
      </Tabs.Content>
      <Tabs.Content value="named" class="space-y-4 pt-3">
        <p class="text-sm text-muted-foreground">
          先在 Cloudflare 控制台创建 Tunnel，并将公共主机名路由到 WebMux
          本地监听地址。固定入口可用于手机推送。
        </p>
        <div class="grid gap-2">
          <Label for="tunnel-hostname">公共主机名</Label>
          <Input
            id="tunnel-hostname"
            bind:value={hostname}
            placeholder="term.example.com"
            autocomplete="off"
            autocapitalize="off"
            spellcheck={false}
            aria-invalid={!!hostnameError && (attempted || !!hostname)}
            aria-describedby="tunnel-hostname-help"
          />
          <p id="tunnel-hostname-help" class="text-xs text-muted-foreground">
            {(attempted || hostname) && hostnameError
              ? hostnameError
              : "仅填写主机名，例如 term.example.com。"}
          </p>
        </div>
        <div class="grid gap-2">
          <Label for="tunnel-token">Cloudflare 隧道凭据</Label>
          <div class="flex items-center gap-2">
            <Input
              id="tunnel-token"
              type={revealed ? "text" : "password"}
              bind:value={token}
              autocomplete="new-password"
              autocapitalize="off"
              spellcheck={false}
              placeholder={current.tokenConfigured
                ? "已配置；留空保留现有凭据"
                : "粘贴 Cloudflare 隧道 Token"}
              aria-invalid={attempted && !!tokenError}
              aria-describedby="tunnel-token-help"
            />
            <Button
              type="button"
              variant="outline"
              size="icon"
              aria-label={revealed ? "隐藏隧道凭据" : "显示隧道凭据"}
              title={revealed ? "隐藏隧道凭据" : "显示隧道凭据"}
              onclick={() => (revealed = !revealed)}
            >
              {#if revealed}<IconEyeOff aria-hidden="true" />{:else}<IconEye
                  aria-hidden="true"
                />{/if}
            </Button>
          </div>
          <p id="tunnel-token-help" class="text-xs text-muted-foreground">
            {attempted && tokenError
              ? tokenError
              : "只写不读，保存成功后输入框清空。与 WebMux 登录 Token 不同。"}
          </p>
        </div>
      </Tabs.Content>
    </Tabs.Root>
    <Button type="submit">保存并应用</Button>
  </fieldset>
</form>
