package suggest

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"
)

const backupDirectory = "backups"

// ApplyResult identifies the source replaced by Apply and its exact backup.
type ApplyResult struct {
	SourcePath string
	BackupPath string
}

// PreflighterFactory resolves the validator for a candidate's recorded sound
// inventory.
type PreflighterFactory func(soundsPath string) (Preflighter, error)

// Applier verifies and atomically installs saved candidates.
type Applier struct {
	store          *Store
	newPreflighter PreflighterFactory
	now            func() time.Time
	files          fileOps
}

// NewApplier creates an applier for one candidate store.
func NewApplier(store *Store, newPreflighter PreflighterFactory, now func() time.Time) *Applier {
	if now == nil {
		now = time.Now
	}
	return &Applier{
		store:          store,
		newPreflighter: newPreflighter,
		now:            now,
		files:          osFileOps{},
	}
}

// Apply verifies a saved candidate and its unchanged base, re-preflights the
// candidate, creates an exact private backup, and atomically replaces the
// source.
func (a *Applier) Apply(ctx context.Context, id string) (ApplyResult, error) {
	if a == nil {
		return ApplyResult{}, errors.New("applier is nil")
	}
	if a.store == nil {
		return ApplyResult{}, errors.New("candidate store is nil")
	}
	if a.newPreflighter == nil {
		return ApplyResult{}, errors.New("preflighter factory is nil")
	}

	if !safeCandidateID(id) {
		return ApplyResult{}, fmt.Errorf("invalid candidate ID %q", id)
	}
	candidatePath := filepath.Join(a.store.root, candidateDirectory, id+".fnl")
	if _, err := requireRegularFile(a.files, candidatePath, "candidate source"); err != nil {
		return ApplyResult{}, err
	}
	candidate, err := a.store.Load(id)
	if err != nil {
		return ApplyResult{}, fmt.Errorf("load candidate: %w", err)
	}

	sourceInfo, err := requireRegularFile(a.files, candidate.Metadata.SourcePath, "target source")
	if err != nil {
		return ApplyResult{}, err
	}
	if err := requireDirectory(a.files, candidate.Metadata.SoundsPath, "sounds directory"); err != nil {
		return ApplyResult{}, err
	}

	original, err := a.files.ReadFile(candidate.Metadata.SourcePath)
	if err != nil {
		return ApplyResult{}, fmt.Errorf("read target source: %w", err)
	}
	if sha256Hex(original) != candidate.Metadata.BaseSHA256 {
		return ApplyResult{}, errors.New("target source hash does not match candidate base hash")
	}

	preflighter, err := a.newPreflighter(candidate.Metadata.SoundsPath)
	if err != nil {
		return ApplyResult{}, fmt.Errorf("create candidate preflighter: %w", err)
	}
	if preflighter == nil {
		return ApplyResult{}, errors.New("preflighter factory returned nil")
	}
	if err := preflighter.Preflight(ctx, string(candidate.Source), 0, 15); err != nil {
		return ApplyResult{}, fmt.Errorf("preflight candidate: %w", err)
	}

	sourceInfo, err = requireRegularFile(a.files, candidate.Metadata.SourcePath, "target source")
	if err != nil {
		return ApplyResult{}, err
	}
	original, err = a.files.ReadFile(candidate.Metadata.SourcePath)
	if err != nil {
		return ApplyResult{}, fmt.Errorf("read target source after preflight: %w", err)
	}
	if sha256Hex(original) != candidate.Metadata.BaseSHA256 {
		return ApplyResult{}, errors.New("target source changed during candidate preflight")
	}

	backupsPath := filepath.Join(a.store.root, backupDirectory)
	if err := makeApplyDirectory(a.files, a.store.root); err != nil {
		return ApplyResult{}, err
	}
	if err := makeApplyDirectory(a.files, backupsPath); err != nil {
		return ApplyResult{}, err
	}
	backupPath := filepath.Join(
		backupsPath,
		a.now().UTC().Format("20060102T150405.000000000Z")+"-"+
			candidate.Metadata.BaseSHA256[:12]+"-"+filepath.Base(candidate.Metadata.SourcePath),
	)
	if err := createBackup(a.files, backupPath, original); err != nil {
		return ApplyResult{}, err
	}

	if err := replaceAtomically(
		a.files,
		candidate.Metadata.SourcePath,
		candidate.Source,
		sourceInfo.Mode().Perm(),
	); err != nil {
		return ApplyResult{}, err
	}
	return ApplyResult{
		SourcePath: candidate.Metadata.SourcePath,
		BackupPath: backupPath,
	}, nil
}

type applyFile interface {
	Name() string
	Write([]byte) (int, error)
	Chmod(os.FileMode) error
	Sync() error
	Close() error
}

type fileOps interface {
	Lstat(string) (os.FileInfo, error)
	ReadFile(string) ([]byte, error)
	MkdirAll(string, os.FileMode) error
	Chmod(string, os.FileMode) error
	OpenFile(string, int, os.FileMode) (applyFile, error)
	CreateTemp(string, string) (applyFile, error)
	Remove(string) error
	Rename(string, string) error
}

type osFileOps struct{}

func (osFileOps) Lstat(path string) (os.FileInfo, error) {
	return os.Lstat(path)
}

func (osFileOps) ReadFile(path string) ([]byte, error) {
	return os.ReadFile(path)
}

func (osFileOps) MkdirAll(path string, mode os.FileMode) error {
	return os.MkdirAll(path, mode)
}

func (osFileOps) Chmod(path string, mode os.FileMode) error {
	return os.Chmod(path, mode)
}

func (osFileOps) OpenFile(path string, flag int, mode os.FileMode) (applyFile, error) {
	return os.OpenFile(path, flag, mode)
}

func (osFileOps) CreateTemp(directory, pattern string) (applyFile, error) {
	return os.CreateTemp(directory, pattern)
}

func (osFileOps) Remove(path string) error {
	return os.Remove(path)
}

func (osFileOps) Rename(oldPath, newPath string) error {
	return os.Rename(oldPath, newPath)
}

func requireRegularFile(files fileOps, path, label string) (os.FileInfo, error) {
	info, err := files.Lstat(path)
	if err != nil {
		return nil, fmt.Errorf("inspect %s: %w", label, err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return nil, fmt.Errorf("%s must not be a symbolic link", label)
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("%s must be a regular file", label)
	}
	return info, nil
}

func requireDirectory(files fileOps, path, label string) error {
	info, err := files.Lstat(path)
	if err != nil {
		return fmt.Errorf("inspect %s: %w", label, err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("%s must not be a symbolic link", label)
	}
	if !info.IsDir() {
		return fmt.Errorf("%s must be a directory", label)
	}
	return nil
}

func makeApplyDirectory(files fileOps, path string) error {
	if err := files.MkdirAll(path, directoryMode); err != nil {
		return fmt.Errorf("create backup directory: %w", err)
	}
	if err := files.Chmod(path, directoryMode); err != nil {
		return fmt.Errorf("set backup directory permissions: %w", err)
	}
	return nil
}

func createBackup(files fileOps, path string, original []byte) error {
	backup, err := files.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, fileMode)
	if err != nil {
		return fmt.Errorf("create source backup: %w", err)
	}
	if err := backup.Chmod(fileMode); err != nil {
		return cleanupCreatedFile(files, backup, path, fmt.Errorf("set source backup permissions: %w", err), false)
	}
	if err := writeApplyFile(backup, original); err != nil {
		return cleanupCreatedFile(files, backup, path, fmt.Errorf("write source backup: %w", err), false)
	}
	if err := backup.Close(); err != nil {
		return cleanupCreatedFile(files, backup, path, fmt.Errorf("close source backup: %w", err), true)
	}
	return nil
}

func replaceAtomically(files fileOps, sourcePath string, candidate []byte, mode os.FileMode) error {
	temp, err := files.CreateTemp(filepath.Dir(sourcePath), "."+filepath.Base(sourcePath)+".basso-*")
	if err != nil {
		return fmt.Errorf("create replacement temporary file: %w", err)
	}
	tempPath := temp.Name()

	if err := temp.Chmod(mode); err != nil {
		return cleanupCreatedFile(files, temp, tempPath, fmt.Errorf("set replacement permissions: %w", err), false)
	}
	if err := writeApplyFile(temp, candidate); err != nil {
		return cleanupCreatedFile(files, temp, tempPath, fmt.Errorf("write replacement: %w", err), false)
	}
	if err := temp.Sync(); err != nil {
		return cleanupCreatedFile(files, temp, tempPath, fmt.Errorf("sync replacement: %w", err), false)
	}
	if err := temp.Close(); err != nil {
		return cleanupCreatedFile(files, temp, tempPath, fmt.Errorf("close replacement: %w", err), true)
	}
	if err := files.Rename(tempPath, sourcePath); err != nil {
		return cleanupCreatedFile(files, temp, tempPath, fmt.Errorf("rename replacement: %w", err), true)
	}
	return nil
}

func writeApplyFile(file applyFile, data []byte) error {
	written, err := file.Write(data)
	if err != nil {
		return err
	}
	if written != len(data) {
		return io.ErrShortWrite
	}
	return nil
}

func cleanupCreatedFile(files fileOps, file applyFile, path string, operationErr error, alreadyClosed bool) error {
	errs := []error{operationErr}
	if !alreadyClosed {
		if err := file.Close(); err != nil {
			errs = append(errs, fmt.Errorf("close failed file: %w", err))
		}
	}
	if err := files.Remove(path); err != nil && !os.IsNotExist(err) {
		errs = append(errs, fmt.Errorf("remove failed file: %w", err))
	}
	return errors.Join(errs...)
}
