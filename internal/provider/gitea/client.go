package gitea

import (
	"fmt"
	"io"
	"net/http"
	"net/url"
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
	if !config.ValidProviderHost(provider.APIHost) {
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
	httpClient.Transport = &errorResponseRedactingTransport{base: httpClient.Transport}
	client, err := sdk.NewClient("https://"+parsed.Host+"/", sdk.SetHTTPClient(httpClient), sdk.SetGiteaVersion(""))
	if err != nil {
		return nil, fmt.Errorf("construct Gitea SDK client: %w", err)
	}
	return client, nil
}

type errorResponseRedactingTransport struct {
	base http.RoundTripper
}

func (transport *errorResponseRedactingTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	response, err := transport.base.RoundTrip(request)
	if err != nil || response == nil || response.StatusCode/100 == 2 {
		return response, err
	}
	if response.Body != nil {
		_, readErr := io.Copy(io.Discard, response.Body)
		closeErr := response.Body.Close()
		if readErr != nil {
			return nil, &errorResponseBodyReadError{cause: readErr}
		}
		if closeErr != nil {
			return nil, &errorResponseBodyReadError{cause: closeErr}
		}
	}
	response.Header = response.Header.Clone()
	response.Header.Del("Content-Length")
	response.ContentLength = 0
	response.Body = http.NoBody
	return response, nil
}

type errorResponseBodyReadError struct {
	cause error
}

func (err *errorResponseBodyReadError) Error() string {
	return "provider HTTP error response rejected"
}

func (err *errorResponseBodyReadError) Unwrap() error {
	return err.cause
}
