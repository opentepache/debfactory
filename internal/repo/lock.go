package repo

import (
	"os"
	"path/filepath"
	"syscall"

	"debfactory/internal/report"
)

// Acquire toma el bloqueo exclusivo de <data>/lock (FR-012). Si otra
// operación lo tiene, falla de inmediato.
func Acquire(dataDir string) (release func(), err error) {
	if err := os.MkdirAll(dataDir, 0o700); err != nil {
		return nil, report.Newf(report.LockRepo, "%v", err)
	}
	f, err := os.OpenFile(filepath.Join(dataDir, "lock"), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, report.Newf(report.LockRepo, "%v", err)
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		f.Close()
		return nil, report.Newf(report.LockRepo, "otra operación está en curso sobre %s", dataDir)
	}
	return func() {
		syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
		f.Close()
	}, nil
}
