import { SAMPLE_NAMES, sampleUrl } from "./assets";
import type { AudioSink, Instrument } from "./model";
import { synthVoices } from "./synth";

export type VoiceFactory = {
  playNote(
    ctx: BaseAudioContext,
    destination: AudioNode,
    note: string,
    instrument: Instrument,
    time: number,
    duration: number,
    velocity: number,
    pan: number,
  ): AudioScheduledSourceNode[];
};

// Headroom for dense patterns: many full-velocity voices summed straight into
// the output clipped about 13% of funkot's samples before this bus existed.
export const MASTER_GAIN = 0.5;
const SOFT_CLIP_KNEE = 0.8;
const SOFT_CLIP_CURVE_POINTS = 2049;

// Leaves signal below the knee untouched and bends louder signal toward
// SOFT_CLIP_KNEE + (1 - SOFT_CLIP_KNEE) * tanh(1), so the output never reaches full scale.
export function softClip(x: number): number {
  const level = Math.abs(x);
  if (level <= SOFT_CLIP_KNEE) return x;
  const room = 1 - SOFT_CLIP_KNEE;
  return Math.sign(x) * (SOFT_CLIP_KNEE + room * Math.tanh((Math.min(level, 1) - SOFT_CLIP_KNEE) / room));
}

// Gain for headroom, a fast limiter for peaks, then a soft clipper for anything
// the limiter's attack lets through.
export function createMasterBus(ctx: BaseAudioContext): AudioNode {
  const gain = ctx.createGain();
  gain.gain.value = MASTER_GAIN;
  const limiter = ctx.createDynamicsCompressor();
  limiter.threshold.value = -6;
  limiter.knee.value = 0;
  limiter.ratio.value = 20;
  limiter.attack.value = 0.001;
  limiter.release.value = 0.1;
  const clipper = ctx.createWaveShaper();
  clipper.curve = Float32Array.from({ length: SOFT_CLIP_CURVE_POINTS }, (_, i) =>
    softClip((i / (SOFT_CLIP_CURVE_POINTS - 1)) * 2 - 1),
  );
  clipper.oversample = "4x";
  gain.connect(limiter).connect(clipper).connect(ctx.destination);
  return gain;
}

export interface WebAudioSink extends AudioSink {
  voices: VoiceFactory | null;
}

export async function createWebAudioSink(
  ctx: BaseAudioContext,
  fetchBytes: (url: string) => Promise<ArrayBuffer>,
  voices: VoiceFactory | null = synthVoices,
): Promise<WebAudioSink> {
  const buffers = new Map<string, AudioBuffer>();
  await Promise.all(
    SAMPLE_NAMES.map(async (name) => {
      buffers.set(name, await ctx.decodeAudioData(await fetchBytes(sampleUrl(name))));
    }),
  );

  const master = createMasterBus(ctx);
  const active = new Set<AudioScheduledSourceNode>();
  const track = (source: AudioScheduledSourceNode) => {
    active.add(source);
    source.onended = () => active.delete(source);
  };

  const sink: WebAudioSink = {
    voices,
    playSample(name, time, velocity, pan) {
      const buffer = buffers.get(name);
      if (!buffer) return;
      const source = ctx.createBufferSource();
      source.buffer = buffer;
      const gain = ctx.createGain();
      gain.gain.value = velocity;
      const panner = ctx.createStereoPanner();
      panner.pan.value = pan;
      source.connect(gain).connect(panner).connect(master);
      track(source);
      source.start(time);
    },
    playNote(note, instrument, time, duration, velocity, pan) {
      if (!this.voices) return;
      for (const source of this.voices.playNote(ctx, master, note, instrument, time, duration, velocity, pan)) {
        track(source);
      }
    },
    stopAll() {
      for (const source of active) source.stop();
      active.clear();
    },
  };
  return sink;
}
