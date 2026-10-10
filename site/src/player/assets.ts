const sampleUrls = import.meta.glob<string>("../../../sound/808/*.wav", {
  query: "?url",
  import: "default",
  eager: true,
});
const patternSources = import.meta.glob<string>("../../../patterns/*.fnl", {
  query: "?raw",
  import: "default",
  eager: true,
});

const basename = (path: string) => path.slice(path.lastIndexOf("/") + 1);

const urlBySample = new Map(Object.entries(sampleUrls).map(([path, url]) => [basename(path), url]));

export const SAMPLE_NAMES: readonly string[] = [...urlBySample.keys()].sort();

export function sampleUrl(name: string): string {
  const url = urlBySample.get(name);
  if (url === undefined) throw new Error(`unknown sample ${JSON.stringify(name)}`);
  return url;
}

export const EXAMPLE_PATTERNS: readonly { name: string; source: string }[] = Object.entries(patternSources)
  .map(([path, source]) => ({ name: basename(path).replace(/\.fnl$/, ""), source }))
  .sort((a, b) => a.name.localeCompare(b.name));
