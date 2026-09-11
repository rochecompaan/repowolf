package server

import "context"

type providerMetadata struct {
	operation   string
	provider    string
	repository  string
	inputBytes  int64
	outputBytes int64
}
type providerMetadataKey struct{}

func withProviderMetadata(ctx context.Context, operation string, inputBytes int64) context.Context {
	return context.WithValue(ctx, providerMetadataKey{}, &providerMetadata{operation: operation, inputBytes: inputBytes})
}
func providerMetadataFrom(ctx context.Context) *providerMetadata {
	metadata, _ := ctx.Value(providerMetadataKey{}).(*providerMetadata)
	return metadata
}
