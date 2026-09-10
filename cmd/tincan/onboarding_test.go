package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestNamedRuntimeIdentityPersistence(t *testing.T) {
	old := cliIdentity
	t.Cleanup(func() { cliIdentity = old })
	root := t.TempDir()
	t.Setenv("TINCAN_CONFIG", filepath.Join(root, "config.json"))
	t.Setenv("TINCAN_TOKEN", "unrelated-global-credential")
	t.Setenv("TINCAN_SERVER", "")
	cliIdentity = "first-runtime"
	if load().Token != "" {
		t.Fatal("new runtime inherited global identity")
	}
	first := Config{Server: "https://tincan.example", Token: "first-private-credential"}
	if err := save(first); err != nil {
		t.Fatal(err)
	}
	if got := load(); got != first {
		t.Fatal("runtime failed to resume")
	}
	info, err := os.Stat(configPath())
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatal("credential file not private", err)
	}
	cliIdentity = "second-runtime"
	if load().Token != "" {
		t.Fatal("second runtime reused first identity")
	}
	if err := save(Config{Server: first.Server, Token: "second-private-credential"}); err != nil {
		t.Fatal(err)
	}
	cliIdentity = "first-runtime"
	if got := load(); got != first {
		t.Fatal("second runtime overwrote first identity")
	}
	cliIdentity = ""
	if load().Token != "unrelated-global-credential" {
		t.Fatal("legacy global configuration changed")
	}
}
