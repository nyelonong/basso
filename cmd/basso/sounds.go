package main

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sync"

	"github.com/nyelonong/basso/sound"
)

const checkoutSoundsDir = "sound/808"

const soundsFlagHelp = "sound inventory directory (default: ./sound/808 when present, otherwise the built-in 808 kit)"

// defaultSounds resolves the sounds directory for `basso play` once per process.
var defaultSounds = sync.OnceValues(func() (string, error) {
	invocationDir, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("resolve sounds directory: %w", err)
	}
	return resolveSoundsDir(invocationDir, "", unpackBuiltInSounds)
})

// resolveSoundsDir returns flagValue resolved against invocationDir. With no
// flag it keeps a checkout's sound/808, so editing samples in a checkout still
// works, and otherwise falls back to the samples built into the binary.
func resolveSoundsDir(invocationDir, flagValue string, unpack func() (string, error)) (string, error) {
	if flagValue != "" {
		return absoluteFrom(invocationDir, flagValue)
	}
	checkout := filepath.Join(invocationDir, filepath.FromSlash(checkoutSoundsDir))
	if info, err := os.Stat(checkout); err == nil && info.IsDir() {
		return checkout, nil
	}
	dir, err := unpack()
	if err != nil {
		return "", fmt.Errorf("unpack built-in sounds: %w", err)
	}
	return dir, nil
}

func unpackBuiltInSounds() (string, error) {
	cache, err := os.UserCacheDir()
	if err != nil {
		return "", err
	}
	return unpackSounds(cache, sound.Kit808)
}

// unpackSounds copies kit's 808 directory to a directory under cacheRoot named
// by the kit's content hash, so different basso versions never share samples.
// The engine reads samples from disk, which is why they are unpacked at all.
func unpackSounds(cacheRoot string, kit fs.FS) (string, error) {
	entries, err := fs.ReadDir(kit, "808")
	if err != nil {
		return "", err
	}
	files := make(map[string][]byte, len(entries))
	hash := sha256.New()
	for _, entry := range entries {
		if !entry.Type().IsRegular() {
			continue
		}
		data, err := fs.ReadFile(kit, path.Join("808", entry.Name()))
		if err != nil {
			return "", err
		}
		files[entry.Name()] = data
		fmt.Fprintf(hash, "%s\x00%d\x00", entry.Name(), len(data))
		hash.Write(data)
	}

	parent := filepath.Join(cacheRoot, "basso", "sounds")
	target := filepath.Join(parent, "808-"+hex.EncodeToString(hash.Sum(nil))[:16])
	if complete(target, files) {
		return target, nil
	}
	if err := os.MkdirAll(parent, 0o755); err != nil {
		return "", err
	}
	staging, err := os.MkdirTemp(parent, ".808-")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(staging)
	for name, data := range files {
		if err := os.WriteFile(filepath.Join(staging, name), data, 0o644); err != nil {
			return "", err
		}
	}
	// A cache cleaner can leave target with files missing; move it aside so the
	// fresh copy can take its place.
	if _, err := os.Stat(target); err == nil && !complete(target, files) {
		stale, err := os.MkdirTemp(parent, ".stale-")
		if err != nil {
			return "", err
		}
		defer os.RemoveAll(stale)
		if err := os.Rename(target, filepath.Join(stale, "808")); err != nil && !complete(target, files) {
			return "", fmt.Errorf("replace incomplete sounds: %w", err)
		}
	}
	if err := os.Rename(staging, target); err != nil {
		// Another basso process may have unpacked the same kit first.
		if complete(target, files) {
			return target, nil
		}
		return "", fmt.Errorf("install sounds: %w", err)
	}
	return target, nil
}

func complete(dir string, files map[string][]byte) bool {
	for name, data := range files {
		info, err := os.Stat(filepath.Join(dir, name))
		if err != nil || !info.Mode().IsRegular() || info.Size() != int64(len(data)) {
			return false
		}
	}
	return len(files) > 0
}
