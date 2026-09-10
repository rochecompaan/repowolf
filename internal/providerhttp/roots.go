package providerhttp

import (
	"crypto/x509"
	"fmt"
	"io"
	"os"
)

const maxCAFileBytes int64 = 1 << 20

func loadRootCAs(path string) (*x509.CertPool, error) {
	roots, err := x509.SystemCertPool()
	if err != nil || roots == nil {
		return nil, fmt.Errorf("%w: load system certificate pool", ErrTrustRoots)
	}
	if path == "" {
		return roots, nil
	}
	info, err := os.Lstat(path)
	if err != nil {
		return nil, fmt.Errorf("%w: inspect %q: %v", ErrTrustRoots, path, err)
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("%w: %q is not a regular file", ErrTrustRoots, path)
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("%w: open %q: %v", ErrTrustRoots, path, err)
	}
	defer file.Close()
	openedInfo, err := file.Stat()
	if err != nil || !openedInfo.Mode().IsRegular() {
		return nil, fmt.Errorf("%w: inspect opened file %q", ErrTrustRoots, path)
	}
	contents, err := io.ReadAll(io.LimitReader(file, maxCAFileBytes+1))
	if err != nil {
		return nil, fmt.Errorf("%w: read %q: %v", ErrTrustRoots, path, err)
	}
	if len(contents) == 0 {
		return nil, fmt.Errorf("%w: %q is empty", ErrTrustRoots, path)
	}
	if int64(len(contents)) > maxCAFileBytes {
		return nil, fmt.Errorf("%w: %q exceeds 1 MiB", ErrTrustRoots, path)
	}
	if !roots.AppendCertsFromPEM(contents) {
		return nil, fmt.Errorf("%w: %q contains no valid certificates", ErrTrustRoots, path)
	}
	return roots, nil
}
