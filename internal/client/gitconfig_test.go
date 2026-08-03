package client

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

const testOriginConfig = "[remote \"origin\"]\n\turl = git@github.example:team/project.git\n"

func TestOriginRemoteDiscoversRepositoryFromNestedDirectory(t *testing.T) {
	root := t.TempDir()
	writeGitConfig(t, filepath.Join(root, ".git"), testOriginConfig)
	nested := filepath.Join(root, "one", "two")
	if err := os.MkdirAll(nested, 0o700); err != nil {
		t.Fatal(err)
	}

	remote, err := originRemote(nested)
	if err != nil {
		t.Fatal(err)
	}
	if remote != "git@github.example:team/project.git" {
		t.Fatalf("originRemote() = %q", remote)
	}
}

func TestOriginRemoteBoundsAncestorDiscovery(t *testing.T) {
	root := t.TempDir()
	writeGitConfig(t, filepath.Join(root, ".git"), testOriginConfig)
	cwd := root
	for depth := 0; depth <= gitDiscoveryDepth; depth++ {
		cwd = filepath.Join(cwd, "nested")
		if err := os.Mkdir(cwd, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := originRemote(cwd); err == nil {
		t.Fatal("originRemote exceeded its ancestor discovery bound")
	}
}

func TestOriginRemoteReadsValidatedLinkedWorktree(t *testing.T) {
	root := t.TempDir()
	common := filepath.Join(root, "main", ".git")
	metadata := filepath.Join(common, "worktrees", "linked")
	worktree := filepath.Join(root, "linked")
	if err := os.MkdirAll(metadata, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(worktree, 0o700); err != nil {
		t.Fatal(err)
	}
	pointer := filepath.Join(worktree, ".git")
	if err := os.WriteFile(pointer, []byte("gitdir: "+metadata+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(metadata, "commondir"), []byte("../..\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(metadata, "gitdir"), []byte(pointer+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	writeGitConfig(t, common, testOriginConfig)

	remote, err := originRemote(worktree)
	if err != nil {
		t.Fatal(err)
	}
	if remote != "git@github.example:team/project.git" {
		t.Fatalf("originRemote() = %q", remote)
	}
}

func TestOriginRemoteRejectsUnsafeMetadata(t *testing.T) {
	t.Run("missing repository", func(t *testing.T) {
		if _, err := originRemote(t.TempDir()); err == nil {
			t.Fatal("originRemote accepted a directory outside a repository")
		}
	})

	t.Run("FIFO does not block", func(t *testing.T) {
		root := t.TempDir()
		gitDir := filepath.Join(root, ".git")
		if err := os.Mkdir(gitDir, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := unix.Mkfifo(filepath.Join(gitDir, "config"), 0o600); err != nil {
			t.Fatal(err)
		}
		result := make(chan error, 1)
		go func() {
			_, err := originRemote(root)
			result <- err
		}()
		select {
		case err := <-result:
			if err == nil {
				t.Fatal("originRemote accepted a FIFO")
			}
		case <-time.After(250 * time.Millisecond):
			t.Fatal("originRemote blocked on a FIFO")
		}
	})

	t.Run("symlink", func(t *testing.T) {
		root := t.TempDir()
		outside := filepath.Join(t.TempDir(), ".git")
		writeGitConfig(t, outside, testOriginConfig)
		if err := os.Symlink(outside, filepath.Join(root, ".git")); err != nil {
			t.Fatal(err)
		}
		if _, err := originRemote(root); err == nil {
			t.Fatal("originRemote followed a .git symlink")
		}
	})

	t.Run("oversize config", func(t *testing.T) {
		root := t.TempDir()
		writeGitConfig(t, filepath.Join(root, ".git"), strings.Repeat("x", gitConfigLimit+1))
		if _, err := originRemote(root); err == nil {
			t.Fatal("originRemote accepted oversized config")
		}
	})

	t.Run("oversize pointer", func(t *testing.T) {
		root := t.TempDir()
		if err := os.WriteFile(filepath.Join(root, ".git"), []byte(strings.Repeat("x", gitPointerLimit+1)), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := originRemote(root); err == nil {
			t.Fatal("originRemote accepted oversized pointer")
		}
	})

	t.Run("relative pointer traversal", func(t *testing.T) {
		root := t.TempDir()
		if err := os.WriteFile(filepath.Join(root, ".git"), []byte("gitdir: ../../outside\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := originRemote(root); err == nil {
			t.Fatal("originRemote accepted relative pointer traversal")
		}
	})

	t.Run("unvalidated absolute pointer", func(t *testing.T) {
		root := t.TempDir()
		outside := filepath.Join(t.TempDir(), ".git", "worktrees", "fake")
		if err := os.MkdirAll(outside, 0o700); err != nil {
			t.Fatal(err)
		}
		writeGitConfig(t, filepath.Clean(filepath.Join(outside, "..", "..")), testOriginConfig)
		if err := os.WriteFile(filepath.Join(root, ".git"), []byte("gitdir: "+outside+"\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(outside, "commondir"), []byte("../..\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := originRemote(root); err == nil {
			t.Fatal("originRemote accepted an unvalidated escaping pointer")
		}
	})
}

func TestReadRegularAtRejectsDeviceAndSymlink(t *testing.T) {
	dev, err := unix.Open("/dev", unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer unix.Close(dev)
	if _, err := readRegularAt(dev, "null", gitPointerLimit); err == nil {
		t.Fatal("readRegularAt accepted a device")
	}

	directory := t.TempDir()
	if err := os.Symlink("/dev/null", filepath.Join(directory, "metadata")); err != nil {
		t.Fatal(err)
	}
	fd, err := unix.Open(directory, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer unix.Close(fd)
	if _, err := readRegularAt(fd, "metadata", gitPointerLimit); err == nil {
		t.Fatal("readRegularAt followed a symlink")
	}
}

func writeGitConfig(t *testing.T, gitDir, contents string) {
	t.Helper()
	if err := os.MkdirAll(gitDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(gitDir, "config"), []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}
}
