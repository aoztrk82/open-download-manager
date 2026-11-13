// Bundles the shared TS once per target and drops in that target's manifest.
// The version comes from package.json only, so both browsers and odm.exe agree
// (AMO also refuses to sign a version twice: bump package.json before `pnpm sign`).
import { build } from "esbuild";
import { cpSync, mkdirSync, readFileSync, rmSync, writeFileSync } from "node:fs";

const { version } = JSON.parse(readFileSync("package.json", "utf8"));

rmSync("dist", { recursive: true, force: true });
for (const target of ["chrome", "firefox"]) {
  const out = `dist/${target}`;
  mkdirSync(out, { recursive: true });
  await build({
    entryPoints: {
      background: "src/background.ts",
      popup: "src/popup.ts",
      options: "src/options.ts",
      download: "src/download.ts",
    },
    outdir: out,
    bundle: true,
    format: "esm",
    target: "es2022",
    logLevel: "warning",
  });
  // Content scripts cannot be ES modules: a separate classic (IIFE) bundle.
  // hook.js runs in the page's world (manifest "world": "MAIN").
  await build({
    entryPoints: { content: "src/content.ts", hook: "src/hook.ts" },
    outdir: out,
    bundle: true,
    format: "iife",
    target: "es2022",
    logLevel: "warning",
  });
  cpSync("static", out, { recursive: true });
  cpSync("_locales", `${out}/_locales`, { recursive: true });
  const manifest = JSON.parse(readFileSync(`manifest.${target}.json`, "utf8"));
  manifest.version = version;
  writeFileSync(`${out}/manifest.json`, JSON.stringify(manifest, null, 2) + "\n");
}

// odm.exe embeds the Chrome/Edge build (internal/extbundle) and installs it
// from there, so the extension always matches the exe it came with.
const bundle = "../internal/extbundle/files/chrome";
rmSync(bundle, { recursive: true, force: true });
cpSync("dist/chrome", bundle, { recursive: true });

console.log(`built dist/chrome and dist/firefox (${version}); chrome copy embedded for odm.exe`);
