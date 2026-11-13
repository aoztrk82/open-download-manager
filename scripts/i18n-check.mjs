// Checks the UI translations: every language has the same keys as English, and
// every key the code uses exists. Run from the repo root: node scripts/i18n-check.mjs
import { readFileSync, readdirSync, existsSync } from "node:fs";
import vm from "node:vm";

let failed = false;
const fail = (msg) => {
  failed = true;
  console.error("FAIL " + msg);
};

function parity(name, dict) {
  const en = Object.keys(dict.en);
  for (const [lang, d] of Object.entries(dict)) {
    const keys = Object.keys(d);
    for (const k of en) if (!(k in d)) fail(`${name}: ${lang} is missing "${k}"`);
    for (const k of keys) if (!(k in dict.en)) fail(`${name}: ${lang} has extra "${k}"`);
    for (const k of keys) {
      const vars = (s) => [...String(s).matchAll(/\{(\w+)\}/g)].map((m) => m[1]).sort().join(",");
      if (k in dict.en && vars(d[k]) !== vars(dict.en[k])) fail(`${name}: ${lang} "${k}" placeholders differ from en`);
    }
  }
  return new Set(en);
}

function used(src, extraPatterns = []) {
  const out = new Set();
  const pats = [/data-i18n(?:-title|-ph)?="([\w.]+)"/g, /\bt\(\s*"([\w.]+)"\s*[,)]/g, ...extraPatterns];
  for (const re of pats) for (const m of src.matchAll(re)) out.add(m[1]);
  return out;
}

function check(name, keys, src, dynamic) {
  for (const k of used(src)) if (!keys.has(k)) fail(`${name}: uses unknown key "${k}"`);
  // keys built at runtime, e.g. t("status." + d.status)
  for (const [prefix, values] of Object.entries(dynamic))
    for (const v of values) if (!keys.has(prefix + v)) fail(`${name}: missing dynamic key "${prefix + v}"`);
  console.log(`ok ${name}: ${keys.size} keys`);
}

// ---- Web UI ----
{
  const ctx = { window: {} };
  vm.runInNewContext(readFileSync("internal/web/static/i18n.js", "utf8"), ctx);
  const keys = parity("web", ctx.window.I18N);
  const pages = ["index.html", "progress.html"].map((f) => readFileSync(`internal/web/static/${f}`, "utf8")).join("\n");
  check("web", keys, pages, {
    "status.": ["active", "queued", "paused", "completed", "failed"],
    "cs.": ["waiting", "connecting", "active", "retrying", "done", "stopped", "failed"],
    "set.tab.": ["general", "types", "save", "downloads", "conn", "queue", "log"],
    "add.when.": ["now", "main", "later", "paused"],
    "clr.": ["completed", "failed", "paused"],
    "cat.": ["video", "music", "compressed", "documents", "programs", "images"],
  });
}

// ---- Extension ----
if (existsSync("extension/src/i18n.ts")) {
  const ts = readFileSync("extension/src/i18n.ts", "utf8");
  const body = ts.match(/export const STRINGS[^=]*=\s*(\{[\s\S]*?\n\});/);
  if (!body) fail("extension: STRINGS object not found in src/i18n.ts");
  else {
    const dict = vm.runInNewContext("(" + body[1] + ")");
    const keys = parity("extension", dict);
    const src = ["src", "static"]
      .flatMap((d) => readdirSync(`extension/${d}`).filter((f) => /\.(ts|html)$/.test(f)).map((f) => `extension/${d}/${f}`))
      .filter((f) => !f.endsWith("i18n.ts"))
      .map((f) => readFileSync(f, "utf8"))
      .join("\n");
    check("extension", keys, src, { "status.": ["active", "queued", "paused", "completed", "failed"] });
  }
  for (const loc of readdirSync("extension/_locales")) {
    const m = JSON.parse(readFileSync(`extension/_locales/${loc}/messages.json`, "utf8"));
    for (const k of ["extName", "extDescription"]) if (!m[k]?.message) fail(`_locales/${loc}: missing ${k}`);
  }
}

if (failed) process.exit(1);
