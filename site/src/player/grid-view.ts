import type { Bar, GridPattern } from "./model";
import "./grid-view.css";

const VELOCITIES = [1, 0.7, 0.4];
const STEP_CHOICES = [8, 12, 16, 32];
const LONG_PRESS_MS = 450;
// Below this velocity the accent fill is too dark for ink-colored step numbers.
const SOFT_BELOW = 0.85;

const label = (sample: string) => sample.replace(/\.wav$/, "");

function element<K extends keyof HTMLElementTagNameMap>(
  tag: K,
  props: Partial<HTMLElementTagNameMap[K]> = {},
  ...children: (Node | string)[]
): HTMLElementTagNameMap[K] {
  const node = Object.assign(document.createElement(tag), props);
  node.append(...children);
  return node;
}

function nextVelocity(velocity: number): number {
  const index = VELOCITIES.findIndex((v) => Math.abs(v - velocity) < 0.01);
  return VELOCITIES[(index + 1) % VELOCITIES.length];
}

function stepName(step: number, what: string) {
  const visible = String(step + 1).padStart(2, "0");
  return { visible, accessible: `${visible}, ${what} step ${step + 1}` };
}

function markStep(root: HTMLElement, step: number | null) {
  for (const cell of root.querySelectorAll<HTMLElement>("[data-step]")) {
    cell.dataset.current = String(Number(cell.dataset.step) === step);
  }
}

export function mountGridEditor(
  el: HTMLElement,
  initial: GridPattern,
  onChange: (grid: GridPattern) => void,
  samples: readonly string[],
): { setStep(step: number | null): void; setGrid(grid: GridPattern): void } {
  let grid = structuredClone(initial);
  let current: number | null = null;

  const change = (next: GridPattern) => {
    grid = next;
    render();
    onChange(structuredClone(grid));
  };

  const updateCell = (row: number, step: number, update: (cell: { on: boolean; velocity: number }) => void) => {
    const next = structuredClone(grid);
    update(next.rows[row].cells[step]);
    change(next);
    el.querySelector<HTMLButtonElement>(`[data-row="${row}"][data-step="${step}"]`)?.focus();
  };

  function cellButton(row: number, step: number) {
    const cell = grid.rows[row].cells[step];
    const name = stepName(step, label(grid.rows[row].sample));
    const button = element("button", {
      type: "button",
      className: "grid-cell",
      textContent: name.visible,
      title: "Click to toggle. Shift-click or long-press to change velocity.",
    });
    button.setAttribute("aria-label", `${name.accessible}, velocity ${Math.round(cell.velocity * 100)}%`);
    button.setAttribute("aria-pressed", String(cell.on));
    button.dataset.row = String(row);
    button.dataset.step = String(step);
    button.style.setProperty("--velocity", String(cell.velocity));
    button.dataset.soft = String(cell.velocity < SOFT_BELOW);

    let pressTimer: number | undefined;
    let longPressed = false;
    const cycle = () =>
      updateCell(row, step, (c) => {
        c.on = true;
        c.velocity = nextVelocity(c.velocity);
      });
    button.addEventListener("pointerdown", () => {
      longPressed = false;
      pressTimer = window.setTimeout(() => {
        longPressed = true;
        cycle();
      }, LONG_PRESS_MS);
    });
    const cancelPress = () => window.clearTimeout(pressTimer);
    button.addEventListener("pointerup", cancelPress);
    button.addEventListener("pointerleave", cancelPress);
    button.addEventListener("contextmenu", (event) => {
      event.preventDefault();
      if (!longPressed) cycle();
    });
    button.addEventListener("click", (event) => {
      if (longPressed) return;
      if (event.shiftKey) cycle();
      else updateCell(row, step, (c) => (c.on = !c.on));
    });
    return button;
  }

  function controls() {
    const bpm = element("input", { type: "number", min: "20", max: "400", value: String(grid.bpm), className: "grid-bpm" });
    bpm.setAttribute("aria-label", "Tempo in BPM");
    bpm.addEventListener("change", () => {
      const value = Math.round(Number(bpm.value));
      if (value >= 20 && value <= 400) change({ ...structuredClone(grid), bpm: value });
      else bpm.value = String(grid.bpm);
    });

    const steps = element("select", { className: "grid-steps" });
    steps.setAttribute("aria-label", "Steps per bar");
    for (const n of STEP_CHOICES) steps.append(element("option", { value: String(n), textContent: `${n} steps`, selected: n === grid.steps }));
    steps.addEventListener("change", () => {
      const n = Number(steps.value);
      const next = structuredClone(grid);
      next.steps = n;
      for (const row of next.rows) {
        row.cells = Array.from({ length: n }, (_, i) => row.cells[i] ?? { on: false, velocity: 1 });
      }
      change(next);
    });

    const unused = samples.filter((s) => !grid.rows.some((r) => r.sample === s));
    const picker = element("select", { className: "grid-add-sample", disabled: unused.length === 0 });
    picker.setAttribute("aria-label", "Sample to add");
    for (const s of unused) picker.append(element("option", { value: s, textContent: label(s) }));
    const add = element("button", { type: "button", className: "grid-add", textContent: "Add row", disabled: unused.length === 0 });
    add.addEventListener("click", () => {
      const next = structuredClone(grid);
      next.rows.push({ sample: picker.value, cells: Array.from({ length: grid.steps }, () => ({ on: false, velocity: 1 })) });
      change(next);
    });

    return element(
      "div",
      { className: "grid-controls" },
      element("label", { className: "grid-field" }, "BPM ", bpm),
      steps,
      element("span", { className: "grid-add-group" }, picker, add),
    );
  }

  function render() {
    const rows = grid.rows.map((row, r) => {
      const remove = element("button", { type: "button", className: "grid-remove", textContent: "×" });
      remove.setAttribute("aria-label", `Remove ${label(row.sample)} row`);
      remove.addEventListener("click", () => {
        const next = structuredClone(grid);
        next.rows.splice(r, 1);
        change(next);
      });
      const cells = element("div", { className: "grid-cells" }, ...row.cells.map((_, s) => cellButton(r, s)));
      cells.style.setProperty("--steps", String(grid.steps));
      return element(
        "div",
        { className: "grid-row" },
        element("div", { className: "grid-row-label" }, element("strong", { textContent: label(row.sample) }), remove),
        cells,
      );
    });
    const body = element("div", { className: "grid-rows" }, ...rows);
    body.setAttribute("role", "group");
    body.setAttribute("aria-label", "Step grid");
    el.replaceChildren(controls(), body);
    markStep(el, current);
  }

  render();
  return {
    setStep(step) {
      current = step;
      markStep(el, step);
    },
    setGrid(next) {
      grid = structuredClone(next);
      render();
    },
  };
}

export function mountBarView(el: HTMLElement): { show(bar: Bar | null): void; setStep(step: number | null): void } {
  let current: number | null = null;
  return {
    show(bar) {
      if (!bar || bar.hits.length === 0) {
        el.replaceChildren(element("p", { className: "bar-view-empty", textContent: bar ? "This bar has no hits." : "Nothing playing yet." }));
        return;
      }
      const rows = new Map<string, Map<number, number>>();
      for (const hit of bar.hits) {
        const key = hit.kind === "sample" ? label(hit.sample) : `${hit.instrument} ${hit.note}`;
        const steps = rows.get(key) ?? new Map<number, number>();
        steps.set(hit.step, Math.max(steps.get(hit.step) ?? 0, hit.velocity));
        rows.set(key, steps);
      }
      el.replaceChildren(
        ...[...rows].map(([key, steps]) => {
          const cells = element(
            "div",
            { className: "grid-cells" },
            ...Array.from({ length: bar.steps }, (_, s) => {
              const cell = element("span", { className: "grid-cell", textContent: stepName(s, key).visible });
              cell.dataset.step = String(s);
              cell.dataset.on = String(steps.has(s));
              cell.style.setProperty("--velocity", String(steps.get(s) ?? 1));
              cell.dataset.soft = String((steps.get(s) ?? 1) < SOFT_BELOW);
              return cell;
            }),
          );
          cells.style.setProperty("--steps", String(bar.steps));
          return element("div", { className: "grid-row" }, element("div", { className: "grid-row-label" }, element("strong", { textContent: key })), cells);
        }),
      );
      el.setAttribute("aria-label", `Bar view: ${bar.hits.length} hits over ${bar.steps} steps`);
      markStep(el, current);
    },
    setStep(step) {
      current = step;
      markStep(el, step);
    },
  };
}
