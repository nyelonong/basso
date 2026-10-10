import type { VoiceFactory } from "./audio";
import type { Instrument } from "./model";
import { noteFrequency } from "./notes";

// Voice parameters mirror internal/engine/sink.go so both players sound alike.
type Envelope = { attack: number; maxRelease: number; releaseFraction: number };
type Oscillator = { type: OscillatorType; cents: number; weight: number };
type Voice = { oscillators: Oscillator[]; envelope: Envelope; cutoff?: number };

const VOICES: Record<Exclude<Instrument, "pluck">, Voice> = {
  bass: {
    oscillators: [{ type: "sawtooth", cents: 0, weight: 1 }],
    envelope: { attack: 0.008, maxRelease: 0.03, releaseFraction: 0.2 },
  },
  brass: {
    oscillators: [{ type: "sawtooth", cents: 0, weight: 1 }],
    envelope: { attack: 0.04, maxRelease: 0.1, releaseFraction: 0.2 },
  },
  lead: {
    oscillators: [
      { type: "square", cents: 0, weight: 0.55 },
      { type: "sawtooth", cents: 0, weight: 0.35 },
    ],
    envelope: { attack: 0.005, maxRelease: 0.06, releaseFraction: 0.25 },
    cutoff: 6000,
  },
  pad: {
    oscillators: [
      { type: "sawtooth", cents: -7, weight: 0.25 },
      { type: "triangle", cents: 0, weight: 0.3 },
      { type: "sawtooth", cents: 7, weight: 0.25 },
    ],
    envelope: { attack: 0.12, maxRelease: 0.18, releaseFraction: 0.3 },
    cutoff: 1800,
  },
};

const PLUCK_ENVELOPE: Envelope = { attack: 0, maxRelease: 0.012, releaseFraction: 0.2 };
const PLUCK_DECAY = 0.996;

export function envelopeTimes(total: number, attack: number, maxRelease: number, releaseFraction: number) {
  const release = Math.max(0, Math.min(maxRelease, total * releaseFraction));
  return { attack: Math.max(0, Math.min(attack, total - release)), release };
}

function envelopeGain(
  ctx: BaseAudioContext,
  envelope: Envelope,
  time: number,
  duration: number,
  velocity: number,
): GainNode {
  const { attack, release } = envelopeTimes(duration, envelope.attack, envelope.maxRelease, envelope.releaseFraction);
  const gain = ctx.createGain();
  if (attack > 0) {
    gain.gain.setValueAtTime(0, time);
    gain.gain.linearRampToValueAtTime(velocity, time + attack);
  } else {
    gain.gain.setValueAtTime(velocity, time);
  }
  gain.gain.setValueAtTime(velocity, time + duration - release);
  gain.gain.linearRampToValueAtTime(0, time + duration);
  return gain;
}

// Port of Go's newOnePoleLowPass: y[n] = y[n-1] + alpha * (x[n] - y[n-1]).
function onePoleLowPass(ctx: BaseAudioContext, cutoff: number): AudioNode {
  const alpha = 1 - Math.exp((-2 * Math.PI * cutoff) / ctx.sampleRate);
  return ctx.createIIRFilter([alpha], [1, -(1 - alpha)]);
}

// Port of Go's karplusStrongStreamer, rendered ahead of time into a buffer.
function pluckBuffer(ctx: BaseAudioContext, freq: number, duration: number): AudioBuffer {
  const length = Math.max(1, Math.round(duration * ctx.sampleRate));
  const buffer = ctx.createBuffer(1, length, ctx.sampleRate);
  const out = buffer.getChannelData(0);
  const ring = Float64Array.from({ length: Math.max(2, Math.round(ctx.sampleRate / freq)) }, () => Math.random() * 2 - 1);
  let pos = 0;
  for (let i = 0; i < length; i++) {
    const value = ring[pos];
    const next = ring[(pos + 1) % ring.length];
    ring[pos] = PLUCK_DECAY * 0.5 * (value + next);
    out[i] = value;
    pos = (pos + 1) % ring.length;
  }
  return buffer;
}

export const synthVoices: VoiceFactory = {
  playNote(ctx, destination, note, instrument, time, duration, velocity, pan) {
    const freq = noteFrequency(note);
    const panner = ctx.createStereoPanner();
    panner.pan.value = pan;
    panner.connect(destination);

    if (instrument === "pluck") {
      const source = ctx.createBufferSource();
      source.buffer = pluckBuffer(ctx, freq, duration);
      source.connect(envelopeGain(ctx, PLUCK_ENVELOPE, time, duration, velocity)).connect(panner);
      source.start(time);
      source.stop(time + duration);
      return [source];
    }

    const voice = VOICES[instrument];
    const envelope = envelopeGain(ctx, voice.envelope, time, duration, velocity);
    envelope.connect(panner);
    const mixInput = voice.cutoff === undefined ? envelope : onePoleLowPass(ctx, voice.cutoff);
    if (mixInput !== envelope) mixInput.connect(envelope);

    return voice.oscillators.map(({ type, cents, weight }) => {
      const osc = ctx.createOscillator();
      osc.type = type;
      osc.frequency.setValueAtTime(freq * 2 ** (cents / 1200), time);
      const mix = ctx.createGain();
      mix.gain.value = weight;
      osc.connect(mix).connect(mixInput);
      osc.start(time);
      osc.stop(time + duration);
      return osc;
    });
  },
};
