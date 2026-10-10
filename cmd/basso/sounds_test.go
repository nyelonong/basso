package main

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"testing/fstest"

	"github.com/nyelonong/basso/internal/engine"
	"github.com/nyelonong/basso/sound"
)

func TestResolveSoundsDirUsesExplicitFlagRelativeToInvocation(t *testing.T) {
	dir := t.TempDir()
	got, err := resolveSoundsDir(dir, "kits", failUnpack(t))
	if err != nil {
		t.Fatalf("resolveSoundsDir: %v", err)
	}
	if want := filepath.Join(dir, "kits"); got != want {
		t.Fatalf("sounds dir = %q, want %q", got, want)
	}
}

func TestResolveSoundsDirPrefersCheckoutSamples(t *testing.T) {
	dir := t.TempDir()
	checkout := filepath.Join(dir, "sound", "808")
	if err := os.MkdirAll(checkout, 0o700); err != nil {
		t.Fatal(err)
	}
	got, err := resolveSoundsDir(dir, "", failUnpack(t))
	if err != nil {
		t.Fatalf("resolveSoundsDir: %v", err)
	}
	if got != checkout {
		t.Fatalf("sounds dir = %q, want %q", got, checkout)
	}
}

func TestResolveSoundsDirFallsBackToBuiltInKit(t *testing.T) {
	got, err := resolveSoundsDir(t.TempDir(), "", func() (string, error) { return "/cache/kit", nil })
	if err != nil {
		t.Fatalf("resolveSoundsDir: %v", err)
	}
	if got != "/cache/kit" {
		t.Fatalf("sounds dir = %q, want the unpacked kit", got)
	}
}

func TestResolveSoundsDirReportsUnpackFailure(t *testing.T) {
	boom := errors.New("disk full")
	_, err := resolveSoundsDir(t.TempDir(), "", func() (string, error) { return "", boom })
	if !errors.Is(err, boom) {
		t.Fatalf("err = %v, want %v", err, boom)
	}
}

func TestUnpackSoundsWritesAPlayableInventory(t *testing.T) {
	kit := fstest.MapFS{
		"808/kick.wav":  {Data: []byte("kick")},
		"808/snare.wav": {Data: []byte("snare")},
	}
	cache := t.TempDir()
	dir, err := unpackSounds(cache, kit)
	if err != nil {
		t.Fatalf("unpackSounds: %v", err)
	}
	inventory, err := engine.LoadSoundInventory(dir)
	if err != nil {
		t.Fatalf("LoadSoundInventory: %v", err)
	}
	if len(inventory) != 2 {
		t.Fatalf("inventory = %v, want kick.wav and snare.wav", inventory)
	}
	data, err := os.ReadFile(filepath.Join(dir, "snare.wav"))
	if err != nil || string(data) != "snare" {
		t.Fatalf("snare.wav = %q, %v", data, err)
	}
}

func TestUnpackSoundsReusesAndSeparatesKitsByContent(t *testing.T) {
	cache := t.TempDir()
	first, err := unpackSounds(cache, fstest.MapFS{"808/kick.wav": {Data: []byte("v1")}})
	if err != nil {
		t.Fatal(err)
	}
	again, err := unpackSounds(cache, fstest.MapFS{"808/kick.wav": {Data: []byte("v1")}})
	if err != nil {
		t.Fatal(err)
	}
	changed, err := unpackSounds(cache, fstest.MapFS{"808/kick.wav": {Data: []byte("v2")}})
	if err != nil {
		t.Fatal(err)
	}
	if first != again {
		t.Fatalf("same kit unpacked to %q then %q, want reuse", first, again)
	}
	if changed == first {
		t.Fatalf("changed kit reused %q, want a separate directory", first)
	}
}

func TestUnpackSoundsRepairsAnIncompleteKit(t *testing.T) {
	kit := fstest.MapFS{
		"808/kick.wav":  {Data: []byte("kick")},
		"808/snare.wav": {Data: []byte("snare")},
	}
	cache := t.TempDir()
	dir, err := unpackSounds(cache, kit)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(dir, "snare.wav")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "kick.wav"), []byte("k"), 0o644); err != nil {
		t.Fatal(err)
	}

	again, err := unpackSounds(cache, kit)
	if err != nil {
		t.Fatalf("unpackSounds: %v", err)
	}
	for name, want := range map[string]string{"kick.wav": "kick", "snare.wav": "snare"} {
		data, err := os.ReadFile(filepath.Join(again, name))
		if err != nil || string(data) != want {
			t.Fatalf("%s = %q, %v; want %q", name, data, err, want)
		}
	}
}

func TestBuiltInKitHoldsEvery808Sample(t *testing.T) {
	entries, err := sound.Kit808.ReadDir("808")
	if err != nil {
		t.Fatalf("read built-in kit: %v", err)
	}
	disk, err := os.ReadDir(filepath.Join("..", "..", "sound", "808"))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != len(disk) || len(entries) == 0 {
		t.Fatalf("built-in kit has %d files, sound/808 has %d", len(entries), len(disk))
	}
}

func failUnpack(t *testing.T) func() (string, error) {
	return func() (string, error) {
		t.Fatal("unexpected unpack of the built-in kit")
		return "", nil
	}
}
