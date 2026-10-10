const NOTE_NAME = /^([A-Ga-g])([#b]?)(-?[0-9]+)$/;

const SEMITONES: Record<string, number> = { C: 0, D: 2, E: 4, F: 5, G: 7, A: 9, B: 11 };

export function noteFrequency(note: string): number {
  const match = NOTE_NAME.exec(note);
  if (!match) throw new Error(`invalid note name ${JSON.stringify(note)}`);
  let semitone = SEMITONES[match[1].toUpperCase()];
  if (match[2] === "#") semitone++;
  if (match[2] === "b") semitone--;
  const midi = (Number(match[3]) + 1) * 12 + semitone;
  return 440 * 2 ** ((midi - 69) / 12);
}
