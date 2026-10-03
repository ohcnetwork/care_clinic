package atomicfile

import (
	"os"
	"path/filepath"
)

func Write(path string, data []byte, mode os.FileMode) error {
	return write(path, data, mode, false)
}

// WritePrivate publishes an owner-restricted file, including a protected DACL
// on Windows where Unix permission bits alone do not restrict access.
func WritePrivate(path string, data []byte) error {
	return write(path, data, 0o600, true)
}

func write(path string, data []byte, mode os.FileMode, private bool) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+"-*")
	if err != nil {
		return err
	}
	defer func() { _ = os.Remove(f.Name()) }()
	if err := f.Chmod(mode); err != nil {
		_ = f.Close()
		return err
	}
	if private {
		if err := restrictPrivateFile(f.Name()); err != nil {
			_ = f.Close()
			return err
		}
	}
	if _, err := f.Write(data); err != nil {
		_ = f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		_ = f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return replace(f.Name(), path)
}
