// Package clientconfig loads and constructs the credential-free client transport.
package clientconfig

import (
	"encoding/base64"
	"fmt"
	"net"
	"net/url"
	"os"
	"strings"
)

const tokenPrefix = "rw1_"

// Config is immutable client-side transport configuration.
type Config struct {
	Endpoint   string
	Token      string
	CAFile     string
	ServerName string
}

// LoadEnv loads only the approved RepoWolf client environment variables.
func LoadEnv() (Config, error) {
	config := Config{
		Endpoint:   os.Getenv("REPOWOLF_ENDPOINT"),
		Token:      os.Getenv("REPOWOLF_TOKEN"),
		CAFile:     os.Getenv("REPOWOLF_CA_FILE"),
		ServerName: os.Getenv("REPOWOLF_SERVER_NAME"),
	}
	if _, err := validate(config); err != nil {
		return Config{}, err
	}
	return config, nil
}

func validate(config Config) (*url.URL, error) {
	endpoint, err := url.Parse(config.Endpoint)
	if err != nil || endpoint.Scheme != "https" || endpoint.Host == "" || endpoint.Hostname() == "" || endpoint.User != nil || (endpoint.Path != "" && endpoint.Path != "/") || endpoint.RawPath != "" || endpoint.RawQuery != "" || endpoint.Fragment != "" || endpoint.Opaque != "" {
		return nil, fmt.Errorf("REPOWOLF_ENDPOINT must be an https origin")
	}
	if !validToken(config.Token) {
		return nil, fmt.Errorf("REPOWOLF_TOKEN has an invalid format")
	}
	if config.ServerName != "" && !validServerName(config.ServerName) {
		return nil, fmt.Errorf("REPOWOLF_SERVER_NAME is invalid")
	}
	return endpoint, nil
}

func validServerName(value string) bool {
	if net.ParseIP(value) != nil || value == "" || len(value) > 253 || strings.HasPrefix(value, ".") || strings.HasSuffix(value, ".") {
		return net.ParseIP(value) != nil
	}
	for _, label := range strings.Split(value, ".") {
		if label == "" || label[0] == '-' || label[len(label)-1] == '-' {
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

func validToken(token string) bool {
	if len(token) != len(tokenPrefix)+43 || !strings.HasPrefix(token, tokenPrefix) {
		return false
	}
	encoded := token[len(tokenPrefix):]
	decoded, err := base64.RawURLEncoding.Strict().DecodeString(encoded)
	return err == nil && len(decoded) == 32 && base64.RawURLEncoding.EncodeToString(decoded) == encoded
}
