import { describe, expect, it } from "vitest";
import { EXAMPLE_PATTERNS } from "../../src/player/assets";
import { Library, STARTER_ID } from "../../src/player/library";

class MemoryStorage {
  data = new Map<string, string>();
  get length() {
    return this.data.size;
  }
  clear() {
    this.data.clear();
  }
  getItem(key: string) {
    return this.data.get(key) ?? null;
  }
  key(i: number) {
    return [...this.data.keys()][i] ?? null;
  }
  removeItem(key: string) {
    this.data.delete(key);
  }
  setItem(key: string, value: string) {
    this.data.set(key, value);
  }
}

const throwing = new Proxy(new MemoryStorage(), {
  get(target, prop) {
    if (prop === "getItem" || prop === "setItem") {
      return () => {
        throw new Error("SecurityError");
      };
    }
    return Reflect.get(target, prop);
  },
}) as unknown as Storage;

let counter = 0;
const ids = () => `id-${++counter}`;
const library = (storage: Storage | null = new MemoryStorage() as unknown as Storage) => new Library(storage, ids);

describe("Library", () => {
  it("lists the starter grid first, then every example pattern, all read-only", () => {
    const entries = library().list();
    expect(entries[0]).toMatchObject({ id: STARTER_ID, name: "starter-grid", kind: "grid", readOnly: true });
    expect(entries.slice(1).map((e) => e.name)).toEqual(EXAMPLE_PATTERNS.map((p) => p.name));
    expect(entries.every((e) => e.readOnly)).toBe(true);
  });

  it("starts the starter grid as the landing page's bounce at 150 BPM", () => {
    const starter = library().get(STARTER_ID);
    if (starter?.kind !== "grid") throw new Error("starter must be a grid");
    expect(starter.grid.bpm).toBe(150);
    const kick = starter.grid.rows.find((r) => r.sample === "kick2.wav")!;
    expect(kick.cells.flatMap((c, i) => (c.on ? [i] : []))).toEqual([0, 3, 6, 8, 11, 14]);
  });

  it("creates, renames, duplicates and deletes entries that survive a reload", () => {
    const storage = new MemoryStorage() as unknown as Storage;
    const lib = new Library(storage, ids);
    const code = lib.create("code", "my beat");
    const grid = lib.create("grid", "my grid");
    lib.rename(code.id, "renamed");
    const copy = lib.duplicate(grid.id);
    lib.delete(grid.id);

    const reloaded = new Library(storage, ids).list().filter((e) => !e.readOnly);
    expect(reloaded.map((e) => [e.name, e.kind])).toEqual([
      ["renamed", "code"],
      ["my grid copy", "grid"],
    ]);
    expect(reloaded[1].id).toBe(copy.id);
  });

  it("gives a new code entry a pattern that plays", () => {
    const entry = library().create("code", "fresh");
    expect(entry.kind === "code" && entry.source).toContain("(fn pattern [bar]");
  });

  it("keeps names unique", () => {
    const lib = library();
    lib.create("code", "beat");
    expect(lib.create("code", "beat").name).toBe("beat 2");
    expect(lib.create("code", "basic-groove").name).toBe("basic-groove 2");
  });

  it("saves edits to a user entry", () => {
    const storage = new MemoryStorage() as unknown as Storage;
    const lib = new Library(storage, ids);
    const entry = lib.create("code", "beat");
    lib.save({ ...entry, kind: "code", source: "(fn pattern [bar] [])" });
    expect(new Library(storage, ids).get(entry.id)).toMatchObject({ source: "(fn pattern [bar] [])" });
  });

  it("refuses to change read-only examples", () => {
    const lib = library();
    const example = lib.list()[1];
    expect(() => lib.save(example)).toThrow(/read-only/);
    expect(() => lib.rename(example.id, "x")).toThrow(/read-only/);
    expect(() => lib.delete(example.id)).toThrow(/read-only/);
    expect(lib.duplicate(example.id)).toMatchObject({ readOnly: false, name: `${example.name} copy` });
  });

  it("keeps working in memory when storage is unavailable", () => {
    for (const storage of [null, throwing]) {
      const lib = library(storage);
      expect(lib.persistent).toBe(false);
      const entry = lib.create("code", "beat");
      expect(lib.get(entry.id)).toBeDefined();
    }
    expect(library().persistent).toBe(true);
  });

  it("ignores corrupt stored data", () => {
    const storage = new MemoryStorage();
    storage.setItem("basso.library.v1", "{not json");
    const lib = new Library(storage as unknown as Storage, ids);
    expect(lib.list().filter((e) => !e.readOnly)).toEqual([]);
  });

  it("imports a .fnl file as a code entry", () => {
    const entry = library().importFnl("funkot.fnl", "(fn pattern [bar] [])");
    expect(entry).toMatchObject({ name: "funkot", kind: "code", source: "(fn pattern [bar] [])", readOnly: false });
  });

  it("exports code unchanged and grids as Fennel", () => {
    const lib = library();
    const code = lib.importFnl("a.fnl", "(fn pattern [bar] [])");
    expect(lib.exportFnl(code.id)).toEqual({ fileName: "a.fnl", text: "(fn pattern [bar] [])" });
    const starter = lib.exportFnl(STARTER_ID);
    expect(starter.fileName).toBe("starter-grid.fnl");
    expect(starter.text).toContain("(bpm 150)");
  });
});
