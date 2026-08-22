import { describe, expect, it } from "vitest";
import {
  advanceStep,
  createSequencerState,
  startSequencer,
  stopSequencer,
  toggleStep,
} from "../src/sequencer";

describe("sequencer state", () => {
  it("applies edits immediately while stopped", () => {
    const initial = createSequencerState();
    const changed = toggleStep(initial, "kick", 1);

    expect(changed.active.kick).toContain(1);
    expect(changed.pending).toBeNull();
  });

  it("queues edits without changing the active bar while playing", () => {
    const playing = startSequencer(createSequencerState());
    const changed = toggleStep(playing, "kick", 1);

    expect(changed.active.kick).not.toContain(1);
    expect(changed.pending?.kick).toContain(1);
  });

  it("activates queued edits only when the next bar begins", () => {
    let state = toggleStep(startSequencer(createSequencerState()), "kick", 1);

    for (let step = 1; step < 16; step += 1) {
      state = advanceStep(state);
      expect(state.active.kick).not.toContain(1);
    }

    state = advanceStep(state);

    expect(state.step).toBe(0);
    expect(state.bar).toBe(1);
    expect(state.active.kick).toContain(1);
    expect(state.pending).toBeNull();
  });

  it("keeps an armed edit when playback stops before the boundary", () => {
    const changed = toggleStep(startSequencer(createSequencerState()), "kick", 1);
    const stopped = stopSequencer(changed);

    expect(stopped.playing).toBe(false);
    expect(stopped.step).toBe(0);
    expect(stopped.bar).toBe(0);
    expect(stopped.active.kick).toContain(1);
    expect(stopped.pending).toBeNull();
  });
});
