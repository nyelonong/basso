import { EXAMPLE_PATTERNS } from "./assets";
import { emptyGrid, gridToFennel } from "./grid";
import type { GridPattern } from "./model";

export type Entry = { id: string; name: string; readOnly: boolean; shared?: true } & (
  | { kind: "code"; source: string }
  | { kind: "grid"; grid: GridPattern }
);

export const STARTER_ID = "example:starter-grid";
export const SHARED_ID = "shared";

const STORAGE_KEY = "basso.library.v1";

const NEW_CODE = `(bpm 120)
(steps 16)

(fn pattern [bar]
  [{:step 0 :sample "kick2.wav"}
   {:step 4 :sample "snare.wav"}
   {:step 8 :sample "kick2.wav"}
   {:step 12 :sample "snare.wav"}])

pattern
`;

function starterGrid(): GridPattern {
  const rows: [string, number, number[]][] = [
    ["kick2.wav", 0.85, [0, 3, 6, 8, 11, 14]],
    ["snare.wav", 0.65, [4, 12]],
    ["cl_hihat.wav", 0.32, Array.from({ length: 16 }, (_, i) => i)],
    ["handclap.wav", 0.55, [4, 12]],
  ];
  const grid = emptyGrid(rows.map(([sample]) => sample), 16, 150);
  rows.forEach(([, velocity, steps], row) => {
    for (const step of steps) grid.rows[row].cells[step] = { on: true, velocity };
  });
  return grid;
}

function examples(): Entry[] {
  return [
    { id: STARTER_ID, name: "starter-grid", readOnly: true, kind: "grid", grid: starterGrid() },
    ...EXAMPLE_PATTERNS.map(
      (p): Entry => ({ id: `example:${p.name}`, name: p.name, readOnly: true, kind: "code", source: p.source }),
    ),
  ];
}

const isEntry = (value: unknown): value is Entry => {
  const e = value as Entry;
  return (
    typeof e?.id === "string" &&
    typeof e.name === "string" &&
    ((e.kind === "code" && typeof e.source === "string") || (e.kind === "grid" && Array.isArray(e.grid?.rows)))
  );
};

const clone = <T>(value: T): T => structuredClone(value);

export class Library {
  private readonly builtIn = examples();
  private user: Entry[] = [];
  private shared: Entry | null = null;
  private storageWorks: boolean;

  constructor(
    private readonly storage: Storage | null,
    private readonly newId: () => string = () => crypto.randomUUID(),
  ) {
    this.storageWorks = storage !== null;
    if (!storage) return;
    try {
      const parsed: unknown = JSON.parse(storage.getItem(STORAGE_KEY) ?? "[]");
      if (Array.isArray(parsed)) {
        this.user = parsed.filter(isEntry).filter((e) => e.id !== SHARED_ID).map((e) => {
          const entry: Entry = { ...e, readOnly: false };
          delete entry.shared;
          return entry;
        });
      }
    } catch (error) {
      if (!(error instanceof SyntaxError)) this.storageWorks = false;
    }
  }

  get persistent(): boolean {
    return this.storageWorks;
  }

  list(): Entry[] {
    return [...(this.shared ? [this.shared] : []), ...this.builtIn, ...this.user].map(clone);
  }

  // The shared entry lives only in memory: a link never changes what is saved.
  openShared(pattern: { name: string } & ({ kind: "code"; source: string } | { kind: "grid"; grid: GridPattern })): Entry {
    this.shared = { ...clone(pattern), id: SHARED_ID, readOnly: true, shared: true };
    return clone(this.shared);
  }

  closeShared(): void {
    this.shared = null;
  }

  get(id: string): Entry | undefined {
    const entry = this.find(id);
    return entry && clone(entry);
  }

  create(kind: Entry["kind"], name: string): Entry {
    const base = { id: this.newId(), name: this.uniqueName(name), readOnly: false };
    const entry: Entry =
      kind === "code"
        ? { ...base, kind, source: NEW_CODE }
        : { ...base, kind, grid: emptyGrid(["kick2.wav", "snare.wav", "cl_hihat.wav", "handclap.wav"]) };
    return this.add(entry);
  }

  duplicate(id: string): Entry {
    const source = this.require(id);
    const copy: Entry = { ...clone(source), id: this.newId(), name: this.uniqueName(`${source.name} copy`), readOnly: false };
    delete copy.shared;
    return this.add(copy);
  }

  rename(id: string, name: string): void {
    const entry = this.editable(id);
    if (entry.name !== name) entry.name = this.uniqueName(name);
    this.persist();
  }

  save(entry: Entry): void {
    const current = this.editable(entry.id);
    const saved: Entry = { ...clone(entry), name: current.name, readOnly: false };
    delete saved.shared;
    this.user[this.user.indexOf(current)] = saved;
    this.persist();
  }

  delete(id: string): void {
    const entry = this.editable(id);
    this.user = this.user.filter((e) => e !== entry);
    this.persist();
  }

  importFnl(fileName: string, text: string): Entry {
    const name = fileName.replace(/\.fnl$/i, "") || "imported";
    return this.add({ id: this.newId(), name: this.uniqueName(name), readOnly: false, kind: "code", source: text });
  }

  exportFnl(id: string): { fileName: string; text: string } {
    const entry = this.require(id);
    return {
      fileName: `${entry.name}.fnl`,
      text: entry.kind === "code" ? entry.source : gridToFennel(entry.grid),
    };
  }

  private find(id: string): Entry | undefined {
    return this.shared?.id === id ? this.shared : (this.builtIn.find((e) => e.id === id) ?? this.user.find((e) => e.id === id));
  }

  private require(id: string): Entry {
    const entry = this.find(id);
    if (!entry) throw new Error(`no pattern with id ${id}`);
    return entry;
  }

  private editable(id: string): Entry {
    const entry = this.require(id);
    if (entry.readOnly) throw new Error(`${entry.name} is read-only; duplicate it first`);
    return entry;
  }

  private uniqueName(name: string): string {
    const taken = new Set([...this.builtIn, ...this.user].map((e) => e.name));
    if (!taken.has(name)) return name;
    let n = 2;
    while (taken.has(`${name} ${n}`)) n++;
    return `${name} ${n}`;
  }

  private add(entry: Entry): Entry {
    this.user.push(entry);
    this.persist();
    return clone(entry);
  }

  private persist(): void {
    if (!this.storage || !this.storageWorks) return;
    try {
      this.storage.setItem(STORAGE_KEY, JSON.stringify(this.user));
    } catch {
      this.storageWorks = false;
    }
  }
}
