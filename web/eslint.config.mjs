// Frontend architecture guard: design-system rules for Svelte templates.
//
// Oxlint stays the general linter (`pnpm lint`); it reads only <script>
// blocks, so this ESLint config runs just the template-aware architecture
// rules (`pnpm arch:lint`). See docs/frontend-architecture.md.
import { plugin as shadcn } from "@shadcn/lint";
import tsParser from "@typescript-eslint/parser";
import { defineConfig } from "eslint/config";
import svelte from "eslint-plugin-svelte";
import svelteParser from "svelte-eslint-parser";

// Must match `aliases.ui` in components.json.
const UI_DIR = "src/lib/components/ui";
const SHARED_DIR = "src/components";

const NOTE =
  `Look in ${UI_DIR} (shadcn-svelte primitives) and ${SHARED_DIR} ` +
  "(business components) before writing new UI. Rules and approved " +
  "exceptions: docs/frontend-architecture.md.";

// Native elements that have a design-system replacement.
const RAW_TO_DS = {
  button:
    "<Button> ($lib/components/ui/button), or <ListButton> " +
    "($lib/components/ui/list-button) for rows, tabs and cards inside lists",
  input:
    "<Input> ($lib/components/ui/input), or <Checkbox> " +
    "($lib/components/ui/checkbox) for checkboxes",
  textarea: "<Textarea> ($lib/components/ui/textarea)",
  select: "<NativeSelect> ($lib/components/ui/native-select)",
  dialog: "<Dialog> or ResponsiveDialog ($lib/components/ui/dialog)",
  label: "<Label> ($lib/components/ui/label)",
};
const rawMessage = (element) =>
  `Do not hand-write <${element}>; use ${RAW_TO_DS[element]}. ` +
  "If it does not fit, add a variant to that component instead.";

const restrictedImports = [
  "error",
  {
    patterns: [
      {
        group: ["bits-ui", "vaul-svelte", "paneforge"],
        message: `Headless primitives are wrapped only in ${UI_DIR}; import the wrapper from $lib/components/ui/*.`,
      },
      {
        group: ["@tabler/icons-svelte", "@tabler/icons-svelte/*"],
        message:
          'Import icons through Iconify, e.g. `import IconX from "~icons/tabler/x"`, so only the used icons are bundled (docs/design-system.md#icons).',
      },
      {
        group: ["tailwind-variants", "class-variance-authority"],
        message: `Variant helpers belong to ${UI_DIR}; add a variant to the existing component instead.`,
      },
    ],
  },
];

export default defineConfig([
  {
    ignores: [
      "dist/**",
      "node_modules/**",
      "storybook-static/**",
      "playwright-report/**",
      "test-results/**",
      ".vitest/**",
      "reports/**",
      // shadcn-svelte helper; its disable comments name typescript-eslint rules.
      "src/lib/utils.ts",
    ],
  },
  ...svelte.configs.base,
  {
    files: ["src/**/*.svelte", "src/**/*.svelte.ts"],
    languageOptions: {
      parser: svelteParser,
      parserOptions: { parser: tsParser },
    },
  },
  {
    files: ["src/**/*.ts"],
    ignores: ["src/**/*.svelte.ts"],
    languageOptions: { parser: tsParser },
  },
  {
    files: ["src/**/*.{ts,svelte}"],
    plugins: { shadcn },
    settings: { shadcn: { note: NOTE } },
    rules: {
      // Components own their appearance; call sites may only place them.
      "shadcn/no-restyle": [
        "error",
        {
          allow: ["layout"],
          contracts: [
            {
              pattern: "^CardTitle$",
              allow: ["layout", "typography"],
              deny: ["font-*"],
            },
            // Content regions: their padding and gaps depend on what they hold.
            {
              pattern:
                "^(CardContent|TabsContent|CommandList|ItemGroup|ItemActions|Collapsible|CollapsibleContent|DialogContent|DialogHeader|PopoverContent|ResponsiveDialog)$",
              allow: ["layout", "spacing"],
            },
            // Unstyled bits-ui triggers: the design system gives them no look
            // of its own, so the call site owns it. Prefer a Button `child`
            // when the trigger should look like a button.
            {
              pattern: "^(CollapsibleTrigger|PopoverTrigger)$",
              allow: [
                "layout",
                "spacing",
                "typography",
                "color",
                "shape",
                "effects",
              ],
            },
            // Layout hooks that responsive CSS in styles.css and the owning
            // component's <style> targets (sizes, overflow and visibility).
            {
              pattern: "^Pane$",
              allow: ["layout", "terminal-pane", "draft-pane", "code-pane"],
            },
            {
              pattern: "^ResizableHandle$",
              allow: ["layout", "draft-divider", "code-divider"],
            },
          ],
        },
      ],
      "shadcn/no-raw-colors": "error",
      // Viewport math such as `max-h-[50dvh]` is layout, not a design token;
      // spacing, typography, colour and shape stay on the theme scale.
      "shadcn/no-arbitrary-values": ["error", { allow: ["layout"] }],
      "shadcn/no-inline-styles": "error",
      "shadcn/no-unknown-classes": "error",
      "shadcn/require-static-classes": "error",
      "no-restricted-imports": restrictedImports,
    },
  },
  {
    files: ["src/**/*.svelte"],
    rules: {
      "svelte/no-restricted-html-elements": [
        "error",
        ...Object.keys(RAW_TO_DS).map((element) => ({
          elements: [element],
          message: rawMessage(element),
        })),
      ],
    },
  },
  // The design system itself wraps native elements and primitives.
  {
    files: [`${UI_DIR}/**`],
    rules: {
      "shadcn/no-restyle": "off",
      "shadcn/no-arbitrary-values": "off",
      "shadcn/no-inline-styles": "off",
      "shadcn/require-static-classes": "off",
      "no-restricted-imports": "off",
    },
  },
  {
    files: [`${UI_DIR}/**/*.svelte`],
    rules: { "svelte/no-restricted-html-elements": "off" },
  },
]);
