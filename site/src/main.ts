import "@fontsource-variable/archivo";
import "@fontsource/fragment-mono";
import "./styles.css";
import {
  advanceStep,
  createSequencerState,
  startSequencer,
  stepLabel,
  stopSequencer,
  toggleStep,
  trackIds,
  type TrackId,
} from "./sequencer";

const bpm = 150;
const stepDuration = (60 / bpm / 4) * 1_000;
const installCommand = "go install github.com/nyelonong/basso/cmd/basso@latest";
const samplePaths: Record<TrackId, string> = {
  kick: "/audio/kick2.wav",
  snare: "/audio/snare.wav",
  hat: "/audio/cl_hihat.wav",
  clap: "/audio/handclap.wav",
};
const trackGain: Record<TrackId, number> = {
  kick: 0.85,
  snare: 0.65,
  hat: 0.32,
  clap: 0.55,
};

function required<T extends Element>(selector: string, root: ParentNode = document): T {
  const element = root.querySelector<T>(selector);
  if (!element) {
    throw new Error(`missing required element: ${selector}`);
  }
  return element;
}

const transport = required<HTMLButtonElement>("#transport");
const transportLabel = required<HTMLElement>("[data-transport-label]", transport);
const status = required<HTMLElement>("[data-status]");
const barReadout = required<HTMLElement>("[data-bar]");
const stepReadout = required<HTMLElement>("[data-step]");
const copyButton = required<HTMLButtonElement>("[data-copy-install]");
const copyStatus = required<HTMLElement>("[data-copy-status]");
const stepButtons = new Map<string, HTMLButtonElement>();

let state = createSequencerState();
let audioContext: AudioContext | null = null;
let audioLoad: Promise<void> | null = null;
let audioBuffers: Partial<Record<TrackId, AudioBuffer>> = {};
let timer: number | null = null;
let nextTickAt = 0;
let firstTick = true;
let loadingAudio = false;
let audioFailure = "";

for (const track of trackIds) {
  const row = required<HTMLElement>(`[data-track="${track}"]`);
  const grid = required<HTMLElement>("[data-steps]", row);

  for (let step = 0; step < 16; step += 1) {
    const button = document.createElement("button");
    const label = stepLabel(track, step);
    button.className = "step";
    button.type = "button";
    button.textContent = label.visible;
    button.setAttribute("aria-label", label.accessible);
    button.addEventListener("click", () => {
      state = toggleStep(state, track, step);
      render();
    });
    grid.append(button);
    stepButtons.set(`${track}:${step}`, button);
  }
}

function displayedPattern() {
  return state.pending ?? state.active;
}

function render() {
  const visible = displayedPattern();

  for (const track of trackIds) {
    for (let step = 0; step < 16; step += 1) {
      const button = stepButtons.get(`${track}:${step}`);
      if (!button) {
        continue;
      }

      const selected = visible[track].includes(step);
      const active = state.active[track].includes(step);
      button.setAttribute("aria-pressed", String(selected));
      button.dataset.queued = String(selected !== active);
      button.dataset.current = String(state.playing && state.step === step);
    }
  }

  transport.disabled = loadingAudio;
  transport.dataset.playing = String(state.playing);
  transportLabel.textContent = loadingAudio
    ? "Loading 808s"
    : state.playing
      ? "Stop"
      : "Play bar";
  barReadout.textContent = String(state.bar + 1).padStart(2, "0");
  stepReadout.textContent = String(state.step + 1).padStart(2, "0");

  if (audioFailure) {
    status.textContent = audioFailure;
  } else if (loadingAudio) {
    status.textContent = "LOADING / BUNDLED 808 SAMPLES";
  } else if (!state.playing) {
    status.textContent = "READY / EDITS APPLY NOW";
  } else if (state.pending) {
    status.textContent = "EDIT ARMED / NEXT BAR";
  } else {
    status.textContent = `PLAYING / BAR ${String(state.bar + 1).padStart(2, "0")}`;
  }
}

async function loadAudio() {
  if (audioLoad) {
    return audioLoad;
  }

  audioContext = new AudioContext();
  await audioContext.resume();
  audioLoad = Promise.all(
    trackIds.map(async (track) => {
      const response = await fetch(samplePaths[track]);
      if (!response.ok) {
        throw new Error(`could not load ${samplePaths[track]}`);
      }
      const encoded = await response.arrayBuffer();
      audioBuffers[track] = await audioContext!.decodeAudioData(encoded);
    }),
  ).then(() => undefined);

  return audioLoad;
}

function playStep() {
  if (!audioContext) {
    return;
  }

  for (const track of trackIds) {
    if (!state.active[track].includes(state.step)) {
      continue;
    }

    const buffer = audioBuffers[track];
    if (!buffer) {
      continue;
    }

    const source = audioContext.createBufferSource();
    const gain = audioContext.createGain();
    source.buffer = buffer;
    gain.gain.value = trackGain[track];
    source.connect(gain).connect(audioContext.destination);
    source.start();
  }
}

function tick() {
  if (!state.playing) {
    return;
  }

  if (firstTick) {
    firstTick = false;
  } else {
    state = advanceStep(state);
  }

  render();
  playStep();
  nextTickAt += stepDuration;
  timer = window.setTimeout(tick, Math.max(0, nextTickAt - performance.now()));
}

function stopPlayback() {
  if (timer !== null) {
    window.clearTimeout(timer);
    timer = null;
  }
  state = stopSequencer(state);
  render();
}

transport.addEventListener("click", async () => {
  if (state.playing) {
    stopPlayback();
    return;
  }

  loadingAudio = true;
  audioFailure = "";
  render();

  try {
    await loadAudio();
  } catch (error) {
    audioLoad = null;
    audioBuffers = {};
    audioFailure = "AUDIO UNAVAILABLE / GRID STILL EDITABLE";
    console.error(error);
    loadingAudio = false;
    render();
    return;
  }

  loadingAudio = false;
  state = startSequencer(state);
  firstTick = true;
  nextTickAt = performance.now();
  tick();
});

document.addEventListener("visibilitychange", () => {
  if (document.hidden && state.playing) {
    stopPlayback();
  }
});

async function copyText(value: string) {
  if (navigator.clipboard) {
    await navigator.clipboard.writeText(value);
    return;
  }

  const field = document.createElement("textarea");
  field.value = value;
  field.style.position = "fixed";
  field.style.opacity = "0";
  document.body.append(field);
  field.select();
  document.execCommand("copy");
  field.remove();
}

copyButton.addEventListener("click", async () => {
  try {
    await copyText(installCommand);
    copyButton.textContent = "Copied";
    copyStatus.textContent = "Install command copied. Then save the file on any step.";
    window.setTimeout(() => {
      copyButton.textContent = "Copy";
      copyStatus.textContent = "Then save the file on any step.";
    }, 2_000);
  } catch (error) {
    copyStatus.textContent = "Copy failed. Select the command manually.";
    console.error(error);
  }
});

render();
