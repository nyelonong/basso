package suggest

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestStore_SaveCreatesSchemaV1Pair(t *testing.T) {
	t.Parallel()

	root := filepath.Join(t.TempDir(), ".basso")
	now := time.Date(2026, time.July, 25, 12, 0, 0, 123456789, time.FixedZone("WIB", 7*60*60))
	store := NewStore(root, func() time.Time { return now })

	candidate := testCandidate([]byte("(bpm 120)\n"))
	got, err := store.Save(candidate)
	if err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	wantHash := testSHA256Hex(candidate.Source)
	if got.Metadata.SchemaVersion != 1 {
		t.Fatalf("SchemaVersion = %d, want 1", got.Metadata.SchemaVersion)
	}
	if got.Metadata.CandidateSHA256 != wantHash {
		t.Fatalf("CandidateSHA256 = %q, want %q", got.Metadata.CandidateSHA256, wantHash)
	}
	if !got.Metadata.CreatedAt.Equal(now.UTC()) {
		t.Fatalf("CreatedAt = %s, want %s", got.Metadata.CreatedAt, now.UTC())
	}

	sourcePath := filepath.Join(root, "candidates", got.Metadata.ID+".fnl")
	metadataPath := filepath.Join(root, "candidates", got.Metadata.ID+".json")
	if source, err := os.ReadFile(sourcePath); err != nil || string(source) != string(candidate.Source) {
		t.Fatalf("source file = %q, %v; want %q, nil", source, err, candidate.Source)
	}

	metadataJSON, err := os.ReadFile(metadataPath)
	if err != nil {
		t.Fatalf("read metadata: %v", err)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(metadataJSON, &fields); err != nil {
		t.Fatalf("decode metadata JSON: %v", err)
	}
	wantFields := []string{
		"attempts", "base_sha256", "candidate_sha256", "created_at", "id", "model",
		"prompt", "provider", "schema_version", "source_path", "sounds_path", "summary", "validation",
	}
	if len(fields) != len(wantFields) {
		t.Fatalf("metadata field count = %d, want %d: %v", len(fields), len(wantFields), fields)
	}
	for _, field := range wantFields {
		if _, ok := fields[field]; !ok {
			t.Errorf("metadata is missing %q", field)
		}
	}
	var createdAt string
	if err := json.Unmarshal(fields["created_at"], &createdAt); err != nil {
		t.Fatalf("decode created_at: %v", err)
	}
	if createdAt != now.UTC().Format(time.RFC3339Nano) {
		t.Errorf("created_at = %q, want RFC3339Nano %q", createdAt, now.UTC().Format(time.RFC3339Nano))
	}
	var validation map[string]json.RawMessage
	if err := json.Unmarshal(fields["validation"], &validation); err != nil {
		t.Fatalf("decode validation: %v", err)
	}
	wantValidationFields := []string{"first_bar", "last_bar", "status", "timeout_ms_per_bar"}
	if len(validation) != len(wantValidationFields) {
		t.Fatalf("validation field count = %d, want %d: %v", len(validation), len(wantValidationFields), validation)
	}
	for _, field := range wantValidationFields {
		if _, ok := validation[field]; !ok {
			t.Errorf("validation is missing %q", field)
		}
	}

	loaded, err := store.Load(got.Metadata.ID)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if !reflect.DeepEqual(loaded, got) {
		t.Errorf("Load() = %#v, want %#v", loaded, got)
	}
}

func TestStore_SaveUsesTimestampAndCandidateHashID(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.July, 25, 12, 0, 0, 123456789, time.FixedZone("WIB", 7*60*60))
	source := []byte("(steps 16)\n")
	got, err := NewStore(filepath.Join(t.TempDir(), ".basso"), func() time.Time { return now }).Save(testCandidate(source))
	if err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	wantHash := testSHA256Hex(source)
	wantID := now.UTC().Format("20060102T150405.000000000Z") + "-" + wantHash[:12]
	if got.Metadata.ID != wantID {
		t.Errorf("ID = %q, want %q", got.Metadata.ID, wantID)
	}
}

func TestStore_SaveUsesPrivatePermissions(t *testing.T) {
	t.Parallel()

	root := filepath.Join(t.TempDir(), ".basso")
	got, err := NewStore(root, fixedNow).Save(testCandidate([]byte("(pattern 1)\n")))
	if err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	for _, path := range []string{root, filepath.Join(root, "candidates")} {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatalf("stat %s: %v", path, err)
		}
		if got := info.Mode().Perm(); got != 0o700 {
			t.Errorf("%s permissions = %#o, want 0700", path, got)
		}
	}
	for _, path := range []string{
		filepath.Join(root, "candidates", got.Metadata.ID+".fnl"),
		filepath.Join(root, "candidates", got.Metadata.ID+".json"),
	} {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatalf("stat %s: %v", path, err)
		}
		if got := info.Mode().Perm(); got != 0o600 {
			t.Errorf("%s permissions = %#o, want 0600", path, got)
		}
	}
}

func TestStore_SaveIsExclusive(t *testing.T) {
	t.Parallel()

	root := filepath.Join(t.TempDir(), ".basso")
	store := NewStore(root, fixedNow)
	first, err := store.Save(testCandidate([]byte("first")))
	if err != nil {
		t.Fatalf("first Save() error = %v", err)
	}
	if _, err := store.Save(testCandidate([]byte("first"))); err == nil {
		t.Fatal("second Save() error = nil, want exclusive-create error")
	}
	content, err := os.ReadFile(filepath.Join(root, "candidates", first.Metadata.ID+".fnl"))
	if err != nil {
		t.Fatalf("read original source: %v", err)
	}
	if string(content) != "first" {
		t.Errorf("exclusive save overwrote source: %q", content)
	}
}

func TestStore_SaveCleansUpPartialPair(t *testing.T) {
	t.Parallel()

	root := filepath.Join(t.TempDir(), ".basso")
	store := NewStore(root, fixedNow)
	candidate := testCandidate([]byte("partial"))
	hash := testSHA256Hex(candidate.Source)
	id := fixedNow().UTC().Format("20060102T150405.000000000Z") + "-" + hash[:12]
	directory := filepath.Join(root, "candidates")
	if err := os.MkdirAll(directory, 0o700); err != nil {
		t.Fatalf("MkdirAll(): %v", err)
	}
	metadataPath := filepath.Join(directory, id+".json")
	if err := os.WriteFile(metadataPath, []byte("pre-existing"), 0o600); err != nil {
		t.Fatalf("write pre-existing metadata: %v", err)
	}

	if _, err := store.Save(candidate); err == nil {
		t.Fatal("Save() error = nil, want exclusive-create error")
	}
	if _, err := os.Stat(filepath.Join(directory, id+".fnl")); !os.IsNotExist(err) {
		t.Errorf("partial source exists after failed save: %v", err)
	}
	metadata, err := os.ReadFile(metadataPath)
	if err != nil {
		t.Fatalf("read pre-existing metadata: %v", err)
	}
	if string(metadata) != "pre-existing" {
		t.Errorf("pre-existing metadata = %q, want unchanged", metadata)
	}
}

func TestStore_LoadRejectsUnknownFields(t *testing.T) {
	t.Parallel()

	root, store, saved := saveTestCandidate(t)
	metadataPath := filepath.Join(root, "candidates", saved.Metadata.ID+".json")
	metadata := readMetadataObject(t, metadataPath)
	metadata["unexpected"] = true
	writeMetadataObject(t, metadataPath, metadata)

	if _, err := store.Load(saved.Metadata.ID); err == nil {
		t.Fatal("Load() error = nil, want unknown-field rejection")
	}
}

func TestStore_LoadRejectsMalformedMetadata(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name   string
		mutate func(map[string]any)
	}{
		{"missing required field", func(metadata map[string]any) { delete(metadata, "provider") }},
		{"unsupported schema", func(metadata map[string]any) { metadata["schema_version"] = 2 }},
		{"uppercase hash", func(metadata map[string]any) { metadata["base_sha256"] = strings.Repeat("A", 64) }},
		{"invalid validation status", func(metadata map[string]any) {
			metadata["validation"].(map[string]any)["status"] = "failed"
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			root, store, saved := saveTestCandidate(t)
			metadataPath := filepath.Join(root, "candidates", saved.Metadata.ID+".json")
			metadata := readMetadataObject(t, metadataPath)
			tc.mutate(metadata)
			writeMetadataObject(t, metadataPath, metadata)

			if _, err := store.Load(saved.Metadata.ID); err == nil {
				t.Fatal("Load() error = nil, want malformed metadata rejection")
			}
		})
	}
}

func TestStore_LoadRejectsHashMismatch(t *testing.T) {
	t.Parallel()

	root, store, saved := saveTestCandidate(t)
	sourcePath := filepath.Join(root, "candidates", saved.Metadata.ID+".fnl")
	if err := os.WriteFile(sourcePath, []byte("tampered"), 0o600); err != nil {
		t.Fatalf("tamper source: %v", err)
	}

	if _, err := store.Load(saved.Metadata.ID); err == nil {
		t.Fatal("Load() error = nil, want candidate hash mismatch")
	}
}

func saveTestCandidate(t *testing.T) (string, *Store, Candidate) {
	t.Helper()
	root := filepath.Join(t.TempDir(), ".basso")
	store := NewStore(root, fixedNow)
	saved, err := store.Save(testCandidate([]byte("candidate source")))
	if err != nil {
		t.Fatalf("Save() error = %v", err)
	}
	return root, store, saved
}

func readMetadataObject(t *testing.T, path string) map[string]any {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read metadata: %v", err)
	}
	var metadata map[string]any
	if err := json.Unmarshal(data, &metadata); err != nil {
		t.Fatalf("decode metadata: %v", err)
	}
	return metadata
}

func writeMetadataObject(t *testing.T, path string, metadata map[string]any) {
	t.Helper()
	data, err := json.Marshal(metadata)
	if err != nil {
		t.Fatalf("marshal metadata: %v", err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatalf("write metadata: %v", err)
	}
}

func testCandidate(source []byte) Candidate {
	return Candidate{
		Metadata: Metadata{
			SourcePath: "/patterns/source.fnl",
			SoundsPath: "/sounds/808",
			BaseSHA256: strings.Repeat("a", 64),
			Provider:   "openai",
			Model:      "gpt-test",
			Prompt:     "make it brighter",
			Summary:    "Added hats.",
			Attempts:   1,
			Validation: ValidationRecord{
				FirstBar:        0,
				LastBar:         15,
				TimeoutMSPerBar: 250,
				Status:          "passed",
			},
		},
		Source: source,
	}
}

func fixedNow() time.Time {
	return time.Date(2026, time.July, 25, 12, 0, 0, 123456789, time.UTC)
}

func testSHA256Hex(source []byte) string {
	sum := sha256.Sum256(source)
	return hex.EncodeToString(sum[:])
}
