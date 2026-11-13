// Runs in the page's own JavaScript world (manifest "world": "MAIN") before the
// page's scripts, in every frame. Players fetch HLS playlists with fetch()/XHR,
// often from URLs that say nothing (".../w/<token>", application/octet-stream)
// and only work inside this session. So the playlist is recognised by its
// content ("#EXTM3U") as the player receives it and handed to content.ts with
// window.postMessage. Nothing else is read or sent anywhere.
//
// Kept tiny and defensive: it wraps the page's own fetch/XHR, so any exception
// here would break the site.
(() => {
  const TAG = "__odm_hls__";
  const MAX = 16 << 20;
  const MAGIC = "#EXTM3U";
  // content.ts loads at document_idle, often after the player fetched its
  // playlists: keep the last few and send them again when it asks.
  const seen: { url: string; text: string }[] = [];
  const post = (url: string, text: string) => {
    try {
      window.postMessage({ [TAG]: 1, url, text }, "*"); // to this window only
    } catch {
      /* never break the page */
    }
  };
  const report = (url: string, text: string) => {
    seen.push({ url, text });
    if (seen.length > 20) seen.shift();
    post(url, text);
  };
  window.addEventListener("message", (e) => {
    if (e.source === window && (e.data as { __odm_hls_replay__?: number } | null)?.__odm_hls_replay__ === 1) {
      for (const s of seen) post(s.url, s.text);
    }
  });
  const startsM3U = (head: string) => head.replace(/^﻿/, "").startsWith(MAGIC);

  // fetch: peek at the first chunk of a clone; segments are dropped at once.
  const origFetch = window.fetch;
  if (typeof origFetch === "function") {
    window.fetch = function (this: unknown, ...args: Parameters<typeof fetch>) {
      const p = origFetch.apply(this, args);
      p.then(async (r) => {
        try {
          if (Number(r.headers.get("content-length") || 0) > MAX || !r.body) return;
          if (/^(image|video|audio|font)\//i.test(r.headers.get("content-type") || "") && !/\.m3u8/i.test(r.url)) {
            // Disguised playlists are octet-stream/text; segments often pose as images.
            return;
          }
          const rd = r.clone().body!.getReader();
          const first = await rd.read();
          if (first.done || !first.value || !startsM3U(new TextDecoder().decode(first.value.subarray(0, 16)))) {
            void rd.cancel();
            return;
          }
          const parts: Uint8Array[] = [first.value];
          let size = first.value.length;
          for (;;) {
            const c = await rd.read();
            if (c.done) break;
            size += c.value.length;
            if (size > MAX) {
              void rd.cancel();
              return;
            }
            parts.push(c.value);
          }
          const all = new Uint8Array(size);
          let o = 0;
          for (const part of parts) {
            all.set(part, o);
            o += part.length;
          }
          report(r.url, new TextDecoder().decode(all));
        } catch {
          /* ignore */
        }
      }, () => {});
      return p;
    } as typeof fetch;
  }

  // XHR: look at the response once it has loaded.
  const proto = XMLHttpRequest.prototype;
  const origSend = proto.send;
  proto.send = function (this: XMLHttpRequest, ...args: Parameters<XMLHttpRequest["send"]>) {
    try {
      this.addEventListener("load", function (this: XMLHttpRequest) {
        try {
          const rt = this.responseType;
          if (rt === "" || rt === "text") {
            const t = this.responseText;
            if (t.length <= MAX && startsM3U(t.slice(0, 16))) report(this.responseURL, t);
          } else if (rt === "arraybuffer" && this.response instanceof ArrayBuffer) {
            const b = this.response;
            if (b.byteLength <= MAX && startsM3U(new TextDecoder().decode(new Uint8Array(b, 0, Math.min(16, b.byteLength))))) {
              report(this.responseURL, new TextDecoder().decode(b));
            }
          }
        } catch {
          /* ignore */
        }
      });
    } catch {
      /* ignore */
    }
    return origSend.apply(this, args);
  };
})();
