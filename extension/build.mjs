// Bundles the shared TS once per target and drops in that target's manifest.
import { build } from "esbuild";
import { cpSync, mkdirSync, rmSync } from "node:fs";

rmSync("dist", { recursive: true, force: true });
for (const target of ["chrome", "firefox"]) {
  const out = `dist/${target}`;
  mkdirSync(out, { recursive: true });
  await build({
    entryPoints: {
      background: "src/background.ts",
      popup: "src/popup.ts",
      options: "src/options.ts",
      choose: "src/choose.ts",
    },
    outdir: out,
    bundle: true,
    format: "esm",
    target: "es2022",
    logLevel: "warning",
  });
  cpSync("static", out, { recursive: true });
  cpSync(`manifest.${target}.json`, `${out}/manifest.json`);
}
console.log("built dist/chrome and dist/firefox");
