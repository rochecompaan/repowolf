package clientconfig

import (
	"context"
	"fmt"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
)

const (
	messageLimitBytes = 1 << 20
	dialTimeout       = 10 * time.Second
)

type bearerCredentials struct{ token string }

func (credentials bearerCredentials) GetRequestMetadata(context.Context, ...string) (map[string]string, error) {
	return map[string]string{"authorization": "Bearer " + credentials.token}, nil
}

func (bearerCredentials) RequireTransportSecurity() bool { return true }

// Dial establishes a blocking TLS gRPC connection with bounded message sizes.
func Dial(ctx context.Context, config Config) (*grpc.ClientConn, error) {
	endpoint, err := validate(config)
	if err != nil {
		return nil, err
	}
	tlsSettings, err := tlsConfig(config)
	if err != nil {
		return nil, err
	}
	dialContext, cancel := context.WithTimeout(ctx, dialTimeout)
	defer cancel()
	connection, err := grpc.DialContext(
		dialContext,
		endpoint.Host,
		grpc.WithBlock(),
		grpc.WithTransportCredentials(credentials.NewTLS(tlsSettings)),
		grpc.WithPerRPCCredentials(bearerCredentials{token: config.Token}),
		grpc.WithDefaultCallOptions(
			grpc.MaxCallRecvMsgSize(messageLimitBytes),
			grpc.MaxCallSendMsgSize(messageLimitBytes),
		),
	)
	if err != nil {
		return nil, fmt.Errorf("connect to RepoWolf: %w", err)
	}
	return connection, nil
}
