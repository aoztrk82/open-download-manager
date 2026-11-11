import { LOCAL_URL, ensureLocal, ext, human, loadSettings, localClient, versionWarning, type Download } from "./api";
import { applyI18n, h, t } from "./i18n";

const $ = <T extends HTMLElement>(id: string) => document.getElementById(id) as T;

function setState(running: boolean) {
  const b = $("state");
  b.className = "badge " + (running ? "ok" : "");
  b.replaceChildren(h("i", "dot"), " " + (running ? t("pop.running") : t("pop.notRunning")));
  $("start").hidden = running;
}

function row(d: Download): HTMLLIElement {
  const li = document.createElement("li");
  const pct = d.size > 0 ? Math.min(100, (d.downloaded * 100) / d.size) : d.status === "completed" ? 100 : 0;
  const name = document.createElement("div");
  name.className = "name";
  name.textContent = d.filename || d.request.url; // textContent: never innerHTML for remote data
  name.title = d.request.url;
  const bar = document.createElement("div");
  bar.className = "bar " + d.status;
  const fill = h("i");
  fill.style.width = pct.toFixed(1) + "%";
  bar.append(fill);
  const meta = document.createElement("div");
  meta.className = "row";
  const st = document.createElement("span");
  st.className = "st-" + d.status;
  st.textContent = t(`status.${d.status}` as Parameters<typeof t>[0]);
  const size = document.createElement("span");
  size.textContent = d.size > 0 ? `${human(d.downloaded)} / ${human(d.size)} · ${Math.floor(pct)}%` : human(d.downloaded);
  meta.append(st, size);
  li.append(name, bar, meta);
  return li;
}

async function refresh() {
  try {
    const odm = localClient();
    const [items, info] = await Promise.all([odm.list(), odm.info()]);
    const recent = items.reverse().slice(0, 12);
    $("list").replaceChildren(...recent.map(row));
    $("empty").hidden = recent.length > 0;
    $("empty").textContent = t("pop.empty");
    const warn = versionWarning(info);
    $("ver").hidden = !warn;
    $("ver").textContent = warn;
    $("err").hidden = true;
    setState(true);
  } catch {
    $("list").replaceChildren();
    $("empty").hidden = false;
    $("empty").textContent = t("pop.notRunningHint");
    setState(false);
  }
}

async function showLast() {
  const { lastEvent } = (await ext.storage.local.get("lastEvent")) as {
    lastEvent?: { t: number; ok: boolean; msg: string };
  };
  const el = $("last");
  if (!lastEvent) {
    el.className = "";
    $("last-text").textContent = t("pop.noEvent");
    return;
  }
  el.className = lastEvent.ok ? "ok" : "bad";
  $("last-text").textContent = `${new Date(lastEvent.t).toLocaleTimeString()} · ${lastEvent.msg}`;
}

async function init() {
  const s = await loadSettings();
  applyI18n();
  const toggle = $<HTMLInputElement>("enabled");
  toggle.checked = s.enabled;
  toggle.addEventListener("change", () => ext.storage.local.set({ enabled: toggle.checked }));
  $("open").addEventListener("click", async () => {
    try {
      await ensureLocal(); // the Web UI is served by ODM itself
    } catch (e) {
      $("err").hidden = false;
      $("err").textContent = (e as Error).message;
      return;
    }
    await ext.tabs.create({ url: LOCAL_URL });
    window.close();
  });
  $("start").addEventListener("click", async () => {
    $("err").hidden = false;
    $("err").textContent = t("pop.starting");
    try {
      await ensureLocal();
      $("err").hidden = true;
    } catch (e) {
      $("err").textContent = (e as Error).message;
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
