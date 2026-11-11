// pnpm test: bundles src/util.ts with esbuild and checks the pure helpers.
import { test } from "node:test";
import assert from "node:assert/strict";
import { build } from "esbuild";
import { fileURLToPath } from "node:url";

const out = await build({
  entryPoints: [fileURLToPath(new URL("../src/util.ts", import.meta.url))],
  bundle: true,
  format: "esm",
  write: false,
});
const { dispositionName } = await import("data:text/javascript," + encodeURIComponent(out.outputFiles[0].text));

test("dispositionName", () => {
  assert.equal(dispositionName('attachment; filename="report.pdf"'), "report.pdf");
  assert.equal(dispositionName("attachment; filename=plain.iso"), "plain.iso");
  assert.equal(dispositionName("attachment; filename=\"a.txt\"; filename*=UTF-8''%C3%A7%C4%B1kt%C4%B1.txt"), "çıktı.txt");
  assert.equal(dispositionName("inline"), "");
  assert.equal(dispositionName(""), "");
});
