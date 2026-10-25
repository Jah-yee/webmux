<script lang="ts">
  import type { Snippet } from "svelte";
  import { MediaQuery } from "svelte/reactivity";
  import { cn } from "$lib/utils";
  import * as Dialog from "$lib/components/ui/dialog";
  import * as Drawer from "$lib/components/ui/drawer";

  // A form dialog that is a centered Dialog on desktop and a bottom Drawer on
  // narrow screens. While `busy`, every dismissal path (Escape, outside click,
  // close button, drawer drag) is ignored so an in-flight request is never
  // abandoned with its form.
  let {
    title,
    description,
    drawerDescription,
    onclose,
    children,
    busy = false,
    class: className,
    pinDrawerHeader = false,
  }: {
    title: string;
    description: string;
    /** Shorter drawer copy; defaults to `description`. */
    drawerDescription?: string;
    onclose: () => void;
    children: Snippet;
    busy?: boolean;
    /** Extra desktop Dialog content classes. */
    class?: string;
    /** Keep the drawer header fixed and scroll only the body. */
    pinDrawerHeader?: boolean;
  } = $props();
  const desktop = new MediaQuery("(min-width: 640px)");
  let open = $state(true);
  function changed(value: boolean) {
    if (!value && !busy) onclose();
    else if (!value) open = true;
  }
</script>

{#snippet drawerBody()}
  <div class={[pinDrawerHeader && "overflow-y-auto", "px-4 pt-1 pb-safe-4"]}>
    {@render children()}
  </div>
{/snippet}

{#snippet drawerHeader()}
  <Drawer.Header>
    <Drawer.Title>{title}</Drawer.Title>
    <Drawer.Description>{drawerDescription ?? description}</Drawer.Description>
  </Drawer.Header>
{/snippet}

{#if desktop.current}
  <Dialog.Root bind:open onOpenChange={changed}>
    <Dialog.Content
      class={cn("max-h-[90dvh] overflow-y-auto sm:max-w-xl", className)}
      onEscapeKeydown={(event) => {
        if (busy) event.preventDefault();
      }}
      onInteractOutside={(event) => {
        if (busy) event.preventDefault();
      }}
    >
      <Dialog.Header>
        <Dialog.Title>{title}</Dialog.Title>
        <Dialog.Description>{description}</Dialog.Description>
      </Dialog.Header>
      {@render children()}
    </Dialog.Content>
  </Dialog.Root>
{:else}
  <Drawer.Root bind:open onOpenChange={changed} dismissible={!busy}>
    <Drawer.Content class="data-[vaul-drawer-direction=bottom]:max-h-[95dvh]">
      {#if pinDrawerHeader}
        {@render drawerHeader()}
        {@render drawerBody()}
      {:else}
        <div class="min-h-0 overflow-y-auto">
          {@render drawerHeader()}
          {@render drawerBody()}
        </div>
      {/if}
    </Drawer.Content>
  </Drawer.Root>
{/if}
