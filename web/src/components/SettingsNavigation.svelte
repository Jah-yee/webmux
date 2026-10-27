<script lang="ts">
  import { onMount } from "svelte";
  import * as NavigationMenu from "$lib/components/ui/navigation-menu";

  const sections = [
    { id: "appearance", label: "外观与阅读" },
    { id: "quick-commands", label: "快捷指令" },
    { id: "launch-configs", label: "启动配置" },
    { id: "drafts", label: "草稿与暂存" },
    { id: "access", label: "公网访问" },
    { id: "tailscale", label: "Tailscale" },
    { id: "push", label: "通知推送" },
    { id: "preview", label: "输出预览" },
  ];
  let active = $state(sections[0]!.id);
  let navigation: HTMLElement;
  let vertical = $state(false);
  let links: HTMLDivElement;

  onMount(() => {
    const targets = sections.map(({ id }) => document.getElementById(id)!);
    const header = document.querySelector<HTMLElement>(
      ".safe-area-page > .app-header",
    );
    const desktop = matchMedia("(min-width: 1024px)");
    const standalone = matchMedia("(display-mode: standalone)");
    let frame = 0;

    // A Home Screen app keeps the header sticky, so the navigation must clear
    // its real height: the touch-target padding and safe-area inset make it
    // taller than the header height constant. Browser tabs let the header
    // scroll away and keep the CSS offsets.
    function syncOffset() {
      const height = standalone.matches
        ? header?.getBoundingClientRect().height
        : 0;
      navigation.style.top = height ? `${height}px` : "";
    }
    function onStandaloneChange() {
      syncOffset();
      schedule();
    }

    function update() {
      frame = 0;
      vertical = desktop.matches;
      const threshold =
        Math.max(
          parseFloat(getComputedStyle(targets[0]!).scrollMarginTop),
          desktop.matches
            ? parseFloat(getComputedStyle(navigation).top)
            : navigation.getBoundingClientRect().bottom,
        ) + 1;
      let index = 0;
      for (let i = 0; i < targets.length; i++) {
        if (targets[i]!.getBoundingClientRect().top <= threshold) index = i;
      }
      const bottom = document.documentElement.scrollHeight - innerHeight;
      if (bottom > 0 && scrollY >= bottom - 2) index = targets.length - 1;
      active = sections[index]!.id;
      // Scroll only the horizontal strip; scrollIntoView would move the page.
      const link = links.querySelector<HTMLAnchorElement>(
        `a[href="#${active}"]`,
      )!;
      const item = link.getBoundingClientRect();
      const strip = links.getBoundingClientRect();
      if (item.left < strip.left) links.scrollLeft -= strip.left - item.left;
      else if (item.right > strip.right)
        links.scrollLeft += item.right - strip.right;
    }
    function schedule() {
      if (!frame) frame = requestAnimationFrame(update);
    }
    function followHash() {
      const target = targets.find(
        (element) => `#${element.id}` === location.hash,
      );
      if (target) {
        target.scrollIntoView({ block: "start" });
        target.focus({ preventScroll: true });
      }
      schedule();
    }

    for (const target of targets) target.tabIndex = -1;
    const observer = new ResizeObserver((entries) => {
      if (header && entries.some(({ target }) => target === header))
        syncOffset();
      schedule();
    });
    observer.observe(document.getElementById("settings-content")!);
    observer.observe(navigation);
    if (header) observer.observe(header, { box: "border-box" });
    window.addEventListener("scroll", schedule, { passive: true });
    window.addEventListener("resize", schedule);
    window.addEventListener("hashchange", followHash);
    standalone.addEventListener("change", onStandaloneChange);
    syncOffset();
    followHash();
    return () => {
      cancelAnimationFrame(frame);
      observer.disconnect();
      window.removeEventListener("scroll", schedule);
      window.removeEventListener("resize", schedule);
      window.removeEventListener("hashchange", followHash);
      standalone.removeEventListener("change", onStandaloneChange);
    };
  });
</script>

<div bind:this={navigation} class="settings-navigation">
  <p
    class="mb-3 hidden px-3 text-xs font-medium text-muted-foreground lg:block"
  >
    设置分区
  </p>
  <div bind:this={links} class="settings-links">
    <NavigationMenu.Root
      aria-label="设置分区"
      layout="sections"
      viewport={false}
      orientation={vertical ? "vertical" : "horizontal"}
    >
      <NavigationMenu.List layout="sections">
        {#each sections as section (section.id)}
          <NavigationMenu.Item>
            <NavigationMenu.Link
              href={`#${section.id}`}
              variant="section"
              active={active === section.id}
              aria-current={active === section.id ? "location" : undefined}
              >{section.label}</NavigationMenu.Link
            >
          </NavigationMenu.Item>
        {/each}
      </NavigationMenu.List>
    </NavigationMenu.Root>
  </div>
</div>

<style>
  .settings-navigation {
    position: sticky;
    top: 0;
    z-index: 10;
    min-width: 0;
    align-self: start;
    padding-block: 0.75rem;
    border-bottom: 1px solid var(--border);
    background: var(--background);
  }
  .settings-links {
    overflow-x: auto;
    padding: 0.25rem;
    scrollbar-width: thin;
  }
  @media (min-width: 1024px) {
    .settings-navigation {
      top: 1rem;
      margin-top: 2rem;
      border-bottom: 0;
    }
  }
</style>
