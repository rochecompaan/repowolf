package client

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

const (
	gitPointerLimit = 4096
	gitConfigLimit  = 64 << 10
)

// originRemote reads bounded local Git metadata only to produce an untrusted
// request hint. It never interprets the remote as policy or authority.
func originRemote(cwd string) (string, error) {
	gitDirectory, err := resolveGitDirectory(cwd)
	if err != nil {
		return "", err
	}
	configPath := filepath.Join(gitDirectory, "config")
	if _, err := os.Stat(configPath); err != nil {
		common, commonErr := readBounded(filepath.Join(gitDirectory, "commondir"), gitPointerLimit)
		if commonErr != nil {
			return "", commonErr
		}
		configPath = filepath.Join(resolvePath(gitDirectory, strings.TrimSpace(string(common))), "config")
	}
	contents, err := readBounded(configPath, gitConfigLimit)
	if err != nil {
		return "", err
	}
	return originURL(contents)
}

func resolveGitDirectory(cwd string) (string, error) {
	path := filepath.Join(cwd, ".git")
	info, err := os.Stat(path)
	if err != nil {
		return "", err
	}
	if info.IsDir() {
		return path, nil
	}
	contents, err := readBounded(path, gitPointerLimit)
	if err != nil {
		return "", err
	}
	line := strings.TrimSpace(string(contents))
	if !strings.HasPrefix(strings.ToLower(line), "gitdir:") {
		return "", fmt.Errorf("invalid git directory pointer")
	}
	return resolvePath(cwd, strings.TrimSpace(line[len("gitdir:"):])), nil
}

func resolvePath(base, value string) string {
	if filepath.IsAbs(value) {
		return filepath.Clean(value)
	}
	return filepath.Clean(filepath.Join(base, value))
}

func originURL(contents []byte) (string, error) {
	scanner := bufio.NewScanner(strings.NewReader(string(contents)))
	inOrigin := false
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, ";") {
			continue
		}
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			section := strings.ToLower(strings.TrimSpace(line[1 : len(line)-1]))
			inOrigin = section == `remote "origin"` || section == "remote.origin"
			continue
		}
		if !inOrigin {
			continue
		}
		key, value, found := strings.Cut(line, "=")
		if !found || !strings.EqualFold(strings.TrimSpace(key), "url") {
			continue
		}
		value = strings.TrimSpace(value)
		if strings.HasPrefix(value, `"`) {
			decoded, err := strconv.Unquote(value)
			if err != nil {
				return "", err
			}
			value = decoded
		}
		if value == "" || strings.ContainsAny(value, "\r\n\x00") {
			return "", fmt.Errorf("invalid origin URL")
		}
		return value, nil
	}
	if err := scanner.Err(); err != nil {
		return "", err
	}
	return "", fmt.Errorf("origin URL unavailable")
}

func readBounded(path string, limit int64) ([]byte, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	contents, err := io.ReadAll(io.LimitReader(file, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(contents)) > limit {
		return nil, fmt.Errorf("git metadata exceeds client limit")
	}
	return contents, nil
}
