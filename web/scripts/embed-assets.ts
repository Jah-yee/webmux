import { readdir, readFile, stat, writeFile } from "node:fs/promises";
import { resolve, join } from "node:path";
import { zipSync, type Zippable } from "fflate";
import type { Plugin } from "vite";

// Keep the ordinary dist tree usable by preview servers. Go embeds only this
// archive, whose paths retain the existing fs.Sub(Assets, "dist") contract.
export function embedAssets(): Plugin {
  let outDir: string;
  return {
    name: "webmux-embed-assets",
    apply: "build",
    configResolved(config) {
      outDir = resolve(config.root, config.build.outDir);
    },
    async writeBundle() {
      const files: Zippable = {};
      const names = (await readdir(outDir, { recursive: true })).sort();
      for (const name of names) {
        if (name === "embed.zip" || name.endsWith(".gitkeep")) continue;
        const path = join(outDir, name);
        // readdir also lists directories; only regular files are archived.
        if (!(await stat(path)).isFile()) continue;
        files[`dist/${name.replaceAll("\\", "/")}`] = [
          await readFile(path),
          {
            // WOFF2 and PNG are already compressed. Store them without an
            // additional decompression step on the first font request.
            level: /\.(woff2|png)$/.test(name) ? 0 : 9,
            mtime: new Date(1980, 0, 1),
          },
        ];
      }
      await writeFile(join(outDir, "embed.zip"), zipSync(files));
    },
  };
}
