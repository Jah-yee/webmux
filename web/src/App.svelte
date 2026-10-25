<script lang="ts">
  import { onMount } from "svelte";
  import { api, consumeTokenFragment, store, stored } from "./api";
  import { HealthSchema } from "./schemas";
  import { sync as syncPush } from "./push";
  import { Button } from "$lib/components/ui/button";
  import { Input } from "$lib/components/ui/input";
  import { Label } from "$lib/components/ui/label";
  import * as Card from "$lib/components/ui/card";
  import TokenHelp from "./components/TokenHelp.svelte";
  import SessionList from "./pages/SessionList.svelte";
  import { createTerminalDrafts } from "./terminal-draft.svelte";
  import { browserDraftStorage } from "./draftStorage";

  const drafts = createTerminalDrafts({ storage: browserDraftStorage() });
  let uploadNotice = $state("");

  let path = $state(location.pathname);
  let ready = $state(false);
  let checking = $state(true);
  let token = $state(stored("token"));
  let error = $state("");
  const match = $derived(path.match(/^\/s\/([a-z2-7]{8})$/));

  async function check() {
    checking = true;
    try {
      await api("/health", HealthSchema);
      ready = true;
      error = "";
      // Re-registers an existing push subscription, e.g. after the server
      // rebuilt its store; without one it touches nothing.
      void syncPush().catch(() => undefined);
    } catch (err) {
      error = (err as Error).message;
    } finally {
      checking = false;
    }
  }

  onMount(() => {
    const change = () => {
      path = location.pathname;
    };
    const auth = () => {
      drafts.clearAll();
      uploadNotice = "";
      ready = false;
      checking = false;
      error = "Token 无效或已更改";
    };
    window.addEventListener("popstate", change);
    window.addEventListener("webmux:auth", auth);
    if (consumeTokenFragment()) token = stored("token");
    void check();
    return () => {
      drafts.clearAll();
      window.removeEventListener("popstate", change);
      window.removeEventListener("webmux:auth", auth);
    };
  });
</script>

{#if !ready}
  <main class="flex min-h-dvh items-center justify-center p-6">
    <Card.Root class="w-full max-w-md">
      <Card.Header>
        <p class="brand mb-6"><span class="brand-mark">WM</span> WebMux</p>
        <Card.Title class="text-2xl">你的终端，始终在线。</Card.Title>
        <Card.Description>在可信内网中连接这台机器。</Card.Description>
      </Card.Header>
      <Card.Content>
        {#if checking}
          <p role="status" class="text-muted-foreground">正在连接…</p>
        {:else}
          <form
            class="space-y-5"
            onsubmit={(event) => {
              event.preventDefault();
              store("token", token);
              void check();
            }}
          >
            <div class="space-y-2">
              <div class="flex items-center gap-1">
                <Label for="token">共享 Token</Label>
                <TokenHelp />
              </div>
              <Input
                id="token"
                type="password"
                bind:value={token}
                autocomplete="current-password"
              />
            </div>
            {#if error}<p role="alert" class="text-destructive">{error}</p>{/if}
            <Button type="submit" class="w-full">连接 WebMux →</Button>
          </form>
        {/if}
      </Card.Content>
    </Card.Root>
  </main>
{:else if match}
  {#await import("./pages/Terminal.svelte")}
    <p role="status" class="p-6 text-muted-foreground">正在加载终端…</p>
  {:then page}
    {#key match[1]}<page.default
        id={match[1]}
        {drafts}
        onUploadNotice={(message) => {
          if (ready) uploadNotice = message;
        }}
      />{/key}
  {:catch error}
    <p role="alert" class="p-6 text-destructive">
      {error instanceof Error ? error.message : "终端加载失败"}
    </p>
  {/await}
{:else if path === "/settings"}
  {#await import("./pages/Settings.svelte")}
    <p role="status" class="p-6 text-muted-foreground">正在加载设置…</p>
  {:then page}
    <page.default />
  {:catch}
    <p role="alert" class="p-6 text-destructive">
      设置加载失败，请刷新页面后重试。
    </p>
  {/await}
{:else}
  <SessionList {drafts} />
{/if}

{#if ready && uploadNotice}
  <div
    class="fixed inset-x-2 top-2 z-50 flex items-start gap-2 rounded-md border bg-card p-3 text-sm shadow-lg"
    role="alert"
  >
    <p class="min-w-0 flex-1 break-all">{uploadNotice}</p>
    <Button variant="outline" size="sm" onclick={() => (uploadNotice = "")}
      >关闭</Button
    >
  </div>
{/if}
