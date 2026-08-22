export const trackIds = ["kick", "snare", "hat", "clap"] as const;

export type TrackId = (typeof trackIds)[number];
export type Pattern = Record<TrackId, readonly number[]>;

export interface SequencerState {
  active: Pattern;
  pending: Pattern | null;
  playing: boolean;
  step: number;
  bar: number;
}

export function stepLabel(track: TrackId, step: number) {
  const visible = String(step + 1).padStart(2, "0");
  return { visible, accessible: `${visible}, ${track} step ${step + 1}` };
}

const initialPattern: Pattern = {
  kick: [0, 3, 6, 8, 11, 14],
  snare: [4, 12],
  hat: [0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15],
  clap: [4, 12],
};

function clonePattern(pattern: Pattern): Pattern {
  return {
    kick: [...pattern.kick],
    snare: [...pattern.snare],
    hat: [...pattern.hat],
    clap: [...pattern.clap],
  };
}

export function createSequencerState(): SequencerState {
  return {
    active: clonePattern(initialPattern),
    pending: null,
    playing: false,
    step: 0,
    bar: 0,
  };
}

export function startSequencer(state: SequencerState): SequencerState {
  return { ...state, playing: true, step: 0, bar: 0 };
}

export function stopSequencer(state: SequencerState): SequencerState {
  return {
    ...state,
    active: state.pending ?? state.active,
    pending: null,
    playing: false,
    step: 0,
    bar: 0,
  };
}

export function toggleStep(
  state: SequencerState,
  track: TrackId,
  step: number,
): SequencerState {
  if (!Number.isInteger(step) || step < 0 || step > 15) {
    throw new RangeError("step must be an integer from 0 through 15");
  }

  const source = state.playing ? state.pending ?? state.active : state.active;
  const values = new Set(source[track]);

  if (values.has(step)) {
    values.delete(step);
  } else {
    values.add(step);
  }

  const changed: Pattern = {
    ...clonePattern(source),
    [track]: [...values].sort((left, right) => left - right),
  };

  return state.playing
    ? { ...state, pending: changed }
    : { ...state, active: changed };
}

export function advanceStep(state: SequencerState): SequencerState {
  if (!state.playing) {
    return state;
  }

  const step = (state.step + 1) % 16;
  const crossedBar = step === 0;

  return {
    ...state,
    active: crossedBar && state.pending ? state.pending : state.active,
    pending: crossedBar ? null : state.pending,
    step,
    bar: crossedBar ? state.bar + 1 : state.bar,
  };
}
