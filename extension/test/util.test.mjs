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
const { dispositionName, mediaKind, sourceLabel, toSource, archivePlaylist, mergeGroups, isMediaUrl, human } = await import(
  "data:text/javascript," + encodeURIComponent(out.outputFiles[0].text)
);

test("dispositionName", () => {
  assert.equal(dispositionName('attachment; filename="report.pdf"'), "report.pdf");
  assert.equal(dispositionName("attachment; filename=plain.iso"), "plain.iso");
  assert.equal(dispositionName("attachment; filename=\"a.txt\"; filename*=UTF-8''%C3%A7%C4%B1kt%C4%B1.txt"), "çıktı.txt");
  assert.equal(dispositionName("inline"), "");
  assert.equal(dispositionName(""), "");
});

test("human", () => {
  assert.equal(human(0), "0 B");
  assert.equal(human(512), "512 B");
  assert.equal(human(156.5 * 1024 * 1024), "157 MiB");
});

test("mediaKind", () => {
  assert.equal(mediaKind("blob:https://x/1"), null);
  assert.equal(mediaKind("data:video/mp4;base64,AA"), null);
  assert.equal(mediaKind("https://x/v.mp4?t=1"), "file");
  assert.equal(mediaKind("https://x/master.m3u8"), "stream");
  assert.equal(mediaKind("https://x/play", "application/x-mpegURL"), "stream");
  assert.equal(mediaKind("https://x/manifest.mpd"), "stream");
});

test("isMediaUrl", () => {
  assert.equal(isMediaUrl("https://archive.org/download/Cops1922/Cops-v2.ogv"), true);
  assert.equal(isMediaUrl("https://archive.org/embed/Cops1922"), false);
  assert.equal(isMediaUrl("https://x/a.webm?sig=1#t=3"), true);
});

test("sourceLabel", () => {
  assert.equal(sourceLabel({ url: "https://x/a.mp4", label: "480p", type: "mp4" }), "480p · MP4");
  assert.equal(sourceLabel({ url: "https://x/a", type: "video/webm", height: "720" }), "720p · WEBM");
  assert.equal(sourceLabel({ url: "https://x/clip_1080p.mp4" }), "1080p · MP4");
  assert.equal(sourceLabel({ url: "https://x/a.mp4", label: "HD MP4" }), "HD MP4");
  assert.equal(sourceLabel({ url: "https://x/stream/%C3%A7" }), "ç");
});

test("toSource resolves relative URLs and drops blob:", () => {
  const s = toSource({ url: "/download/a.mp4", label: "480p" }, "https://archive.org/details/a");
  assert.deepEqual(s, { url: "https://archive.org/download/a.mp4", label: "480p · MP4", kind: "file", height: 480 });
  assert.equal(toSource({ url: "blob:https://x/1" }, "https://x/"), null);
});

// The real attribute from https://archive.org/details/Cops1922 (2026-11-13).
const COPS =
  '[{"title":"Cops-v2","orig":"Cops-v2.mp4","image":"/download/Cops1922/Cops1922.thumbs%2FCops-v2_000114.jpg","duration":1092,' +
  '"sources":[{"file":"/download/Cops1922/Cops-v2.mp4","type":"mp4","height":"480","width":"640","label":"480p"},' +
  '{"file":"/download/Cops1922/Cops-v2.ogv","type":"ogg","height":"300","width":"400","label":"300p"}],' +
  '"tracks":[{"kind":"thumbnails","file":"/stream/Cops1922/Cops1922.thumbs/Cops-v2_000114.jpg?vtt=vtt.vtt"}]}]';

test("archivePlaylist", () => {
  const g = archivePlaylist(COPS, "https://archive.org/details/Cops1922");
  assert.equal(g.length, 1);
  assert.equal(g[0].title, "Cops-v2");
  assert.deepEqual(
    g[0].sources.map((s) => [s.url, s.label]),
    [
      ["https://archive.org/download/Cops1922/Cops-v2.mp4", "480p · MP4"],
      ["https://archive.org/download/Cops1922/Cops-v2.ogv", "300p · OGV"],
    ],
  );
  assert.deepEqual(archivePlaylist("not json", "https://archive.org/"), []);
  assert.deepEqual(archivePlaylist('{"a":1}', "https://archive.org/"), []);
});

test("mergeGroups dedupes, marks playing, sorts and drops empty groups", () => {
  const base = "https://archive.org/details/Cops1922";
  const site = archivePlaylist(COPS, base);
  const own = { title: "This video", sources: [toSource({ url: "/download/Cops1922/Cops-v2.mp4", height: 480 }, base)] };
  const page = {
    title: "Other",
    sources: [
      toSource({ url: "https://x/live.m3u8" }, base),
      toSource({ url: "https://x/low_240p.mp4" }, base),
      toSource({ url: "https://x/hi_720p.mp4" }, base),
    ],
  };
  const m = mergeGroups([...site, own, page], "https://archive.org/download/Cops1922/Cops-v2.mp4");
  assert.equal(m.length, 2, "own group became empty and was dropped");
  assert.equal(m[0].sources[0].playing, true);
  assert.equal(m[0].sources[0].label, "480p · MP4", "the site's label wins");
  assert.deepEqual(
    m[1].sources.map((s) => s.url),
    ["https://x/hi_720p.mp4", "https://x/low_240p.mp4", "https://x/live.m3u8"],
  );
});
