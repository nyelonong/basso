export class FakeParam {
  value = 0;
  events: [string, number, number][] = [];
  setValueAtTime(v: number, t: number) {
    this.events.push(["set", v, t]);
    return this;
  }
  linearRampToValueAtTime(v: number, t: number) {
    this.events.push(["linear", v, t]);
    return this;
  }
  exponentialRampToValueAtTime(v: number, t: number) {
    this.events.push(["exp", v, t]);
    return this;
  }
  setTargetAtTime(v: number, t: number, c: number) {
    this.events.push(["target", v, t + c]);
    return this;
  }
  cancelScheduledValues() {
    return this;
  }
}

export class FakeNode {
  connections: FakeNode[] = [];
  constructor(
    readonly kind: string,
    readonly context: FakeContext,
  ) {}
  connect(node: FakeNode) {
    this.connections.push(node);
    return node;
  }
  disconnect() {}
}

export class FakeSource extends FakeNode {
  buffer: unknown = null;
  type = "sine";
  frequency = new FakeParam();
  detune = new FakeParam();
  startedAt: number | null = null;
  stoppedAt: number | null = null;
  onended: (() => void) | null = null;
  start(t = 0) {
    this.startedAt = t;
  }
  stop(t = 0) {
    this.stoppedAt = t;
  }
}

export class FakeGain extends FakeNode {
  gain = new FakeParam();
}

export class FakePanner extends FakeNode {
  pan = new FakeParam();
}

export class FakeFilter extends FakeNode {
  type = "lowpass";
  frequency = new FakeParam();
  Q = new FakeParam();
}

export class FakeIIR extends FakeNode {
  constructor(
    context: FakeContext,
    readonly feedforward: number[],
    readonly feedback: number[],
  ) {
    super("iir", context);
  }
}

export class FakeBuffer {
  constructor(
    readonly numberOfChannels: number,
    readonly length: number,
    readonly sampleRate: number,
    readonly data = new Float32Array(length),
  ) {}
  getChannelData() {
    return this.data;
  }
}

export class FakeContext {
  currentTime = 0;
  sampleRate = 44100;
  nodes: FakeNode[] = [];
  decoded: ArrayBuffer[] = [];
  destination = new FakeNode("destination", this);
  private add<T extends FakeNode>(node: T): T {
    this.nodes.push(node);
    return node;
  }
  createBufferSource() {
    return this.add(new FakeSource("buffer", this));
  }
  createOscillator() {
    return this.add(new FakeSource("oscillator", this));
  }
  createGain() {
    return this.add(new FakeGain("gain", this));
  }
  createStereoPanner() {
    return this.add(new FakePanner("panner", this));
  }
  createBiquadFilter() {
    return this.add(new FakeFilter("filter", this));
  }
  createIIRFilter(feedforward: number[], feedback: number[]) {
    return this.add(new FakeIIR(this, feedforward, feedback));
  }
  createBuffer(channels: number, length: number, sampleRate: number) {
    return new FakeBuffer(channels, length, sampleRate);
  }
  async decodeAudioData(bytes: ArrayBuffer) {
    this.decoded.push(bytes);
    return { bytes };
  }
  sources() {
    return this.nodes.filter((n): n is FakeSource => n instanceof FakeSource);
  }
  asContext() {
    return this as unknown as BaseAudioContext;
  }
}
