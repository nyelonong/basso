package suggest

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestApplier_AppliesValidCandidate(t *testing.T) {
	t.Parallel()

	fixture := newApplyFixture(t)
	preflighter := &recordingPreflighter{}
	applier := NewApplier(
		fixture.store,
		func(soundsPath string) (Preflighter, error) {
			if soundsPath != fixture.soundsPath {
				t.Fatalf("preflighter sounds path = %q, want %q", soundsPath, fixture.soundsPath)
			}
			return preflighter, nil
		},
		fixedApplyNow,
	)

	result, err := applier.Apply(context.Background(), fixture.candidate.Metadata.ID)
	if err != nil {
		t.Fatalf("Apply() error = %v", err)
	}

	wantBackupPath := filepath.Join(
		fixture.store.root,
		"backups",
		fixedApplyNow().UTC().Format("20060102T150405.000000000Z")+"-"+fixture.candidate.Metadata.BaseSHA256[:12]+"-"+filepath.Base(fixture.sourcePath),
	)
	if result.SourcePath != fixture.sourcePath {
		t.Errorf("SourcePath = %q, want %q", result.SourcePath, fixture.sourcePath)
	}
	if result.BackupPath != wantBackupPath {
		t.Errorf("BackupPath = %q, want %q", result.BackupPath, wantBackupPath)
	}
	if len(preflighter.calls) != 1 {
		t.Fatalf("preflight call count = %d, want 1", len(preflighter.calls))
	}
	if got := preflighter.calls[0]; got.source != string(fixture.candidate.Source) || got.firstBar != 0 || got.lastBar != 15 {
		t.Errorf("preflight call = %#v, want candidate source over bars 0..15", got)
	}

	assertFileBytes(t, fixture.sourcePath, fixture.candidate.Source)
	assertFileBytes(t, wantBackupPath, fixture.original)
	assertPermissions(t, fixture.sourcePath, 0o640)
	assertPermissions(t, wantBackupPath, 0o600)
	assertPermissions(t, fixture.store.root, 0o700)
	assertPermissions(t, filepath.Join(fixture.store.root, "backups"), 0o700)
}

func TestApplier_RefusesStaleBase(t *testing.T) {
	t.Parallel()

	fixture := newApplyFixture(t)
	manualEdit := []byte("(bpm 99)\n")
	if err := os.WriteFile(fixture.sourcePath, manualEdit, 0o640); err != nil {
		t.Fatalf("write manual edit: %v", err)
	}
	factoryCalls := 0
	applier := NewApplier(fixture.store, func(string) (Preflighter, error) {
		factoryCalls++
		return &recordingPreflighter{}, nil
	}, fixedApplyNow)

	if _, err := applier.Apply(context.Background(), fixture.candidate.Metadata.ID); err == nil {
		t.Fatal("Apply() error = nil, want stale-base refusal")
	}
	assertFileBytes(t, fixture.sourcePath, manualEdit)
	assertNoBackupDirectory(t, fixture.store.root)
	if factoryCalls != 0 {
		t.Errorf("preflighter factory call count = %d, want 0", factoryCalls)
	}
}

func TestApplier_RefusesMetadataAndCandidateHashMismatch(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		mutate func(*testing.T, applyFixture)
	}{
		{
			name: "metadata mismatch",
			mutate: func(t *testing.T, fixture applyFixture) {
				metadataPath := filepath.Join(
					fixture.store.root,
					candidateDirectory,
					fixture.candidate.Metadata.ID+".json",
				)
				metadata := readMetadataObject(t, metadataPath)
				metadata["schema_version"] = 2
				writeMetadataObject(t, metadataPath, metadata)
			},
		},
		{
			name: "candidate hash mismatch",
			mutate: func(t *testing.T, fixture applyFixture) {
				if err := os.WriteFile(fixture.candidateAt, []byte("tampered"), 0o600); err != nil {
					t.Fatalf("tamper candidate: %v", err)
				}
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			fixture := newApplyFixture(t)
			test.mutate(t, fixture)
			factoryCalls := 0
			applier := NewApplier(fixture.store, func(string) (Preflighter, error) {
				factoryCalls++
				return &recordingPreflighter{}, nil
			}, fixedApplyNow)

			if _, err := applier.Apply(context.Background(), fixture.candidate.Metadata.ID); err == nil {
				t.Fatal("Apply() error = nil, want artifact mismatch refusal")
			}
			assertFileBytes(t, fixture.sourcePath, fixture.original)
			assertNoBackupDirectory(t, fixture.store.root)
			if factoryCalls != 0 {
				t.Errorf("preflighter factory call count = %d, want 0", factoryCalls)
			}
		})
	}
}

func TestApplier_RefusesSymlinkAndWrongTypes(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		wantError string
		mutate    func(*testing.T, applyFixture)
	}{
		{
			name:      "candidate symlink",
			wantError: "candidate source must not be a symbolic link",
			mutate: func(t *testing.T, fixture applyFixture) {
				target := filepath.Join(filepath.Dir(fixture.candidateAt), "linked-candidate.fnl")
				if err := os.WriteFile(target, fixture.candidate.Source, 0o600); err != nil {
					t.Fatalf("write candidate target: %v", err)
				}
				replaceWithSymlink(t, fixture.candidateAt, target)
			},
		},
		{
			name:      "target symlink",
			wantError: "target source must not be a symbolic link",
			mutate: func(t *testing.T, fixture applyFixture) {
				target := filepath.Join(filepath.Dir(fixture.sourcePath), "linked-source.fnl")
				if err := os.WriteFile(target, fixture.original, 0o640); err != nil {
					t.Fatalf("write source target: %v", err)
				}
				replaceWithSymlink(t, fixture.sourcePath, target)
			},
		},
		{
			name:      "sounds symlink",
			wantError: "sounds directory must not be a symbolic link",
			mutate: func(t *testing.T, fixture applyFixture) {
				target := filepath.Join(filepath.Dir(fixture.soundsPath), "linked-sounds")
				if err := os.Mkdir(target, 0o700); err != nil {
					t.Fatalf("create sounds target: %v", err)
				}
				replaceWithSymlink(t, fixture.soundsPath, target)
			},
		},
		{
			name:      "candidate directory",
			wantError: "candidate source must be a regular file",
			mutate: func(t *testing.T, fixture applyFixture) {
				replaceWithDirectory(t, fixture.candidateAt)
			},
		},
		{
			name:      "target directory",
			wantError: "target source must be a regular file",
			mutate: func(t *testing.T, fixture applyFixture) {
				replaceWithDirectory(t, fixture.sourcePath)
			},
		},
		{
			name:      "sounds regular file",
			wantError: "sounds directory must be a directory",
			mutate: func(t *testing.T, fixture applyFixture) {
				if err := os.Remove(fixture.soundsPath); err != nil {
					t.Fatalf("remove sounds directory: %v", err)
				}
				if err := os.WriteFile(fixture.soundsPath, []byte("not a directory"), 0o600); err != nil {
					t.Fatalf("write sounds file: %v", err)
				}
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			fixture := newApplyFixture(t)
			test.mutate(t, fixture)
			factoryCalls := 0
			applier := NewApplier(fixture.store, func(string) (Preflighter, error) {
				factoryCalls++
				return &recordingPreflighter{}, nil
			}, fixedApplyNow)

			_, err := applier.Apply(context.Background(), fixture.candidate.Metadata.ID)
			if err == nil {
				t.Fatal("Apply() error = nil, want type refusal")
			}
			if !strings.Contains(err.Error(), test.wantError) {
				t.Errorf("Apply() error = %q, want substring %q", err, test.wantError)
			}
			assertNoBackupDirectory(t, fixture.store.root)
			if factoryCalls != 0 {
				t.Errorf("preflighter factory call count = %d, want 0", factoryCalls)
			}
		})
	}
}

func TestApplier_RepreflightsBeforeWrite(t *testing.T) {
	t.Parallel()

	fixture := newApplyFixture(t)
	preflightErr := errors.New("bar 7 is invalid")
	preflighter := &recordingPreflighter{err: preflightErr}
	applier := NewApplier(fixture.store, func(soundsPath string) (Preflighter, error) {
		if soundsPath != fixture.soundsPath {
			t.Fatalf("preflighter sounds path = %q, want %q", soundsPath, fixture.soundsPath)
		}
		return preflighter, nil
	}, fixedApplyNow)

	if _, err := applier.Apply(context.Background(), fixture.candidate.Metadata.ID); !errors.Is(err, preflightErr) {
		t.Fatalf("Apply() error = %v, want wrapped preflight error", err)
	}
	if len(preflighter.calls) != 1 {
		t.Fatalf("preflight call count = %d, want 1", len(preflighter.calls))
	}
	if got := preflighter.calls[0]; got.source != string(fixture.candidate.Source) || got.firstBar != 0 || got.lastBar != 15 {
		t.Errorf("preflight call = %#v, want candidate source over bars 0..15", got)
	}
	assertFileBytes(t, fixture.sourcePath, fixture.original)
	assertNoBackupDirectory(t, fixture.store.root)
}

func TestApplier_CreatesExactPrivateBackup(t *testing.T) {
	t.Parallel()

	fixture := newApplyFixture(t)
	applier := newSuccessfulApplier(fixture)

	result, err := applier.Apply(context.Background(), fixture.candidate.Metadata.ID)
	if err != nil {
		t.Fatalf("Apply() error = %v", err)
	}

	wantName := fixedApplyNow().UTC().Format("20060102T150405.000000000Z") +
		"-" + fixture.candidate.Metadata.BaseSHA256[:12] +
		"-" + filepath.Base(fixture.sourcePath)
	if filepath.Base(result.BackupPath) != wantName {
		t.Errorf("backup basename = %q, want %q", filepath.Base(result.BackupPath), wantName)
	}
	if filepath.Dir(result.BackupPath) != filepath.Join(fixture.store.root, backupDirectory) {
		t.Errorf("backup directory = %q, want store backup directory", filepath.Dir(result.BackupPath))
	}
	assertFileBytes(t, result.BackupPath, fixture.original)
	assertPermissions(t, result.BackupPath, 0o600)
	assertPermissions(t, filepath.Dir(result.BackupPath), 0o700)
}

func TestApplier_PreservesSourceMode(t *testing.T) {
	t.Parallel()

	fixture := newApplyFixture(t)
	if err := os.Chmod(fixture.sourcePath, 0o751); err != nil {
		t.Fatalf("chmod source fixture: %v", err)
	}

	if _, err := newSuccessfulApplier(fixture).Apply(context.Background(), fixture.candidate.Metadata.ID); err != nil {
		t.Fatalf("Apply() error = %v", err)
	}
	assertPermissions(t, fixture.sourcePath, 0o751)
}

func TestApplier_FailureBeforeRenamePreservesSource(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		failAt         failurePoint
		wantRenameCall bool
	}{
		{name: "backup creation", failAt: failBackupCreate},
		{name: "backup chmod", failAt: failBackupChmod},
		{name: "backup write", failAt: failBackupWrite},
		{name: "backup close", failAt: failBackupClose},
		{name: "temporary creation", failAt: failTempCreate},
		{name: "temporary chmod", failAt: failTempChmod},
		{name: "temporary write", failAt: failTempWrite},
		{name: "temporary sync", failAt: failTempSync},
		{name: "temporary close", failAt: failTempClose},
		{name: "rename", failAt: failRename, wantRenameCall: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			fixture := newApplyFixture(t)
			files := &failingFileOps{failAt: test.failAt}
			applier := newSuccessfulApplier(fixture)
			applier.files = files

			if _, err := applier.Apply(context.Background(), fixture.candidate.Metadata.ID); err == nil {
				t.Fatal("Apply() error = nil, want injected failure")
			}
			assertFileBytes(t, fixture.sourcePath, fixture.original)
			if got := files.renameCalls > 0; got != test.wantRenameCall {
				t.Errorf("rename attempted = %t, want %t", got, test.wantRenameCall)
			}
		})
	}
}

func TestApplier_RemovesTemporaryFileOnFailure(t *testing.T) {
	t.Parallel()

	for _, failAt := range []failurePoint{
		failTempChmod,
		failTempWrite,
		failTempSync,
		failTempClose,
		failRename,
	} {
		t.Run(string(failAt), func(t *testing.T) {
			t.Parallel()

			fixture := newApplyFixture(t)
			files := &failingFileOps{failAt: failAt}
			applier := newSuccessfulApplier(fixture)
			applier.files = files

			if _, err := applier.Apply(context.Background(), fixture.candidate.Metadata.ID); err == nil {
				t.Fatal("Apply() error = nil, want injected failure")
			}
			if len(files.tempPaths) != 1 {
				t.Fatalf("created temporary paths = %v, want exactly one", files.tempPaths)
			}
			for _, tempPath := range files.tempPaths {
				if _, err := os.Lstat(tempPath); !os.IsNotExist(err) {
					t.Errorf("temporary file %q survives failure: %v", tempPath, err)
				}
			}
			assertNoReplacementTemps(t, fixture.sourcePath)
			assertFileBytes(t, fixture.sourcePath, fixture.original)
		})
	}
}

type applyFixture struct {
	store       *Store
	sourcePath  string
	soundsPath  string
	original    []byte
	candidate   Candidate
	candidateAt string
}

func newSuccessfulApplier(fixture applyFixture) *Applier {
	return NewApplier(fixture.store, func(string) (Preflighter, error) {
		return &recordingPreflighter{}, nil
	}, fixedApplyNow)
}

func newApplyFixture(t *testing.T) applyFixture {
	t.Helper()

	directory := t.TempDir()
	sourcePath := filepath.Join(directory, "pattern.fnl")
	original := []byte("(bpm 120)\n")
	if err := os.WriteFile(sourcePath, original, 0o640); err != nil {
		t.Fatalf("write source: %v", err)
	}
	if err := os.Chmod(sourcePath, 0o640); err != nil {
		t.Fatalf("chmod source: %v", err)
	}
	soundsPath := filepath.Join(directory, "sounds")
	if err := os.Mkdir(soundsPath, 0o700); err != nil {
		t.Fatalf("create sounds directory: %v", err)
	}

	store := NewStore(filepath.Join(directory, ".basso"), fixedApplyNow)
	candidateSource := []byte("(bpm 140)\n")
	saved, err := store.Save(Candidate{
		Metadata: Metadata{
			SourcePath: sourcePath,
			SoundsPath: soundsPath,
			BaseSHA256: sha256Hex(original),
			Provider:   "openai",
			Model:      "gpt-test",
			Prompt:     "increase the tempo",
			Summary:    "Raised the tempo.",
			Attempts:   1,
			Validation: ValidationRecord{
				FirstBar:        0,
				LastBar:         15,
				TimeoutMSPerBar: 250,
				Status:          "passed",
			},
		},
		Source: candidateSource,
	})
	if err != nil {
		t.Fatalf("save candidate: %v", err)
	}

	return applyFixture{
		store:       store,
		sourcePath:  sourcePath,
		soundsPath:  soundsPath,
		original:    original,
		candidate:   saved,
		candidateAt: filepath.Join(store.root, candidateDirectory, saved.Metadata.ID+".fnl"),
	}
}

type preflightCall struct {
	source   string
	firstBar int
	lastBar  int
}

type recordingPreflighter struct {
	calls []preflightCall
	err   error
}

func (p *recordingPreflighter) Preflight(_ context.Context, source string, firstBar, lastBar int) error {
	p.calls = append(p.calls, preflightCall{
		source:   source,
		firstBar: firstBar,
		lastBar:  lastBar,
	})
	return p.err
}

func fixedApplyNow() time.Time {
	return time.Date(2026, time.July, 25, 12, 0, 0, 123456789, time.UTC)
}

func replaceWithSymlink(t *testing.T, path, target string) {
	t.Helper()

	if err := os.Remove(path); err != nil {
		t.Fatalf("remove %s: %v", path, err)
	}
	if err := os.Symlink(target, path); err != nil {
		t.Fatalf("symlink %s to %s: %v", path, target, err)
	}
}

func replaceWithDirectory(t *testing.T, path string) {
	t.Helper()

	if err := os.Remove(path); err != nil {
		t.Fatalf("remove %s: %v", path, err)
	}
	if err := os.Mkdir(path, 0o700); err != nil {
		t.Fatalf("mkdir %s: %v", path, err)
	}
}

func assertFileBytes(t *testing.T, path string, want []byte) {
	t.Helper()

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	if string(got) != string(want) {
		t.Errorf("%s bytes = %q, want %q", path, got, want)
	}
}

func assertNoBackupDirectory(t *testing.T, storeRoot string) {
	t.Helper()

	if _, err := os.Stat(filepath.Join(storeRoot, backupDirectory)); !os.IsNotExist(err) {
		t.Errorf("backup directory exists before successful preflight: %v", err)
	}
}

func assertNoReplacementTemps(t *testing.T, sourcePath string) {
	t.Helper()

	entries, err := os.ReadDir(filepath.Dir(sourcePath))
	if err != nil {
		t.Fatalf("read source directory: %v", err)
	}
	prefix := "." + filepath.Base(sourcePath) + ".basso-"
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), prefix) {
			t.Errorf("replacement temporary file survives: %s", entry.Name())
		}
	}
}

func assertPermissions(t *testing.T, path string, want os.FileMode) {
	t.Helper()

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat %s: %v", path, err)
	}
	if got := info.Mode().Perm(); got != want {
		t.Errorf("%s permissions = %#o, want %#o", path, got, want)
	}
}

type failurePoint string

const (
	failBackupCreate failurePoint = "backup-create"
	failBackupChmod  failurePoint = "backup-chmod"
	failBackupWrite  failurePoint = "backup-write"
	failBackupClose  failurePoint = "backup-close"
	failTempCreate   failurePoint = "temp-create"
	failTempChmod    failurePoint = "temp-chmod"
	failTempWrite    failurePoint = "temp-write"
	failTempSync     failurePoint = "temp-sync"
	failTempClose    failurePoint = "temp-close"
	failRename       failurePoint = "rename"
)

var errInjectedApplyFailure = errors.New("injected apply failure")

type failingFileOps struct {
	osFileOps
	failAt      failurePoint
	tempPaths   []string
	renameCalls int
}

func (f *failingFileOps) OpenFile(path string, flag int, mode os.FileMode) (applyFile, error) {
	if f.failAt == failBackupCreate {
		return nil, errInjectedApplyFailure
	}
	file, err := f.osFileOps.OpenFile(path, flag, mode)
	if err != nil {
		return nil, err
	}
	return &failingApplyFile{applyFile: file, role: "backup", failAt: f.failAt}, nil
}

func (f *failingFileOps) CreateTemp(directory, pattern string) (applyFile, error) {
	if f.failAt == failTempCreate {
		return nil, errInjectedApplyFailure
	}
	file, err := f.osFileOps.CreateTemp(directory, pattern)
	if err != nil {
		return nil, err
	}
	f.tempPaths = append(f.tempPaths, file.Name())
	return &failingApplyFile{applyFile: file, role: "temp", failAt: f.failAt}, nil
}

func (f *failingFileOps) Rename(oldPath, newPath string) error {
	f.renameCalls++
	if f.failAt == failRename {
		return errInjectedApplyFailure
	}
	return f.osFileOps.Rename(oldPath, newPath)
}

type failingApplyFile struct {
	applyFile
	role   string
	failAt failurePoint
}

func (f *failingApplyFile) Chmod(mode os.FileMode) error {
	if (f.role == "backup" && f.failAt == failBackupChmod) ||
		(f.role == "temp" && f.failAt == failTempChmod) {
		return errInjectedApplyFailure
	}
	return f.applyFile.Chmod(mode)
}

func (f *failingApplyFile) Write(data []byte) (int, error) {
	if (f.role == "backup" && f.failAt == failBackupWrite) ||
		(f.role == "temp" && f.failAt == failTempWrite) {
		return 0, errInjectedApplyFailure
	}
	return f.applyFile.Write(data)
}

func (f *failingApplyFile) Sync() error {
	if f.role == "temp" && f.failAt == failTempSync {
		return errInjectedApplyFailure
	}
	return f.applyFile.Sync()
}

func (f *failingApplyFile) Close() error {
	closeErr := f.applyFile.Close()
	if (f.role == "backup" && f.failAt == failBackupClose) ||
		(f.role == "temp" && f.failAt == failTempClose) {
		return errInjectedApplyFailure
	}
	return closeErr
}
