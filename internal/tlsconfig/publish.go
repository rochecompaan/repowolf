package tlsconfig

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sync/atomic"
)

var temporarySequence atomic.Uint64

type stagedFile struct {
	temporary string
	final     string
	info      fs.FileInfo
}

type publishedFile struct {
	path string
	info fs.FileInfo
}

func publishAll(dir string, specs []fileSpec) error {
	staged, err := stageAll(specs)
	if err != nil {
		return errors.Join(err, cleanupInvocation(dir, staged, nil))
	}

	published := make([]publishedFile, 0, len(staged))
	for _, file := range staged {
		if err := os.Link(file.temporary, file.final); err != nil {
			return errors.Join(err, cleanupInvocation(dir, staged, published))
		}
		published = append(published, publishedFile{path: file.final, info: file.info})
		if err := os.Remove(file.temporary); err != nil {
			return errors.Join(err, cleanupInvocation(dir, staged, published))
		}
		if err := syncDirectory(dir); err != nil {
			return errors.Join(err, cleanupInvocation(dir, staged, published))
		}
	}
	return nil
}

func stageAll(specs []fileSpec) ([]stagedFile, error) {
	staged := make([]stagedFile, 0, len(specs))
	for _, spec := range specs {
		file, err := stageOne(spec)
		if file.temporary != "" {
			staged = append(staged, file)
		}
		if err != nil {
			return staged, err
		}
	}
	return staged, nil
}

func stageOne(spec fileSpec) (stagedFile, error) {
	temporary := filepath.Join(filepath.Dir(spec.path), fmt.Sprintf(".%s.repowolf-%d-%d", filepath.Base(spec.path), os.Getpid(), temporarySequence.Add(1)))
	file, err := os.OpenFile(temporary, os.O_WRONLY|os.O_CREATE|os.O_EXCL, spec.mode)
	if err != nil {
		return stagedFile{}, err
	}
	staged := stagedFile{temporary: temporary, final: spec.path}
	staged.info, err = file.Stat()
	if err == nil {
		var written int64
		written, err = io.Copy(file, bytes.NewReader(spec.contents))
		if err == nil && written != int64(len(spec.contents)) {
			err = io.ErrShortWrite
		}
	}
	if err == nil {
		err = file.Sync()
	}
	return staged, errors.Join(err, file.Close())
}

func cleanupInvocation(dir string, staged []stagedFile, published []publishedFile) error {
	return errors.Join(cleanupPublished(published), cleanupStaged(staged), syncDirectory(dir))
}

func cleanupPublished(published []publishedFile) error {
	var cleanupErrors []error
	for _, file := range published {
		cleanupErrors = append(cleanupErrors, removeIfSame(file.path, file.info))
	}
	return errors.Join(cleanupErrors...)
}

func cleanupStaged(staged []stagedFile) error {
	var cleanupErrors []error
	for _, file := range staged {
		cleanupErrors = append(cleanupErrors, removeIfSame(file.temporary, file.info))
	}
	return errors.Join(cleanupErrors...)
}

func removeIfSame(path string, stagedInfo fs.FileInfo) error {
	if stagedInfo == nil {
		return nil
	}
	current, err := os.Lstat(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil || !os.SameFile(current, stagedInfo) {
		return err
	}
	return os.Remove(path)
}

func syncDirectory(dir string) error {
	directory, err := os.Open(dir)
	if err != nil {
		return err
	}
	return errors.Join(directory.Sync(), directory.Close())
}
