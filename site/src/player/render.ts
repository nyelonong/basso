import { createMasterBus, createWebAudioSink } from "./audio";
import { scheduleBar } from "./scheduler";
import { stepSeconds, type AudioSink, type Bar, type BarProvider, type Diagnostic } from "./model";

export const RENDER_SAMPLE_RATE = 44100;
// Five minutes of stereo float audio is about 100 MB; longer renders risk the tab running out of memory.
export const MAX_RENDER_SECONDS = 300;
const CHANNELS = 2;
// The limiter squashes a hit that arrives before it has settled, so the bars
// start after this much silence, which is trimmed from the file.
const LEAD_IN_SECONDS = 0.1;

export type RenderedAudio = {
  readonly numberOfChannels: number;
  readonly sampleRate: number;
  readonly length: number;
  getChannelData(channel: number): Float32Array;
};

export type RenderContext = BaseAudioContext & { startRendering(): Promise<RenderedAudio> };

export type RenderDeps = {
  createContext(channels: number, frames: number, sampleRate: number): RenderContext;
  fetchBytes(url: string): Promise<ArrayBuffer>;
  createSink?(ctx: RenderContext, fetchBytes: (url: string) => Promise<ArrayBuffer>): Promise<AudioSink>;
  measureDelay?(): Promise<number>;
};

// Frames between a sound entering the master bus and leaving it. The limiter's
// lookahead and the clipper's oversampling differ between browsers, so it is
// measured with a single click rather than assumed.
export async function measureMasterBusDelay(createContext: RenderDeps["createContext"]): Promise<number> {
  const lead = Math.round(LEAD_IN_SECONDS * RENDER_SAMPLE_RATE);
  const ctx = createContext(1, lead * 2, RENDER_SAMPLE_RATE);
  const click = ctx.createBuffer(1, 1, RENDER_SAMPLE_RATE);
  click.getChannelData(0)[0] = 0.5;
  const source = ctx.createBufferSource();
  source.buffer = click;
  source.connect(createMasterBus(ctx));
  source.start(LEAD_IN_SECONDS);
  const out = (await ctx.startRendering()).getChannelData(0);
  let peak = lead;
  for (let i = lead; i < out.length; i++) if (Math.abs(out[i]) > Math.abs(out[peak])) peak = i;
  return peak - lead;
}

export type RenderResult = { ok: true; wav: Uint8Array<ArrayBuffer>; seconds: number } | { ok: false; error: Diagnostic };

// Evaluates every bar before rendering, so a broken pattern reports its error
// instead of producing a file with silent gaps. The file ends exactly on the
// last bar line so it loops cleanly; sounds still ringing there are cut.
export async function renderPattern(provider: BarProvider, barCount: number, deps: RenderDeps): Promise<RenderResult> {
  const bars: Bar[] = [];
  for (let n = 0; n < barCount; n++) {
    const result = provider(n);
    if (!result.ok) return result;
    bars.push(result.bar);
  }

  const seconds = bars.reduce((total, bar) => total + bar.steps * stepSeconds(bar.bpm), 0);
  if (seconds > MAX_RENDER_SECONDS) {
    return {
      ok: false,
      error: { message: `${barCount} bars last ${Math.round(seconds)} seconds; renders are limited to ${MAX_RENDER_SECONDS}` },
    };
  }
  // Trimming the lead-in and the bus delay from the front puts every hit
  // exactly on its grid line when the file is placed in a DAW.
  const delay = await (deps.measureDelay ?? (() => measureMasterBusDelay(deps.createContext)))();
  const skip = Math.round(LEAD_IN_SECONDS * RENDER_SAMPLE_RATE) + delay;
  const frames = Math.max(1, Math.round(seconds * RENDER_SAMPLE_RATE));
  const ctx = deps.createContext(CHANNELS, skip + frames, RENDER_SAMPLE_RATE);
  const sink = await (deps.createSink ?? createWebAudioSink)(ctx, deps.fetchBytes);
  let start = LEAD_IN_SECONDS;
  for (const bar of bars) start += scheduleBar(sink, bar, start);
  return { ok: true, wav: encodeWav(await ctx.startRendering(), skip), seconds };
}

// 16-bit PCM WAV of audio from startFrame on; samples outside [-1, 1] are clipped as a sound card would.
export function encodeWav(audio: RenderedAudio, startFrame = 0): Uint8Array<ArrayBuffer> {
  const channels = audio.numberOfChannels;
  const length = Math.max(0, audio.length - startFrame);
  const dataSize = length * channels * 2;
  const bytes = new Uint8Array(44 + dataSize);
  const view = new DataView(bytes.buffer);
  const text = (offset: number, value: string) => {
    for (let i = 0; i < value.length; i++) view.setUint8(offset + i, value.charCodeAt(i));
  };
  text(0, "RIFF");
  view.setUint32(4, 36 + dataSize, true);
  text(8, "WAVEfmt ");
  view.setUint32(16, 16, true);
  view.setUint16(20, 1, true);
  view.setUint16(22, channels, true);
  view.setUint32(24, audio.sampleRate, true);
  view.setUint32(28, audio.sampleRate * channels * 2, true);
  view.setUint16(32, channels * 2, true);
  view.setUint16(34, 16, true);
  text(36, "data");
  view.setUint32(40, dataSize, true);

  const data = Array.from({ length: channels }, (_, c) => audio.getChannelData(c));
  let offset = 44;
  for (let i = startFrame; i < audio.length; i++) {
    for (const channel of data) {
      const sample = Math.max(-1, Math.min(1, channel[i]));
      view.setInt16(offset, Math.round(sample * 32767), true);
      offset += 2;
    }
  }
  return bytes;
}
