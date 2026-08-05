package runner

import (
	"reflect"
	"testing"
)

func TestProviderEnvironmentRemovesExactTokensAndInternalControls(t *testing.T) {
	base := []string{
		"PATH=/bin",
		"GH_TOKEN=provider-auth=preserved\nbyte-for-byte",
		"REPOWOLF_TOKEN_AGENT=service-secret",
		"REPOWOLF_INTERNAL=control",
		"XREPOWOLF_INTERNAL=ordinary",
		"REPOWOLF=ordinary",
		"EMPTY=",
		"MALFORMED",
		"GH_TOKEN_SUFFIX=ordinary",
	}
	got := ProviderEnvironment(base, []string{"REPOWOLF_TOKEN_AGENT"})
	want := []string{
		"PATH=/bin",
		"GH_TOKEN=provider-auth=preserved\nbyte-for-byte",
		"XREPOWOLF_INTERNAL=ordinary",
		"REPOWOLF=ordinary",
		"EMPTY=",
		"MALFORMED",
		"GH_TOKEN_SUFFIX=ordinary",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("environment = %#v, want %#v", got, want)
	}
	base[0] = "changed"
	if got[0] != "PATH=/bin" {
		t.Fatal("result aliases input")
	}
}

func TestProviderEnvironmentParsesNameAtFirstEquals(t *testing.T) {
	got := ProviderEnvironment([]string{"TOKEN=value=TOKEN", "OTHER=TOKEN=value"}, []string{"TOKEN"})
	want := []string{"OTHER=TOKEN=value"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("environment = %#v, want %#v", got, want)
	}
}
