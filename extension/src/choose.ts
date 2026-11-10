import { OdmClient, ensureLocal, ext, loadSettings, remoteOf, report, scratch, type Pending } from "./api";

const $ = <T extends HTMLElement>(id: string) => document.getElementById(id) as T;

const id = new URLSearchParams(location.search).get("id") ?? "";
const key = `pending:${id}`;

function fileName(url: string): string {
  try {
    const last = new URL(url).pathname.split("/").filter(Boolean).pop();
    return last ? decodeURIComponent(last) : new URL(url).host;
  } catch {
    return url;
  }
}

async function main() {
  const rec = ((await scratch.get(key)) as Record<string, Pending>)[key];
  if (!rec) {
    $("what").textContent = "Nothing to download here any more.";
    return;
  }
  const remote = remoteOf(await loadSettings());
  $("what").textContent = fileName(rec.req.url);
  $("from").textContent = new URL(rec.req.url).host;
  $("remote-label").textContent = remote ? remote.label : "";
  if (!remote) $("btn-remote").hidden = true;

  const buttons = ["btn-local", "btn-remote", "btn-browser"].map((b) => $<HTMLButtonElement>(b));
  const status = $("status");

  async function send(kind: "local" | "remote" | "browser") {
    buttons.forEach((b) => (b.disabled = true));
    status.className = "";
    status.textContent = kind === "local" ? "Starting ODM if needed..." : "Sending...";
    try {
      if (kind === "local") await (await ensureLocal()).add(rec.req);
      else if (kind === "remote" && remote) await new OdmClient(remote.url, remote.token).add(rec.req);
      else if (kind === "browser") {
        const r = (await ext.runtime.sendMessage({ type: "browser-download", url: rec.req.url })) as
          | { ok: boolean; error?: string }
          | undefined;
        if (r && !r.ok) throw new Error(r.error ?? "browser download failed");
      }
      await scratch.remove(key); // before closing, so the window-closed handler sees it as a decision
      await report(true, kind === "browser" ? `Left to the browser: ${rec.req.url}` : `Sent to ${kind === "local" ? "this computer" : remote?.label}: ${rec.req.url}`);
      window.close();
    } catch (e) {
      status.className = "bad";
      status.textContent = (e as Error).message;
      buttons.forEach((b) => (b.disabled = false));
    }
  }

  $("btn-local").addEventListener("click", () => send("local"));
  $("btn-remote").addEventListener("click", () => send("remote"));
  $("btn-browser").addEventListener("click", () => send("browser"));
  document.addEventListener("keydown", (e) => {
    if (e.key === "1") send("local");
    else if (e.key === "2" && remote) send("remote");
    else if (e.key === "3") send("browser");
    else if (e.key === "Escape") window.close();
  });
  $("btn-local").focus();
}

main();
