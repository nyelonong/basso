package suggest

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	schemaVersion      = 1
	directoryMode      = 0o700
	fileMode           = 0o600
	candidateDirectory = "candidates"
)

// ValidationRecord records the local validation that made a candidate eligible
// for persistence.
type ValidationRecord struct {
	FirstBar        int    `json:"first_bar"`
	LastBar         int    `json:"last_bar"`
	TimeoutMSPerBar int    `json:"timeout_ms_per_bar"`
	Status          string `json:"status"`
}

// Metadata is the schema-v1 provenance record for a candidate source file.
type Metadata struct {
	SchemaVersion   int              `json:"schema_version"`
	ID              string           `json:"id"`
	CreatedAt       time.Time        `json:"created_at"`
	SourcePath      string           `json:"source_path"`
	SoundsPath      string           `json:"sounds_path"`
	BaseSHA256      string           `json:"base_sha256"`
	CandidateSHA256 string           `json:"candidate_sha256"`
	Provider        string           `json:"provider"`
	Model           string           `json:"model"`
	Prompt          string           `json:"prompt"`
	Summary         string           `json:"summary"`
	Attempts        int              `json:"attempts"`
	Validation      ValidationRecord `json:"validation"`
}

// Candidate is a validated source revision and its schema-v1 metadata.
type Candidate struct {
	Metadata Metadata
	Source   []byte
}

// Store persists candidate pairs under a caller-supplied .basso root.
type Store struct {
	root string
	now  func() time.Time
}

// NewStore creates a candidate store rooted at root.
func NewStore(root string, now func() time.Time) *Store {
	if now == nil {
		now = time.Now
	}
	return &Store{root: root, now: now}
}

// CandidateSourcePath returns the stored source path for id.
func (s *Store) CandidateSourcePath(id string) (string, error) {
	if s == nil || s.root == "" {
		return "", errors.New("candidate store root is empty")
	}
	if !safeCandidateID(id) {
		return "", fmt.Errorf("invalid candidate ID %q", id)
	}
	return filepath.Join(s.root, candidateDirectory, id+".fnl"), nil
}

// Save writes a candidate source and metadata pair using exclusive, private
// file creation. It fills the schema version, creation time, candidate hash,
// and deterministic ID.
func (s *Store) Save(candidate Candidate) (Candidate, error) {
	if s == nil {
		return Candidate{}, errors.New("candidate store is nil")
	}
	if s.root == "" {
		return Candidate{}, errors.New("candidate store root is empty")
	}
	if len(candidate.Source) == 0 {
		return Candidate{}, errors.New("candidate source is empty")
	}

	createdAt := s.now().UTC()
	candidate.Metadata.SchemaVersion = schemaVersion
	candidate.Metadata.CreatedAt = createdAt
	candidate.Metadata.CandidateSHA256 = sha256Hex(candidate.Source)
	candidate.Metadata.ID = candidateID(createdAt, candidate.Metadata.CandidateSHA256)
	if err := validateMetadata(candidate.Metadata); err != nil {
		return Candidate{}, fmt.Errorf("validate candidate metadata: %w", err)
	}
	metadata, err := json.Marshal(candidate.Metadata)
	if err != nil {
		return Candidate{}, fmt.Errorf("marshal candidate metadata: %w", err)
	}

	directory := filepath.Join(s.root, candidateDirectory)
	if err := makePrivateDirectory(s.root); err != nil {
		return Candidate{}, err
	}
	if err := makePrivateDirectory(directory); err != nil {
		return Candidate{}, err
	}

	sourcePath := filepath.Join(directory, candidate.Metadata.ID+".fnl")
	metadataPath := filepath.Join(directory, candidate.Metadata.ID+".json")
	if err := writeExclusive(sourcePath, candidate.Source); err != nil {
		return Candidate{}, fmt.Errorf("create candidate source: %w", err)
	}
	if err := writeExclusive(metadataPath, metadata); err != nil {
		if removeErr := os.Remove(sourcePath); removeErr != nil && !os.IsNotExist(removeErr) {
			return Candidate{}, fmt.Errorf("create candidate metadata: %w (remove partial source: %v)", err, removeErr)
		}
		return Candidate{}, fmt.Errorf("create candidate metadata: %w", err)
	}
	return candidate, nil
}

// Load reads one strict schema-v1 candidate pair and verifies its source hash.
func (s *Store) Load(id string) (Candidate, error) {
	if s == nil {
		return Candidate{}, errors.New("candidate store is nil")
	}
	if !safeCandidateID(id) {
		return Candidate{}, fmt.Errorf("invalid candidate ID %q", id)
	}
	directory := filepath.Join(s.root, candidateDirectory)
	metadataPath := filepath.Join(directory, id+".json")
	metadataJSON, err := os.ReadFile(metadataPath)
	if err != nil {
		return Candidate{}, fmt.Errorf("read candidate metadata: %w", err)
	}
	metadata, err := decodeMetadata(metadataJSON)
	if err != nil {
		return Candidate{}, fmt.Errorf("decode candidate metadata: %w", err)
	}
	if metadata.ID != id {
		return Candidate{}, fmt.Errorf("candidate metadata ID %q does not match requested ID %q", metadata.ID, id)
	}
	source, err := os.ReadFile(filepath.Join(directory, id+".fnl"))
	if err != nil {
		return Candidate{}, fmt.Errorf("read candidate source: %w", err)
	}
	if sha256Hex(source) != metadata.CandidateSHA256 {
		return Candidate{}, errors.New("candidate source hash does not match metadata")
	}
	if metadata.ID != candidateID(metadata.CreatedAt, metadata.CandidateSHA256) {
		return Candidate{}, errors.New("candidate ID does not match timestamp and source hash")
	}
	return Candidate{Metadata: metadata, Source: source}, nil
}

func makePrivateDirectory(path string) error {
	if err := os.MkdirAll(path, directoryMode); err != nil {
		return fmt.Errorf("create candidate directory: %w", err)
	}
	if err := os.Chmod(path, directoryMode); err != nil {
		return fmt.Errorf("set candidate directory permissions: %w", err)
	}
	return nil
}

func writeExclusive(path string, data []byte) (err error) {
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, fileMode)
	if err != nil {
		return err
	}
	defer func() {
		if closeErr := file.Close(); err == nil && closeErr != nil {
			err = closeErr
		}
	}()
	if _, err := file.Write(data); err != nil {
		return err
	}
	return file.Chmod(fileMode)
}

func decodeMetadata(data []byte) (Metadata, error) {
	decoder := json.NewDecoder(bytes.NewReader(data))
	token, err := decoder.Token()
	if err != nil {
		return Metadata{}, err
	}
	if token != json.Delim('{') {
		return Metadata{}, errors.New("metadata must be a JSON object")
	}

	var metadata Metadata
	seen := make(map[string]bool, 13)
	for decoder.More() {
		keyToken, err := decoder.Token()
		if err != nil {
			return Metadata{}, err
		}
		key, ok := keyToken.(string)
		if !ok {
			return Metadata{}, errors.New("metadata field name must be a string")
		}
		if seen[key] {
			return Metadata{}, fmt.Errorf("duplicate metadata field %q", key)
		}
		seen[key] = true

		var raw json.RawMessage
		if err := decoder.Decode(&raw); err != nil {
			return Metadata{}, fmt.Errorf("decode metadata field %q: %w", key, err)
		}
		switch key {
		case "schema_version":
			err = json.Unmarshal(raw, &metadata.SchemaVersion)
		case "id":
			err = json.Unmarshal(raw, &metadata.ID)
		case "created_at":
			err = decodeCreatedAt(raw, &metadata.CreatedAt)
		case "source_path":
			err = json.Unmarshal(raw, &metadata.SourcePath)
		case "sounds_path":
			err = json.Unmarshal(raw, &metadata.SoundsPath)
		case "base_sha256":
			err = json.Unmarshal(raw, &metadata.BaseSHA256)
		case "candidate_sha256":
			err = json.Unmarshal(raw, &metadata.CandidateSHA256)
		case "provider":
			err = json.Unmarshal(raw, &metadata.Provider)
		case "model":
			err = json.Unmarshal(raw, &metadata.Model)
		case "prompt":
			err = json.Unmarshal(raw, &metadata.Prompt)
		case "summary":
			err = json.Unmarshal(raw, &metadata.Summary)
		case "attempts":
			err = json.Unmarshal(raw, &metadata.Attempts)
		case "validation":
			err = decodeValidation(raw, &metadata.Validation)
		default:
			return Metadata{}, fmt.Errorf("unknown metadata field %q", key)
		}
		if err != nil {
			return Metadata{}, fmt.Errorf("invalid metadata field %q: %w", key, err)
		}
	}
	if _, err := decoder.Token(); err != nil {
		return Metadata{}, err
	}
	if err := ensureEOF(decoder); err != nil {
		return Metadata{}, err
	}
	if len(seen) != 13 {
		return Metadata{}, errors.New("metadata has missing required fields")
	}
	if err := validateMetadata(metadata); err != nil {
		return Metadata{}, err
	}
	return metadata, nil
}

func decodeCreatedAt(raw json.RawMessage, createdAt *time.Time) error {
	var value string
	if err := json.Unmarshal(raw, &value); err != nil {
		return err
	}
	parsed, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		return err
	}
	if parsed.Format(time.RFC3339Nano) != value {
		return errors.New("must use canonical RFC3339Nano format")
	}
	*createdAt = parsed
	return nil
}

func decodeValidation(raw json.RawMessage, validation *ValidationRecord) error {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	token, err := decoder.Token()
	if err != nil {
		return err
	}
	if token != json.Delim('{') {
		return errors.New("validation must be a JSON object")
	}
	seen := make(map[string]bool, 4)
	for decoder.More() {
		keyToken, err := decoder.Token()
		if err != nil {
			return err
		}
		key, ok := keyToken.(string)
		if !ok {
			return errors.New("validation field name must be a string")
		}
		if seen[key] {
			return fmt.Errorf("duplicate validation field %q", key)
		}
		seen[key] = true

		var field json.RawMessage
		if err := decoder.Decode(&field); err != nil {
			return fmt.Errorf("decode validation field %q: %w", key, err)
		}
		switch key {
		case "first_bar":
			err = json.Unmarshal(field, &validation.FirstBar)
		case "last_bar":
			err = json.Unmarshal(field, &validation.LastBar)
		case "timeout_ms_per_bar":
			err = json.Unmarshal(field, &validation.TimeoutMSPerBar)
		case "status":
			err = json.Unmarshal(field, &validation.Status)
		default:
			return fmt.Errorf("unknown validation field %q", key)
		}
		if err != nil {
			return fmt.Errorf("invalid validation field %q: %w", key, err)
		}
	}
	if _, err := decoder.Token(); err != nil {
		return err
	}
	if err := ensureEOF(decoder); err != nil {
		return err
	}
	if len(seen) != 4 {
		return errors.New("validation has missing required fields")
	}
	return nil
}

func ensureEOF(decoder *json.Decoder) error {
	var extra any
	err := decoder.Decode(&extra)
	if errors.Is(err, io.EOF) {
		return nil
	}
	if err == nil {
		return errors.New("unexpected JSON value after object")
	}
	return err
}

func validateMetadata(metadata Metadata) error {
	if metadata.SchemaVersion != schemaVersion {
		return fmt.Errorf("unsupported schema version %d", metadata.SchemaVersion)
	}
	if !safeCandidateID(metadata.ID) {
		return fmt.Errorf("invalid candidate ID %q", metadata.ID)
	}
	if metadata.CreatedAt.IsZero() {
		return errors.New("created_at is required")
	}
	if !filepath.IsAbs(metadata.SourcePath) || !filepath.IsAbs(metadata.SoundsPath) {
		return errors.New("source_path and sounds_path must be absolute")
	}
	if !validSHA256(metadata.BaseSHA256) || !validSHA256(metadata.CandidateSHA256) {
		return errors.New("hashes must be full lowercase SHA-256 values")
	}
	if metadata.Provider == "" || metadata.Model == "" || metadata.Prompt == "" || metadata.Summary == "" {
		return errors.New("provider, model, prompt, and summary are required")
	}
	if metadata.Attempts != 1 && metadata.Attempts != 2 {
		return fmt.Errorf("attempts = %d, want 1 or 2", metadata.Attempts)
	}
	if metadata.Validation.FirstBar != 0 || metadata.Validation.LastBar != 15 ||
		metadata.Validation.TimeoutMSPerBar != 250 || metadata.Validation.Status != "passed" {
		return errors.New("validation must record passed bars 0 through 15 at 250 ms per bar")
	}
	return nil
}

func candidateID(createdAt time.Time, hash string) string {
	return createdAt.UTC().Format("20060102T150405.000000000Z") + "-" + hash[:12]
}

func safeCandidateID(id string) bool {
	if len(id) != len("20060102T150405.000000000Z-")+12 || strings.ContainsAny(id, "/\\") {
		return false
	}
	timestamp := id[:len("20060102T150405.000000000Z")]
	if _, err := time.Parse("20060102T150405.000000000Z", timestamp); err != nil {
		return false
	}
	_, err := hex.DecodeString(id[len(timestamp)+1:])
	return err == nil
}

func validSHA256(value string) bool {
	if len(value) != sha256.Size*2 || strings.ToLower(value) != value {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}

func sha256Hex(source []byte) string {
	sum := sha256.Sum256(source)
	return hex.EncodeToString(sum[:])
}
