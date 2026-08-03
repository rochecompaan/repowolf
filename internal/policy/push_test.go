package policy

import (
	"errors"
	"path/filepath"
	"testing"

	"github.com/rochecompaan/repowolf/internal/config"
)

func TestValidatePushAllowsSyntacticallyValidUpdates(t *testing.T) {
	policy := config.PushPolicy{MaxRefUpdates: 4}
	updates := []Update{
		{OldOID: zeros(40), NewOID: oid('1', 40), Ref: "refs/heads/feature/new"},
		{OldOID: oid('2', 64), NewOID: oid('3', 64), Ref: "refs/tags/v1.0.0"},
	}

	if err := ValidatePush(policy, updates); err != nil {
		t.Fatalf("ValidatePush() error = %v", err)
	}
}

func TestValidatePushRejectsDefaultMainFromDecodedRepositoryPolicy(t *testing.T) {
	cfg, err := config.LoadFile(filepath.Join("..", "config", "testdata", "valid.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	policy := cfg.Repositories["clubhouse"].Git

	err = ValidatePush(policy, []Update{{OldOID: oid('1', 40), NewOID: oid('2', 40), Ref: "refs/heads/main"}})
	if !errors.Is(err, ErrRefPolicy) {
		t.Fatalf("ValidatePush() error = %v, want ErrRefPolicy", err)
	}
}

func TestValidatePushUsesExactDeniedRefs(t *testing.T) {
	policy := config.PushPolicy{DenyRefs: []string{"refs/heads/release"}, MaxRefUpdates: 2}
	denied := Update{OldOID: oid('1', 40), NewOID: oid('2', 40), Ref: "refs/heads/release"}
	allowed := Update{OldOID: oid('1', 40), NewOID: oid('2', 40), Ref: "refs/heads/release-candidate"}

	if err := ValidatePush(policy, []Update{denied}); !errors.Is(err, ErrRefPolicy) {
		t.Fatalf("denied ref error = %v, want ErrRefPolicy", err)
	}
	if err := ValidatePush(policy, []Update{allowed}); err != nil {
		t.Fatalf("non-exact ref error = %v", err)
	}
}

func TestValidatePushRejectsUpdatesAboveLimit(t *testing.T) {
	policy := config.PushPolicy{MaxRefUpdates: 1}
	updates := []Update{
		{OldOID: oid('1', 40), NewOID: oid('2', 40), Ref: "refs/heads/a"},
		{OldOID: oid('3', 40), NewOID: oid('4', 40), Ref: "refs/heads/b"},
	}

	err := ValidatePush(policy, updates)
	if !errors.Is(err, ErrRefPolicy) {
		t.Fatalf("ValidatePush() error = %v, want ErrRefPolicy", err)
	}
}

func TestValidatePushRejectsDeletesForSHA1AndSHA256(t *testing.T) {
	policy := config.PushPolicy{DenyDeletes: true, MaxRefUpdates: 2}
	for _, length := range []int{40, 64} {
		err := ValidatePush(policy, []Update{{
			OldOID: oid('1', length), NewOID: zeros(length), Ref: "refs/heads/obsolete",
		}})
		if !errors.Is(err, ErrRefPolicy) {
			t.Fatalf("SHA-%d delete error = %v, want ErrRefPolicy", length*4, err)
		}
	}
}

func TestValidatePushAllowsDeletesForSHA1AndSHA256(t *testing.T) {
	policy := config.PushPolicy{DenyDeletes: false, MaxRefUpdates: 2}
	for _, length := range []int{40, 64} {
		err := ValidatePush(policy, []Update{{
			OldOID: oid('1', length), NewOID: zeros(length), Ref: "refs/heads/obsolete",
		}})
		if err != nil {
			t.Fatalf("SHA-%d delete error = %v", length*4, err)
		}
	}
}

func TestValidatePushRejectsMalformedRefsAndObjectIDs(t *testing.T) {
	policy := config.PushPolicy{MaxRefUpdates: 8}
	tests := []struct {
		name   string
		update Update
	}{
		{"missing refs prefix", Update{OldOID: oid('1', 40), NewOID: oid('2', 40), Ref: "main"}},
		{"invalid ref component", Update{OldOID: oid('1', 40), NewOID: oid('2', 40), Ref: "refs/heads/bad..name"}},
		{"lock ref component", Update{OldOID: oid('1', 40), NewOID: oid('2', 40), Ref: "refs/heads/foo.lock/bar"}},
		{"empty old object ID", Update{OldOID: "", NewOID: oid('2', 40), Ref: "refs/heads/topic"}},
		{"invalid object ID character", Update{OldOID: oid('g', 40), NewOID: oid('2', 40), Ref: "refs/heads/topic"}},
		{"short object ID", Update{OldOID: oid('1', 39), NewOID: oid('2', 40), Ref: "refs/heads/topic"}},
		{"mismatched object ID formats", Update{OldOID: oid('1', 40), NewOID: oid('2', 64), Ref: "refs/heads/topic"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := ValidatePush(policy, []Update{test.update})
			if !errors.Is(err, ErrRefPolicy) {
				t.Fatalf("ValidatePush() error = %v, want ErrRefPolicy", err)
			}
		})
	}
}

func zeros(length int) string { return oid('0', length) }

func oid(character byte, length int) string {
	return repeat(character, length)
}

func repeat(character byte, length int) string {
	bytes := make([]byte, length)
	for index := range bytes {
		bytes[index] = character
	}
	return string(bytes)
}
