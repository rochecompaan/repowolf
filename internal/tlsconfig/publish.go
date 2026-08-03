package tlsconfig

import (
	"bytes"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"

	"golang.org/x/sys/unix"
)

var (
	renameNoReplace = unix.Renameat2
	removeStaging   = os.RemoveAll
	syncParent      = unix.Fsync
)

func publishDirectory(output string, specs []fileSpec) (err error) {
	parent := filepath.Dir(output)
	parentFD, err := unix.Open(parent, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC, 0)
	if err != nil {
		return err
	}

	staging, err := os.MkdirTemp(parent, ".repowolf-cert-")
	if err != nil {
		return errors.Join(err, unix.Close(parentFD))
	}
	published := false
	defer func() {
		if !published {
			err = errors.Join(err, removeStaging(staging))
		}
		err = errors.Join(err, unix.Close(parentFD))
	}()

	if err := writeStagedFiles(staging, specs); err != nil {
		return err
	}
	if err := syncDirectory(staging); err != nil {
		return err
	}
	if err := renameNoReplace(parentFD, filepath.Base(staging), parentFD, filepath.Base(output), unix.RENAME_NOREPLACE); err != nil {
		if errors.Is(err, unix.EEXIST) {
			return errors.Join(fs.ErrExist, err)
		}
		return err
	}
	published = true
	return syncParent(parentFD)
}

func writeStagedFiles(staging string, specs []fileSpec) error {
	for _, spec := range specs {
		if err := writeStagedFile(filepath.Join(staging, filepath.Base(spec.path)), spec.contents, spec.mode); err != nil {
			return err
		}
	}
	return nil
}

func writeStagedFile(path string, contents []byte, mode fs.FileMode) error {
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, mode)
	if err != nil {
		return err
	}
	var writeErr error
	written, writeErr := io.Copy(file, bytes.NewReader(contents))
	if writeErr == nil && written != int64(len(contents)) {
		writeErr = io.ErrShortWrite
	}
	if writeErr == nil {
		writeErr = file.Sync()
	}
	return errors.Join(writeErr, file.Close())
}

func syncDirectory(path string) error {
	directory, err := os.Open(path)
	if err != nil {
		return err
	}
	return errors.Join(directory.Sync(), directory.Close())
}
