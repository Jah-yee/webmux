import { cp, mkdir, realpath } from "node:fs/promises";
import { createRequire } from "node:module";
import { dirname, join, resolve } from "node:path";

const destination = resolve(process.argv[2]);
const web = resolve("web");
const playwright = await realpath(join(web, "node_modules/playwright"));
const require = createRequire(join(playwright, "package.json"));
await mkdir(join(destination, "web/node_modules/@playwright"), {
  recursive: true,
});
// Playwright's runner has only these three runtime packages. Copy the exact
// versions installed by the frozen pnpm lockfile, following pnpm's links.
for (const name of ["@playwright/test", "playwright", "playwright-core"]) {
  await cp(
    name === "playwright-core"
      ? dirname(require.resolve("playwright-core/package.json"))
      : await realpath(join(web, "node_modules", name)),
    join(destination, "web/node_modules", name),
    { recursive: true, dereference: true },
  );
}
await cp(join(web, "dist"), join(destination, "web/dist"), {
  recursive: true,
});
