package gitea

import (
	"fmt"
	"net"
	"net/url"
	"strings"
	"time"

	sdk "code.gitea.io/sdk/gitea"
	"github.com/rochecompaan/repowolf/internal/config"
	"github.com/rochecompaan/repowolf/internal/providerhttp"
)

const (
	operationTimeout = 2 * time.Minute
	maxResponseBytes = int64(8 << 20)
)

func New(provider config.Provider, token string) (*sdk.Client, error) {
	if provider.Kind != config.ProviderGitea {
		return nil, fmt.Errorf("construct Gitea client: provider kind must be Gitea")
	}
	if !validAPIHost(provider.APIHost) {
		return nil, fmt.Errorf("construct Gitea client: invalid apiHost")
	}
	return newClient("https://"+provider.APIHost+"/", providerhttp.Options{
		Token:            token,
		CAFile:           provider.CAFile,
		Timeout:          operationTimeout,
		MaxResponseBytes: maxResponseBytes,
	})
}

func newClient(baseURL string, options providerhttp.Options) (*sdk.Client, error) {
	parsed, err := url.Parse(baseURL)
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" || parsed.User != nil || (parsed.Path != "" && parsed.Path != "/") || parsed.RawQuery != "" || parsed.Fragment != "" {
		return nil, fmt.Errorf("construct Gitea client: invalid HTTPS base URL")
	}
	options.Authority = parsed.Host
	httpClient, err := providerhttp.New(options)
	if err != nil {
		return nil, fmt.Errorf("construct Gitea HTTP client: %w", err)
	}
	client, err := sdk.NewClient("https://"+parsed.Host+"/", sdk.SetHTTPClient(httpClient), sdk.SetGiteaVersion(""))
	if err != nil {
		return nil, fmt.Errorf("construct Gitea SDK client: %w", err)
	}
	return client, nil
}

func validAPIHost(host string) bool {
	if host == "" || len(host) > 253 || strings.ContainsAny(host, ":/@ ") {
		return false
	}
	if net.ParseIP(host) != nil {
		return true
	}
	for _, label := range strings.Split(host, ".") {
		if label == "" || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return false
		}
		for _, character := range label {
			if !(character >= 'a' && character <= 'z' || character >= 'A' && character <= 'Z' || character >= '0' && character <= '9' || character == '-') {
				return false
			}
		}
	}
	return true
}
