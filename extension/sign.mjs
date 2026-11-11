// Signs dist/firefox with Mozilla as an *unlisted* add-on and puts the signed
// xpi where odm.exe embeds it (internal/extbundle/files/odm-firefox.xpi).
//
// Needs the AMO API credentials (https://addons.mozilla.org/developers/addon/api/key/),
// either in the environment as WEB_EXT_API_KEY / WEB_EXT_API_SECRET, or in the
// repo-root .env (gitignored) as MOZILLA_JWT_ISSUER / MOZILLA_JWT_KEY.
// web-ext reads the WEB_EXT_* variables itself. Each signed version is final:
// bump package.json first.
import { spawnSync } from "node:child_process";
import { copyFileSync, existsSync, readdirSync, readFileSync } from "node:fs";

// Minimal .env reader (KEY=value per line); values never get printed.
function loadDotEnv(path) {
  if (!existsSync(path)) return {};
  const out = {};
  for (const line of readFileSync(path, "utf8").split(/\r?\n/)) {
    const m = line.match(/^\s*([A-Za-z_][A-Za-z0-9_]*)\s*=\s*(.*?)\s*$/);
    if (m && !line.trimStart().startsWith("#")) out[m[1]] = m[2].replace(/^(['"])(.*)\1$/, "$2");
  }
  return out;
}
const dotenv = loadDotEnv("../.env");
process.env.WEB_EXT_API_KEY ||= dotenv.MOZILLA_JWT_ISSUER ?? "";
process.env.WEB_EXT_API_SECRET ||= dotenv.MOZILLA_JWT_KEY ?? "";

const { version } = JSON.parse(readFileSync("package.json", "utf8"));
if (!process.env.WEB_EXT_API_KEY || !process.env.WEB_EXT_API_SECRET) {
  console.error("No AMO credentials: set WEB_EXT_API_KEY/WEB_EXT_API_SECRET or MOZILLA_JWT_ISSUER/MOZILLA_JWT_KEY in .env.");
  process.exit(1);
}
const built = JSON.parse(readFileSync("dist/firefox/manifest.json", "utf8")).version;
if (built !== version) {
  console.error(`dist/firefox is ${built}, package.json is ${version}: run pnpm build first.`);
  process.exit(1);
}

const out = "dist/signed";
const r = spawnSync(
  "pnpm",
  ["dlx", "web-ext", "sign", "--channel", "unlisted", "--source-dir", "dist/firefox", "--artifacts-dir", out],
  { stdio: "inherit", shell: process.platform === "win32" },
);
if (r.status !== 0) process.exit(r.status ?? 1);

const xpi = existsSync(out) && readdirSync(out).find((f) => f.endsWith(`-${version}.xpi`));
if (!xpi) {
  console.error(`No signed xpi for ${version} in ${out}.`);
  process.exit(1);
}
copyFileSync(`${out}/${xpi}`, "../internal/extbundle/files/odm-firefox.xpi");
console.log(`signed ${xpi} -> internal/extbundle/files/odm-firefox.xpi (rebuild odm.exe to embed it)`);
