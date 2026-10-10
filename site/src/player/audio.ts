import { SAMPLE_NAMES, sampleUrl } from "./assets";
import type { AudioSink, Instrument } from "./model";

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

export interface WebAudioSink extends AudioSink {
  voices: VoiceFactory | null;
}

export async function createWebAudioSink(
  ctx: BaseAudioContext,
  fetchBytes: (url: string) => Promise<ArrayBuffer>,
): Promise<WebAudioSink> {
  const buffers = new Map<string, AudioBuffer>();
  await Promise.all(
    SAMPLE_NAMES.map(async (name) => {
      buffers.set(name, await ctx.decodeAudioData(await fetchBytes(sampleUrl(name))));
    }),
  );

  const active = new Set<AudioScheduledSourceNode>();
  const track = (source: AudioScheduledSourceNode) => {
    active.add(source);
    source.onended = () => active.delete(source);
  };

  const sink: WebAudioSink = {
    voices: null,
    playSample(name, time, velocity, pan) {
      const buffer = buffers.get(name);
      if (!buffer) return;
      const source = ctx.createBufferSource();
      source.buffer = buffer;
      const gain = ctx.createGain();
      gain.gain.value = velocity;
      const panner = ctx.createStereoPanner();
      panner.pan.value = pan;
      source.connect(gain).connect(panner).connect(ctx.destination);
      track(source);
      source.start(time);
    },
    playNote(note, instrument, time, duration, velocity, pan) {
      if (!this.voices) return;
      for (const source of this.voices.playNote(ctx, ctx.destination, note, instrument, time, duration, velocity, pan)) {
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
