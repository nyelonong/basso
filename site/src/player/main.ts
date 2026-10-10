import "@fontsource-variable/archivo";
import "@fontsource/fragment-mono";
import "./player.css";
import { SAMPLE_NAMES } from "./assets";
import { createWebAudioSink, type WebAudioSink } from "./audio";
import type { createEditor } from "./editor";
import { gridToFennel } from "./grid";
import { mountBarView, mountGridEditor } from "./grid-view";
import { Library, SHARED_ID, STARTER_ID, type Entry } from "./library";
import { stepSeconds, type AudioSink, type Bar, type Diagnostic, type GridPattern } from "./model";
import { Session } from "./session";
import {
  buildLink,
  decodePayload,
  describeShareError,
  encodePayload,
  readPayload,
  ShareError,
  shareableName,
  sharingSupported,
} from "./share";

const SELECTED_KEY = "basso.selected";
const HINT_KEY = "basso.hintDismissed";

function required<T extends Element>(selector: string): T {
  const element = document.querySelector<T>(selector);
  if (!element) throw new Error(`missing required element: ${selector}`);
  return element;
}

function storageOrNull(): Storage | null {
  try {
    const storage = window.localStorage;
    const probe = "basso.probe";
    storage.setItem(probe, probe);
    storage.removeItem(probe);
    return storage;
  } catch {
    return null;
  }
}

const storage = storageOrNull();
const remember = (key: string, value: string) => {
  try {
    storage?.setItem(key, value);
  } catch {
    // Remembering the selection or hint is a convenience; losing it is harmless.
  }
};
const recall = (key: string) => {
  try {
    return storage?.getItem(key) ?? null;
  } catch {
    return null;
  }
};

const transport = required<HTMLButtonElement>("[data-transport]");
const transportLabel = required<HTMLElement>("[data-transport-label]");
const bpmReadout = required<HTMLElement>("[data-bpm]");
const barReadout = required<HTMLElement>("[data-bar]");
const stepReadout = required<HTMLElement>("[data-step]");
const status = required<HTMLElement>("[data-status]");
const hint = required<HTMLElement>("[data-hint]");
const nameInput = required<HTMLInputElement>("[data-entry-name]");
const badge = required<HTMLElement>("[data-entry-badge]");
const convertButton = required<HTMLButtonElement>("[data-convert]");
const deleteButton = required<HTMLButtonElement>("[data-delete]");
const gridMode = required<HTMLElement>("[data-grid-mode]");
const codeMode = required<HTMLElement>("[data-code-mode]");
const diagnosticPanel = required<HTMLElement>("[data-diagnostic]");
const libraryToggle = required<HTMLButtonElement>("[data-library-toggle]");
const shareButton = required<HTMLButtonElement>("[data-share]");
const shareNotice = required<HTMLElement>("[data-share-notice]");
const shareFieldWrap = required<HTMLElement>("[data-share-field-wrap]");
const shareField = required<HTMLInputElement>("[data-share-field]");
const sharedBanner = required<HTMLElement>("[data-shared-banner]");
const sharedMessage = required<HTMLElement>("[data-shared-message]");
const sharedSave = required<HTMLButtonElement>("[data-shared-save]");

const SHARED_BANNER_TEXT = "Shared pattern. Press Play to hear it. Editing makes your own copy.";
const CODE_SHARE_TEXT = "Code patterns cannot be shared as links yet. Use Export .fnl.";

const library = new Library(storage);
let selected: Entry = library.get(recall(SELECTED_KEY) ?? "") ?? library.get(STARTER_ID)!;
let playing = false;
let audio: { ctx: AudioContext; sink: WebAudioSink } | null = null;
let loadingAudio: Promise<void> | null = null;
let loadingEditor = false;
let upcoming: { n: number; bar: Bar | null; start: number }[] = [];
let shownBar: number | null = null;
let openToken = 0;

function announce(message: string) {
  status.textContent = message;
}

function showBanner(kind: "shared" | "error", text: string) {
  sharedBanner.dataset.kind = kind;
  sharedMessage.textContent = text;
  sharedSave.hidden = kind === "error";
  sharedBanner.hidden = false;
}

function hideBanner() {
  sharedBanner.hidden = true;
}

function clearShareResult() {
  shareNotice.textContent = "";
  shareFieldWrap.hidden = true;
}

function clearLinkFromAddress() {
  // Until the runtime is ready the link has not been opened yet, so keep it.
  if (!session) return;
  if (readPayload(location.hash) !== null) history.replaceState(null, "", location.pathname + location.search);
}

function describe(diagnostic: Diagnostic): string {
  const where = [diagnostic.line && `line ${diagnostic.line}`, diagnostic.bar !== undefined && `bar ${diagnostic.bar + 1}`]
    .filter(Boolean)
    .join(", ");
  return where ? `${where}: ${diagnostic.message}` : diagnostic.message;
}

const sink: AudioSink = {
  playSample: (...args) => audio?.sink.playSample(...args),
  playNote: (...args) => audio?.sink.playNote(...args),
  stopAll: () => audio?.sink.stopAll(),
};

const gridEditor = mountGridEditor(required("[data-grid-editor]"), initialGrid(), onGridChange, SAMPLE_NAMES);
const barView = mountBarView(required("[data-bar-view]"));
type Editor = ReturnType<typeof createEditor>;
let editor: Editor | null = null;
let editorLoad: Promise<Editor> | null = null;

// CodeMirror is only downloaded once a code pattern is first opened.
function ensureEditor(): Promise<Editor> {
  editorLoad ??= import("./editor").then(({ createEditor }) => {
    editor = createEditor(required("[data-editor]"), {
      onSubmit: submitCode,
      onChange: () => {
        if (!loadingEditor) announce("Code changed. Press Update to hear it at the next bar.");
      },
    });
    return editor;
  });
  return editorLoad;
}

function showSource(entry: Entry & { kind: "code" }) {
  void ensureEditor().then((loaded) => {
    if (selected.id !== entry.id) return;
    loadingEditor = true;
    loaded.setSource(entry.source);
    loadingEditor = false;
  });
}

function initialGrid(): GridPattern {
  const starter = library.get(STARTER_ID);
  return starter?.kind === "grid" ? starter.grid : { bpm: 120, steps: 16, rows: [] };
}

let session: Session | null = null;

function ownCopy(): Entry {
  if (!selected.readOnly) return selected;
  const wasShared = selected.shared === true;
  const copy = library.duplicate(selected.id);
  if (wasShared) {
    library.closeShared();
    clearLinkFromAddress();
  }
  remember(SELECTED_KEY, copy.id);
  announce(`Saved your changes as ${copy.name}.`);
  return copy;
}

function onGridChange(grid: GridPattern) {
  if (selected.kind !== "grid") return;
  if (sharedBanner.dataset.kind === "error") hideBanner();
  const target = ownCopy();
  library.save({ ...target, kind: "grid", grid });
  selected = library.get(target.id)!;
  session?.updateGrid(grid);
  renderEntry({ keepEditors: true });
  if (playing) announce("Grid change queued for the next bar.");
}

function submitCode() {
  if (selected.kind !== "code" || !session || !editor) return;
  const source = editor.getSource();
  const diagnostic = session.updateCode(source);
  if (selected.readOnly && source === selected.source && !diagnostic) {
    announce(playing ? "Update queued for the next bar." : "Ready. Press Play.");
    return;
  }
  const target = ownCopy();
  library.save({ ...target, kind: "code", source });
  selected = library.get(target.id)!;
  renderEntry({ keepEditors: true });
  if (diagnostic) announce(`Not applied. ${describe(diagnostic)}`);
  else announce(playing ? "Update queued for the next bar." : "Saved. Press Play to hear it.");
}

function showDiagnostic(diagnostic: Diagnostic | null) {
  diagnosticPanel.textContent = diagnostic ? describe(diagnostic) : "";
  diagnosticPanel.dataset.error = String(diagnostic !== null);
}

function renderList() {
  const entries = library.list();
  const groups: [string, (entry: Entry) => boolean][] = [
    ["shared", (entry) => entry.shared === true],
    ["user", (entry) => !entry.readOnly],
    ["examples", (entry) => entry.readOnly && !entry.shared],
  ];
  for (const [name, include] of groups) {
    required<HTMLUListElement>(`[data-list="${name}"]`).replaceChildren(
      ...entries.filter(include).map((entry) => {
        const button = document.createElement("button");
        button.type = "button";
        button.className = "library-item";
        button.textContent = entry.name;
        const kind = document.createElement("span");
        kind.textContent = entry.kind;
        button.append(kind);
        if (entry.id === selected.id) button.setAttribute("aria-current", "true");
        button.addEventListener("click", () => select(entry.id));
        const item = document.createElement("li");
        item.append(button);
        return item;
      }),
    );
  }
  required<HTMLElement>("[data-shared-heading]").hidden = !entries.some((entry) => entry.shared);
  required<HTMLElement>("[data-user-empty]").hidden = entries.some((entry) => !entry.readOnly);
  required<HTMLElement>("[data-storage-note]").hidden = library.persistent;
}

function renderEntry({ keepEditors = false } = {}) {
  nameInput.value = selected.name;
  nameInput.readOnly = selected.readOnly;
  badge.hidden = !selected.readOnly;
  badge.textContent = selected.shared
    ? "Shared with you: edits make your own copy"
    : "Example: edits make your own copy";
  if (selected.shared) showBanner("shared", SHARED_BANNER_TEXT);
  else if (sharedBanner.dataset.kind === "shared") hideBanner();
  deleteButton.hidden = selected.readOnly;
  convertButton.hidden = selected.kind !== "grid";
  gridMode.hidden = selected.kind !== "grid";
  codeMode.hidden = selected.kind !== "code";
  if (!keepEditors) {
    if (selected.kind === "grid") gridEditor.setGrid(selected.grid);
    else showSource(selected);
  }
  renderList();
}

function select(id: string, { persist = true } = {}) {
  const entry = library.get(id);
  if (!entry) return;
  openToken++;
  selected = entry;
  if (!entry.shared && persist) remember(SELECTED_KEY, id);
  hideBanner();
  clearShareResult();
  if (!entry.shared) clearLinkFromAddress();
  renderEntry();
  showDiagnostic(null);
  if (session) {
    const diagnostic =
      entry.kind === "grid" ? session.updateGrid(entry.grid) : session.updateCode(entry.source);
    if (diagnostic) announce(`${entry.name} has an error. ${describe(diagnostic)}`);
    else announce(playing ? `${entry.name} starts at the next bar.` : `${entry.name} is ready. Press Play.`);
  }
  libraryToggle.setAttribute("aria-expanded", "false");
}

function setReadout(n: number | null, bar: Bar | null, step: number | null) {
  bpmReadout.textContent = bar ? String(bar.bpm) : selected.kind === "grid" ? String(selected.grid.bpm) : "---";
  barReadout.textContent = n === null ? "--" : String(n + 1).padStart(2, "0");
  stepReadout.textContent = step === null ? "--" : String(step + 1).padStart(2, "0");
}

function frame() {
  if (!playing || !audio) return;
  const now = audio.ctx.currentTime;
  const current = upcoming.filter((u) => u.start <= now).at(-1);
  if (current) {
    upcoming = upcoming.filter((u) => u.start >= current.start);
    if (shownBar !== current.n) {
      shownBar = current.n;
      barView.show(current.bar);
    }
    const bar = current.bar;
    const step = bar ? Math.min(bar.steps - 1, Math.floor((now - current.start) / stepSeconds(bar.bpm))) : null;
    gridEditor.setStep(step);
    barView.setStep(step);
    setReadout(current.n, bar, step);
  }
  requestAnimationFrame(frame);
}

async function ensureAudio() {
  if (audio) return;
  loadingAudio ??= (async () => {
    const ctx = new AudioContext();
    await ctx.resume();
    const loaded = await createWebAudioSink(ctx, async (url) => {
      const response = await fetch(url);
      if (!response.ok) throw new Error(`could not load ${url}`);
      return response.arrayBuffer();
    });
    audio = { ctx, sink: loaded };
  })();
  try {
    await loadingAudio;
  } catch (error) {
    loadingAudio = null;
    throw error;
  }
}

function setPlaying(next: boolean) {
  playing = next;
  transport.setAttribute("aria-pressed", String(next));
  transportLabel.textContent = next ? "Stop" : "Play";
  document.body.dataset.playing = String(next);
  if (next) {
    requestAnimationFrame(frame);
    return;
  }
  upcoming = [];
  shownBar = null;
  gridEditor.setStep(null);
  barView.setStep(null);
  setReadout(null, null, null);
}

transport.addEventListener("click", async () => {
  if (!session) return;
  if (playing) {
    session.stop();
    announce("Stopped.");
    return;
  }
  transport.disabled = true;
  transportLabel.textContent = "Loading";
  try {
    await ensureAudio();
    await audio!.ctx.resume();
    session.play();
    announce("Playing.");
  } catch (error) {
    console.error(error);
    transportLabel.textContent = "Play";
    announce("Audio could not start in this browser. The editor still works.");
  } finally {
    transport.disabled = false;
  }
});

required<HTMLButtonElement>("[data-update]").addEventListener("click", submitCode);

for (const button of document.querySelectorAll<HTMLButtonElement>("[data-new]")) {
  button.addEventListener("click", () => {
    const kind = button.dataset.new === "grid" ? "grid" : "code";
    select(library.create(kind, kind === "grid" ? "new grid" : "new pattern").id);
    nameInput.focus();
    nameInput.select();
  });
}

nameInput.addEventListener("change", () => {
  if (selected.readOnly) return;
  const name = nameInput.value.trim();
  if (name) library.rename(selected.id, name);
  selected = library.get(selected.id)!;
  renderEntry({ keepEditors: true });
});

required<HTMLButtonElement>("[data-duplicate]").addEventListener("click", () => {
  select(library.duplicate(selected.id).id);
});

deleteButton.addEventListener("click", () => {
  if (selected.readOnly || !window.confirm(`Delete ${selected.name}? Export it first if you want to keep it.`)) return;
  library.delete(selected.id);
  select(STARTER_ID);
});

convertButton.addEventListener("click", () => {
  if (selected.kind !== "grid") return;
  const created = library.create("code", `${selected.name} code`);
  library.save({ ...created, kind: "code", source: gridToFennel(selected.grid) });
  select(created.id);
});

async function copyLink(link: string, note: string) {
  try {
    await navigator.clipboard.writeText(link);
    shareFieldWrap.hidden = true;
    shareNotice.textContent = `Link copied.${note}`;
  } catch {
    shareField.value = link;
    shareFieldWrap.hidden = false;
    shareField.focus();
    shareField.select();
    shareNotice.textContent = `Copy this link.${note}`;
  }
}

shareButton.addEventListener("click", async () => {
  clearShareResult();
  if (selected.kind !== "grid") {
    shareNotice.textContent = CODE_SHARE_TEXT;
    return;
  }
  if (!sharingSupported()) {
    shareNotice.textContent = describeShareError(new ShareError("unsupported", "no stream support"));
    return;
  }
  const entryId = selected.id;
  const name = shareableName(selected.name);
  const note = name === selected.name.trim() ? "" : " The name was shortened or cleaned in the link.";
  try {
    const payload = await encodePayload({ name, kind: "grid", grid: selected.grid });
    if (selected.id !== entryId) return;
    await copyLink(buildLink(`${location.origin}/`, payload), note);
  } catch (error) {
    if (error instanceof ShareError) shareNotice.textContent = describeShareError(error);
    else {
      console.error(error);
      shareNotice.textContent = "The link could not be made.";
    }
  }
});

// Opens a link found in the address bar. Returns false when the address holds no link.
async function openFromAddress(): Promise<boolean> {
  const payload = readPayload(location.hash);
  if (payload === null || !session) return false;
  const token = ++openToken;
  let problem: string;
  if (!sharingSupported()) {
    problem = describeShareError(new ShareError("unsupported", "no stream support"));
  } else {
    try {
      const shared = await decodePayload(payload, SAMPLE_NAMES);
      if (token === openToken) select(library.openShared(shared).id);
      return true;
    } catch (error) {
      if (error instanceof ShareError) problem = describeShareError(error);
      else {
        console.error(error);
        problem = "This link could not be opened.";
      }
    }
  }
  if (token !== openToken) return true;
  select(STARTER_ID, { persist: false });
  showBanner("error", problem);
  return true;
}

sharedSave.addEventListener("click", () => {
  const copy = library.duplicate(SHARED_ID);
  library.closeShared();
  select(copy.id);
  transport.focus();
  announce(`Saved as ${copy.name}.`);
});

required<HTMLButtonElement>("[data-export]").addEventListener("click", () => {
  const { fileName, text } = library.exportFnl(selected.id);
  const url = URL.createObjectURL(new Blob([text], { type: "text/plain" }));
  const link = Object.assign(document.createElement("a"), { href: url, download: fileName });
  link.click();
  URL.revokeObjectURL(url);
});

required<HTMLInputElement>("[data-import]").addEventListener("change", async (event) => {
  const input = event.currentTarget as HTMLInputElement;
  const file = input.files?.[0];
  input.value = "";
  if (!file) return;
  select(library.importFnl(file.name, await file.text()).id);
});

const codeToggle = required<HTMLButtonElement>("[data-code-toggle]");
codeToggle.addEventListener("click", () => {
  const open = codeMode.dataset.codeOpen !== "true";
  codeMode.dataset.codeOpen = String(open);
  codeToggle.setAttribute("aria-expanded", String(open));
  codeToggle.textContent = open ? "Hide code" : "Show code";
  if (open) editor?.focus();
});

libraryToggle.addEventListener("click", () => {
  libraryToggle.setAttribute("aria-expanded", String(libraryToggle.getAttribute("aria-expanded") !== "true"));
});

if (recall(HINT_KEY)) hint.hidden = true;
required<HTMLButtonElement>("[data-hint-dismiss]").addEventListener("click", () => {
  hint.hidden = true;
  remember(HINT_KEY, "1");
});

if (/Mac|iPhone|iPad/.test(navigator.userAgent)) required<HTMLElement>("[data-mod-key]").textContent = "⌘";

renderEntry();
barView.show(null);
setReadout(null, null, null);
transport.disabled = true;
announce("Loading the Fennel runtime.");

import("./fennel-loader")
  .then(({ loadFennelRuntime }) => loadFennelRuntime())
  .then((runtime) => {
    session = new Session(
      {
        runtime,
        sink,
        clock: { now: () => audio?.ctx.currentTime ?? 0 },
        timer: {
          every: (ms, fn) => {
            const id = window.setInterval(fn, ms);
            return () => window.clearInterval(id);
          },
        },
        sampleNames: SAMPLE_NAMES,
        random: Math.random,
      },
      {
        onBar: (n, bar, start) => upcoming.push({ n, bar, start }),
        onDiagnostic: showDiagnostic,
        onState: setPlaying,
      },
    );
    transport.disabled = false;
    void openFromAddress().then((opened) => {
      if (!opened) select(selected.id);
    });
    window.addEventListener("hashchange", () => void openFromAddress());
  })
  .catch((error) => {
    console.error(error);
    announce("The Fennel runtime could not load. Reload the page to try again.");
    if (readPayload(location.hash) !== null) {
      showBanner("error", "This link could not be opened because the player did not finish loading. Reload to try again.");
    }
  });
