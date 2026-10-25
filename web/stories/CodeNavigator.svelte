<script lang="ts">
  import CodeNavigator from "../src/components/CodeNavigator.svelte";
  import { nodeId } from "../src/code-reference";
  import type { CodeListing } from "../src/schemas";
  let directory = $state("");
  let refresh = $state(0);
  let selected = $state("docs/architecture.md");
  const names: Record<string, string[]> = {
    "": [
      ".agents/",
      ".github/",
      "cmd/",
      "deploy/",
      "docs/",
      "internal/",
      "web/",
      ".gitignore",
      "AGENTS.md",
      "go.mod",
      "README.md",
    ],
    docs: [
      "images/",
      "architecture.md",
      "design-system.md",
      "frontend-architecture.md",
      "protocol.md",
      "usage.md",
      "user-stories.md",
    ],
    "docs/images": ["explorer.png"],
    web: ["src/", "package.json"],
    "web/src": ["components/", "App.svelte", "main.ts"],
  };
  function listing(path: string): CodeListing {
    return {
      children: (names[path] ?? ["README.md"]).map((entry) => {
        const folder = entry.endsWith("/");
        const name = entry.replace(/\/$/, "");
        const child = path ? `${path}/${name}` : name;
        return {
          nodeId: nodeId(child),
          path: child,
          name,
          kind: folder ? "directory" : "file",
          gitStatus: name === "design-system.md" ? "M" : "",
          ignored: false,
          referenceable: !folder,
          mediaType: name.endsWith(".png") ? "image/png" : "",
        };
      }),
      nextCursor: "",
      complete: true,
      limited: false,
      contentVersion: "demo",
      checkedAt: "2026-10-25T00:00:00Z",
    };
  }
  let current = $state(listing(""));
</script>

<div class="flex h-dvh min-h-0 bg-background text-foreground">
  <div
    class="code-inner flex min-h-0 min-w-0 flex-1"
    style:--code-nav-width="288px"
  >
    <CodeNavigator
      context={{
        contextId: "demo",
        rootPath: "/workspace/webmux",
        rootKind: "worktree",
        browseAvailable: true,
        diffAvailable: true,
        ignoreAvailable: true,
        reason: "",
      }}
      listing={current}
      rootListing={listing("")}
      treeRefresh={refresh}
      activePath={selected}
      fetchDirectory={async (path) => listing(path)}
      changes={undefined}
      changesError=""
      pending={false}
      changesEnabled={true}
      nav="files"
      query=""
      bind:directory
      includeIgnored={false}
      changeGroup=""
      changeQuery=""
      changeSearch=""
      load={() => {
        current = listing("");
        refresh++;
      }}
      openFile={(path) => {
        selected = path;
      }}
      openDiff={() => {}}
      locate={async () => {}}
      sessionCwd="/workspace/webmux"
      reportedCwd={null}
      navStatus=""
      reload={() => {
        current = listing("");
        refresh++;
      }}
    />
    <div class="hidden min-w-0 flex-1 flex-col md:flex">
      <div class="border-b px-4 py-2 text-xs">{selected} · 示例数据</div>
      <pre class="p-6 text-sm text-muted-foreground"># WebMux architecture

Read code alongside your terminal.
Expand folders to explore the workspace.</pre>
    </div>
  </div>
</div>
