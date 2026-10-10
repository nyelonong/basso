import { describe, expect, it } from "vitest";
import { noteFrequency } from "../../src/player/notes";

describe("noteFrequency", () => {
  it.each([
    ["A4", 440],
    ["C4", 261.6256],
    ["C-1", 8.1757989156],
    ["a4", 440],
  ])("%s is %f Hz", (note, hz) => {
    expect(noteFrequency(note)).toBeCloseTo(hz, 3);
  });

  it("treats sharps and flats as the same pitch", () => {
    expect(noteFrequency("C#2")).toBeCloseTo(noteFrequency("Db2"), 9);
  });

  it.each(["", "H4", "C#", "4", "C4 ", "Cb#4"])("rejects %j", (note) => {
    expect(() => noteFrequency(note)).toThrow(/invalid note/);
  });
});
