import { LOCAL_URL, ensureLocal, ext, loadSettings, localClient, versionWarning } from "./api";

const $ = <T extends HTMLElement>(id: string) => document.getElementById(id) as T;

function human(n: number) {
  if (n < 1024) return `${n} B`;
  const u = "KMGTP";
  let i = -1;
  do {
    n /= 1024;
    i++;
  } while (n >= 1024 && i < u.length - 1);
  return `${n.toFixed(1)} ${u[i]}iB`;
}

async function refresh() {
  const list = $("list");
  try {
    const odm = localClient();
    const items = (await odm.list()).reverse().slice(0, 15);
    list.replaceChildren(
      ...items.map((d) => {
        const li = document.createElement("li");
        const pct = d.size > 0 ? Math.floor((d.downloaded * 100) / d.size) : 0;
        const name = document.createElement("div");
        name.className = "name";
        name.textContent = d.filename || d.request.url; // textContent: never innerHTML for remote data
        const meta = document.createElement("div");
        meta.className = "meta";
        meta.textContent = `${d.status} - ${human(d.downloaded)}${d.size > 0 ? ` / ${human(d.size)} (${pct}%)` : ""}`;
        li.append(name, meta);
        return li;
      }),
    );
    if (!items.length) list.textContent = "No downloads yet.";
    $("ver").textContent = versionWarning(await odm.info());
    $("err").textContent = "";
    $("start").hidden = true;
  } catch (e) {
    list.replaceChildren();
    $("err").textContent = `ODM is not running (${(e as Error).message}).`;
    $("start").hidden = false;
  }
}

async function showLast() {
  const { lastEvent } = (await ext.storage.local.get("lastEvent")) as {
    lastEvent?: { t: number; ok: boolean; msg: string };
  };
  const el = $("last");
  if (!lastEvent) {
    el.textContent = "No downloads captured yet.";
    return;
  }
  el.textContent = `${new Date(lastEvent.t).toLocaleTimeString()} - ${lastEvent.msg}`;
  el.className = lastEvent.ok ? "ok" : "bad";
}

async function init() {
  const s = await loadSettings();
  const toggle = $<HTMLInputElement>("enabled");
  toggle.checked = s.enabled;
  toggle.addEventListener("change", () => ext.storage.local.set({ enabled: toggle.checked }));
  $("open").addEventListener("click", () => ext.tabs.create({ url: LOCAL_URL }));
  $("start").addEventListener("click", async () => {
    $("err").textContent = "Starting ODM...";
    try {
      await ensureLocal();
    } catch (e) {
      $("err").textContent = (e as Error).message;
      return;
    }
    refresh();
  });
  $("settings").addEventListener("click", () => ext.runtime.openOptionsPage());
  refresh();
  showLast();
  setInterval(() => {
    refresh();
    showLast();
  }, 1500);
}

init();
