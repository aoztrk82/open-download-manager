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
const {
  dispositionName, mediaKind, sourceLabel, toSource, jsonSources, mayHoldMedia, mergeGroups, isMediaUrl, human, fileNameFor,
  isHlsMaster, hlsVariants, hlsDuration, ytFormat, isYtdlpSite, cleanTitle,
  extOf, extOfFile, hostMatches, captureDecision, categoryDir,
} = await import(
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
  assert.equal(mediaKind("https://x/master.m3u8"), "hls");
  assert.equal(mediaKind("https://x/play", "application/x-mpegURL"), "hls");
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

// data-hope-video from https://www.showtv.com.tr/show-ozel-haber/saatini-satacakti-dolandirildi-922945
// (2026-11-13; ad settings shortened). The player plays the m3u8 through MSE, so
// <video> only shows blob:, but every quality is also a plain MP4.
const HOPE = JSON.stringify({
  id: "hope-video-922945", autoplay: true, title: "Saatini satacaktı dolandırıldı!",
  poster: "https://mo.ciner.com.tr/video/2026/11/12/ver1794510802/F03747F5175171D66A5D8BDAAB5A1C73_1920x1080.jpg",
  settings: { qrtrack: { enabled: true, bg: "/cover.jpg" } },
  adTagPreroll: "https://www.showtv.com.tr/vmap/video/922945",
  media: {
    m3u8: [{ label: "Auto", src: "https://vmcdn.ciner.com.tr/ht/2026/11/12/F03747F5175171D66A5D8BDAAB5A1C73.m3u8", type: "application/x-mpegURL" }],
    mp4: [240, 360, 480, 720, 1080].map((h) => ({
      label: `${h}p`,
      src: `https://vmcdn.ciner.com.tr/ht/2026/11/12/F03747F5175171D66A5D8BDAAB5A1C73_${{ 240: "426x240", 360: "640x360", 480: "854x480", 720: "1280x720", 1080: "1920x1080" }[h]}.mp4`,
      type: "video/mp4",
    })),
  },
});

test("mayHoldMedia", () => {
  assert.equal(mayHoldMedia(COPS), true);
  assert.equal(mayHoldMedia(HOPE), true);
  assert.equal(mayHoldMedia("https://x/a.mp4"), false, "a bare URL attribute is not a config");
  assert.equal(mayHoldMedia('{"poster":"https://x/a.jpg","n":12345}'), false);
  assert.equal(mayHoldMedia(null), false);
});

test("jsonSources: archive.org playlist", () => {
  const g = jsonSources(COPS, "https://archive.org/details/Cops1922");
  assert.equal(g.length, 1);
  assert.equal(g[0].title, "Cops-v2");
  assert.deepEqual(
    g[0].sources.map((s) => [s.url, s.label]),
    [
      ["https://archive.org/download/Cops1922/Cops-v2.mp4", "480p · MP4"],
      ["https://archive.org/download/Cops1922/Cops-v2.ogv", "300p · OGV"],
    ],
    'the bare "orig" file name and the thumbnail track are not sources',
  );
  assert.deepEqual(jsonSources("not json", "https://archive.org/"), []);
  assert.deepEqual(jsonSources('{"a":1}', "https://archive.org/"), []);
});

test("jsonSources: Show TV hope player", () => {
  const page = "https://www.showtv.com.tr/show-ozel-haber/saatini-satacakti-dolandirildi-922945";
  const [g] = mergeGroups(jsonSources(HOPE, page));
  assert.equal(g.title, "Saatini satacaktı dolandırıldı!");
  assert.deepEqual(
    g.sources.map((s) => [s.label, s.kind]),
    [["1080p · MP4", "file"], ["720p · MP4", "file"], ["480p · MP4", "file"], ["360p · MP4", "file"], ["240p · MP4", "file"], ["Auto · HLS", "hls"]],
  );
});

test("jsonSources: JSON-LD VideoObject and loose URLs", () => {
  const ld = { "@type": "VideoObject", name: "Clip", contentUrl: "https://cdn.x/clip.mp4", thumbnailUrl: "https://cdn.x/t.jpg" };
  assert.deepEqual(jsonSources(JSON.stringify(ld), "https://x/").map((g) => [g.title, g.sources[0].url]), [["Clip", "https://cdn.x/clip.mp4"]]);
  const loose = { player: { hls: "https://cdn.x/master.m3u8", fallback: "/rel/v.mp4" } };
  assert.deepEqual(jsonSources(JSON.stringify(loose), "https://x/")[0].sources.map((s) => s.url), ["https://cdn.x/master.m3u8"]);
});

test("fileNameFor", () => {
  const mp4 = { url: "https://vmcdn.ciner.com.tr/ht/F03747F5_1920x1080.mp4", label: "1080p · MP4", height: 1080, kind: "file" };
  assert.equal(fileNameFor("Saatini satacaktı dolandırıldı!", mp4), "Saatini satacaktı dolandırıldı! (1080p).mp4");
  assert.equal(fileNameFor('A/B: "C" | D?*. ', mp4), "A B C D (1080p).mp4", "Windows-illegal characters and trailing dots go");
  assert.equal(fileNameFor("", mp4), "F03747F5_1920x1080.mp4", "no title: the URL's name");
  assert.equal(fileNameFor("Cops-v2", { url: "https://archive.org/x/Cops-v2.ogv", height: 300, kind: "file" }), "Cops-v2 (300p).ogv");
  assert.equal(fileNameFor("Bölüm 1", { url: "https://x/w/token", height: 720, kind: "hls" }), "Bölüm 1 (720p).mp4", "streams become MP4");
  assert.equal(fileNameFor("x".repeat(300), mp4).length, 150 + " (1080p).mp4".length);
});

test("yt-dlp helpers", () => {
  assert.equal(ytFormat(0), "bv*+ba/b");
  assert.equal(ytFormat(-1), "ba[ext=m4a]/ba/b");
  assert.equal(ytFormat(720), "bv*[height<=720]+ba/b[height<=720]", "same selectors as ytdlp.FormatFor (Go)");
  for (const h of ["www.youtube.com", "m.youtube.com", "music.youtube.com", "youtu.be", "www.youtube-nocookie.com", "vimeo.com", "x.com"]) {
    assert.equal(isYtdlpSite(h), true, h);
  }
  for (const h of ["notyoutube.com", "youtube.com.evil.example", "www.showtv.com.tr"]) assert.equal(isYtdlpSite(h), false, h);
  assert.equal(cleanTitle("(3) Big Buck Bunny 60fps 4K - Official Blender Foundation Short Film - YouTube"),
    "Big Buck Bunny 60fps 4K - Official Blender Foundation Short Film");
  assert.equal(cleanTitle("Some clip | Vimeo"), "Some clip");
  const page = "https://www.youtube.com/watch?v=aqz-KE-bpKQ";
  assert.equal(fileNameFor("Big Buck Bunny", { url: page, height: 1080, kind: "ytdlp" }), "Big Buck Bunny (1080p).mp4");
  assert.equal(fileNameFor("Big Buck Bunny", { url: page, height: -1, kind: "ytdlp" }), "Big Buck Bunny.m4a", "audio only");
  assert.equal(fileNameFor("Big Buck Bunny", { url: page, height: 0, kind: "ytdlp" }), "Big Buck Bunny.mp4", "best: no fixed quality");
});

test("mergeGroups joins configs that name the same video", () => {
  const page = "https://www.showtv.com.tr/x";
  const ld = JSON.stringify({ "@type": "VideoObject", name: "Saatini satacaktı dolandırıldı!",
    contentUrl: "https://vmcdn.ciner.com.tr/ht/2026/11/12/F03747F5175171D66A5D8BDAAB5A1C73_1920x1080.mp4" });
  const groups = mergeGroups([...jsonSources(ld, page), ...jsonSources(HOPE, page)]);
  assert.equal(groups.length, 1);
  assert.equal(groups[0].sources.length, 6, "1080p from JSON-LD is not listed twice");
  assert.equal(groups[0].sources[0].label, "1080p · MP4");
});

// tranimeizle.live (JW Player), 2026-11-13: served from /w/<token> as application/octet-stream.
const ANIME_MASTER = `#EXTM3U
#EXT-X-VERSION:6
#EXT-X-STREAM-INF:BANDWIDTH=7823504,AVERAGE-BANDWIDTH=3114814,RESOLUTION=1920x1080,CODECS="avc1.4d4028,mp4a.40.2"
/w/TOKEN/v0

#EXT-X-STREAM-INF:BANDWIDTH=4577358,AVERAGE-BANDWIDTH=2004592,RESOLUTION=1280x720,CODECS="avc1.4d401f,mp4a.40.2"
/w/TOKEN/v1

#EXT-X-STREAM-INF:BANDWIDTH=2089974,AVERAGE-BANDWIDTH=949413,RESOLUTION=854x480,CODECS="avc1.4d401e,mp4a.40.2"
/w/TOKEN/v2
`;

test("HLS master playlist", () => {
  assert.equal(isHlsMaster(ANIME_MASTER), true);
  const v = hlsVariants(ANIME_MASTER, "https://www.tranimeizle.live/w/TOKEN");
  assert.deepEqual(v.map((x) => [x.url, x.height, x.bandwidth]), [
    ["https://www.tranimeizle.live/w/TOKEN/v0", 1080, 3114814],
    ["https://www.tranimeizle.live/w/TOKEN/v1", 720, 2004592],
    ["https://www.tranimeizle.live/w/TOKEN/v2", 480, 949413],
  ], 'CODECS="a,b" must not confuse the attribute parser');
  const media = "#EXTM3U\n#EXTINF:2.002000,\nhttps://sv65.x/file000.jpeg\n#EXTINF:1.5,\nhttps://sv27.x/file001.jpeg\n#EXT-X-ENDLIST\n";
  assert.equal(isHlsMaster(media), false);
  assert.equal(hlsDuration(media), 3.502);
});

test("mergeGroups: the playing mirror of a listed file is the same entry", () => {
  const base = "https://archive.org/details/Cops1922";
  const mirror = "https://ia800306.us.archive.org/12/items/Cops1922/Cops-v2.mp4";
  const own = { title: "This video", generic: true, sources: [toSource({ url: mirror, height: 480 }, base)] };
  const m = mergeGroups([...jsonSources(COPS, base), own], mirror);
  assert.equal(m.length, 1, "the mirror is not listed again");
  assert.equal(m[0].sources[0].playing, true, "the listed 480p gets the playing tag");
  // Generic names are not enough to call two files the same.
  const a = { title: "A", sources: [toSource({ url: "https://a.x/1/index.mp4" }, base)] };
  const b = { title: "B", sources: [toSource({ url: "https://b.x/2/index.mp4" }, base)] };
  assert.equal(mergeGroups([a, b]).length, 2);
});

test("mergeGroups dedupes, marks playing, sorts and drops empty groups", () => {
  const base = "https://archive.org/details/Cops1922";
  const site = jsonSources(COPS, base);
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

test("capture rules from ODM's Settings", () => {
  assert.equal(extOf("Report.PDF"), "pdf");
  assert.equal(extOf("noext"), "");
  assert.equal(extOfFile("", "application/pdf; charset=binary"), "pdf");
  assert.ok(hostMatches(["example.com"], "dl.Example.com"));
  assert.ok(!hostMatches(["example.com"], "badexample.com"));

  const r = {
    capture: { firefox: false },
    browserTypes: ["pdf"],
    autoTypes: ["iso"],
    autoSites: ["mirror.org"],
    skipSites: ["bank.com"],
  };
  const d = (url, name, page, browser = "chrome", mime = "") => captureDecision(r, browser, url, page, name, mime);
  assert.equal(d("https://x.com/a.zip", "a.zip"), "ask");
  assert.equal(d("https://x.com/a.zip", "a.zip", undefined, "firefox"), "browser");
  assert.equal(d("https://x.com/doc", "doc", undefined, "chrome", "application/pdf"), "browser");
  assert.equal(d("https://x.com/a.ISO", "a.ISO"), "auto");
  assert.equal(d("https://cdn.mirror.org/a.zip", "a.zip"), "auto");
  assert.equal(d("https://cdn.net/a.zip", "a.zip", "https://www.bank.com/statements"), "browser");
  assert.equal(captureDecision(null, "chrome", "https://x/a.pdf", undefined, "a.pdf"), "ask");

  const cats = {
    useCategories: true,
    categories: [{ id: "video", exts: ["mp4", "mkv"], dir: "D:\\Video" }, { id: "music", exts: ["mp3"], dir: "" }],
    categoryDirs: { video: "D:\\Video", music: "C:\\Users\\u\\Downloads\\Music" },
  };
  assert.equal(categoryDir(cats, "film.MKV"), "D:\\Video");
  assert.equal(categoryDir(cats, "song.mp3"), "C:\\Users\\u\\Downloads\\Music"); // inside the download folder
  assert.equal(categoryDir(cats, "notes.txt"), "");
  assert.equal(categoryDir(cats, "", true), "D:\\Video"); // a stream becomes an MP4
  assert.equal(categoryDir({ ...cats, useCategories: false }, "film.mkv"), "");
});
test("a <video src=\"\"> page address is never a source (mail.ru)", () => {
  const page = "https://my.mail.ru/mail/u/video/_myvideo/449.html?from=videoplayer";
  assert.equal(toSource({ url: page }, page), null);
  assert.equal(toSource({ url: page + "#t=5" }, page), null);
  assert.equal(toSource({ url: "" }, page), null); // resolves to the page itself
  assert.ok(toSource({ url: "https://cdn.mail.ru/v/1.mp4" }, page));
  assert.ok(isYtdlpSite("my.mail.ru"));
});