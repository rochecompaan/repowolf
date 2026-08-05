package config

import "time"

const (
	defaultSSHPort          = 22
	maxMessageBytes         = 1 << 20
	maxStreamChunkBytes     = 64 << 10
	maxPushPrefixBytes      = 1 << 20
	defaultGitBytesPerLimit = 8 << 30
)

func defaultLimits() Limits {
	return Limits{
		MaxConcurrentRequests:             8,
		MaxConcurrentRequestsPerPrincipal: 4,
		MaxMessageBytes:                   maxMessageBytes,
		MaxStreamChunkBytes:               maxStreamChunkBytes,
		MaxPushPrefixBytes:                maxPushPrefixBytes,
		MaxGitBytesPerDirection:           defaultGitBytesPerLimit,
		InitialStreamTimeout:              5 * time.Second,
		OperationTimeout:                  10 * time.Minute,
		IdleStreamTimeout:                 2 * time.Minute,
	}
}

func defaultPushPolicy() PushPolicy {
	return PushPolicy{DenyRefs: []string{"refs/heads/main"}}
}

func copyStrings(values []string) []string {
	return append([]string(nil), values...)
}
