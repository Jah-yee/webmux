// Frontend architecture guard: import direction between layers.
// Run with `pnpm arch:deps`. See docs/frontend-architecture.md.
//
// Layers (the source tree is flat by design; there is no features/ split):
//   src/lib/components/ui/  shadcn-svelte primitives, the design-system leaf
//   src/lib/                framework-free helpers (cn, touch targets)
//   src/components/         business components shared by pages
//   src/hooks/              resource-owning runes (terminal, viewport, ...)
//   src/pages/              route pages, composed only by App.svelte
//   src/*.ts                domain logic, schemas, transport and storage

const PRIMITIVES = "node_modules/(bits-ui|vaul-svelte|paneforge)/";
const VARIANTS = "node_modules/(tailwind-variants|class-variance-authority)/";

/** @type {import('dependency-cruiser').IConfiguration} */
module.exports = {
  forbidden: [
    {
      name: "no-circular",
      severity: "error",
      comment:
        "Import cycles blur ownership; move the shared part down a layer.",
      // shadcn-svelte barrels re-export their own parts; that cycle is generated.
      from: { pathNot: "^src/lib/components/ui/" },
      to: { circular: true },
    },
    {
      name: "ui-is-a-leaf",
      severity: "error",
      comment:
        "Design-system components must not depend on business code; " +
        "otherwise they cannot be reused or regenerated safely.",
      from: { path: "^src/lib/components/ui/" },
      to: { path: "^src/", pathNot: "^src/lib/" },
    },
    {
      name: "lib-is-framework-free",
      severity: "error",
      comment:
        "src/lib helpers must not reach into business components, hooks or pages.",
      from: { path: "^src/lib/", pathNot: "^src/lib/components/ui/" },
      to: { path: "^src/(components|hooks|pages)/" },
    },
    {
      name: "only-app-composes-pages",
      severity: "error",
      comment:
        "Pages are composed by App.svelte; share code through components or hooks instead.",
      from: { pathNot: "^src/(App\\.svelte|pages/)" },
      to: { path: "^src/pages/" },
    },
    {
      name: "components-do-not-know-pages",
      severity: "error",
      comment:
        "Shared components receive page state through props, not page imports.",
      from: { path: "^src/components/" },
      to: { path: "^src/pages/" },
    },
    {
      name: "hooks-do-not-render",
      severity: "error",
      comment:
        "Hooks own resources and state; rendering belongs to components and pages.",
      from: { path: "^src/hooks/" },
      to: { path: "\\.svelte$" },
    },
    {
      name: "domain-does-not-render",
      severity: "error",
      comment:
        "Top-level domain modules (schemas, transport, storage) must stay " +
        "independent of Svelte components so they remain unit-testable.",
      from: { path: "^src/[^/]+\\.ts$", pathNot: "^src/main\\.ts$" },
      to: { path: "\\.svelte$" },
    },
    {
      name: "primitives-only-in-ui",
      severity: "error",
      comment:
        "Headless primitives are wrapped only in src/lib/components/ui " +
        "(backs up the ESLint no-restricted-imports rule).",
      from: { pathNot: "^src/lib/components/ui/" },
      to: { path: PRIMITIVES },
    },
    {
      name: "variants-only-in-ui",
      severity: "error",
      comment:
        "Variant helpers define looks only in src/lib/components/ui; add a variant there.",
      from: { pathNot: "^src/lib/components/ui/" },
      to: { path: VARIANTS },
    },
    {
      name: "no-orphans",
      severity: "error",
      comment:
        "An unused module is usually a leftover after a rewrite; delete it.",
      from: {
        orphan: true,
        pathNot: [
          "\\.d\\.ts$",
          "^src/main\\.ts$",
          "^src/lib/components/ui/", // generated on demand by shadcn-svelte
          "\\.css$",
        ],
      },
      to: {},
    },
  ],
  options: {
    doNotFollow: { path: "node_modules" },
    exclude: { path: "^(dist|storybook-static)/" },
    tsConfig: { fileName: "tsconfig.json" },
    tsPreCompilationDeps: true,
    enhancedResolveOptions: {
      exportsFields: ["exports"],
      conditionNames: [
        "import",
        "require",
        "node",
        "default",
        "types",
        "svelte",
      ],
      extensions: [".ts", ".js", ".svelte"],
    },
    reporterOptions: { text: { highlightFocused: true } },
  },
};
