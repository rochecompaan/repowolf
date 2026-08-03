package gitservice

import "errors"

const maxChunkBytes = 64 << 10

var errChunkLimit = errors.New("git frame chunk limit exceeded")

func validChunk(data []byte) error {
	if len(data) > maxChunkBytes {
		return errChunkLimit
	}
	return nil
}
