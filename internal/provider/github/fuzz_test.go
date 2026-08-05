package github

import (
	"testing"

	repowolfv1 "github.com/rochecompaan/repowolf/gen/repowolf/v1"
	"google.golang.org/protobuf/proto"
)

func FuzzValidateGitHubRequest(f *testing.F) {
	for _, test := range validOperations() {
		data, err := proto.Marshal(test.request)
		if err != nil {
			f.Fatal(err)
		}
		f.Add(data)
	}
	f.Add([]byte{})
	f.Add([]byte{0xf8, 0x07, 0x01}) // unknown field 127 only
	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) > maximumResponseBytes {
			return
		}
		request := &repowolfv1.GitHubRequest{}
		if proto.Unmarshal(data, request) != nil {
			return
		}
		_ = ValidateGitHubRequest(request)
	})
}
