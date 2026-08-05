package tlsconfig

import (
	"crypto/tls"
	"testing"
)

func TestLoadServerRequiresTLS13(t *testing.T) {
	files, err := Init(testOptions(testOutput(t)))
	if err != nil {
		t.Fatalf("Init() error = %v", err)
	}
	config, err := LoadServer(files.ServerCertificate, files.ServerPrivateKey)
	if err != nil {
		t.Fatalf("LoadServer() error = %v", err)
	}
	if config.MinVersion != tls.VersionTLS13 {
		t.Fatalf("MinVersion = %d, want TLS 1.3", config.MinVersion)
	}
	if len(config.Certificates) != 1 {
		t.Fatalf("len(Certificates) = %d, want 1", len(config.Certificates))
	}
}

func TestLoadServerRejectsWrongPrivateKey(t *testing.T) {
	files, err := Init(testOptions(testOutput(t)))
	if err != nil {
		t.Fatalf("Init() error = %v", err)
	}
	if _, err := LoadServer(files.ServerCertificate, files.CAPrivateKey); err == nil {
		t.Fatal("LoadServer() unexpectedly accepted the CA private key")
	}
}
