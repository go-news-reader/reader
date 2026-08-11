package settings

import (
	"path/filepath"
	"testing"
)

func TestPluginsDirOrDefaultConfigured(t *testing.T) {
	s := &Settings{PluginsDir: "/custom/plugins"}
	if got := s.PluginsDirOrDefault(); got != "/custom/plugins" {
		t.Fatalf("PluginsDirOrDefault = %q, want /custom/plugins", got)
	}
}

func TestPluginsDirOrDefaultStandard(t *testing.T) {
	s := &Settings{} // no PluginsDir configured
	got := s.PluginsDirOrDefault()
	if got == "" {
		t.Skip("UserConfigDir does not resolve on this platform")
	}
	want := filepath.Join(filepath.Dir(filepath.Dir(got)), appDir, pluginsSubdir)
	if got != want {
		t.Fatalf("PluginsDirOrDefault = %q, want %q", got, want)
	}
	if filepath.Base(got) != pluginsSubdir {
		t.Fatalf("PluginsDirOrDefault base = %q, want %q", filepath.Base(got), pluginsSubdir)
	}
}

func TestDefaultPluginsDirNoConfigDir(t *testing.T) {
	// Clear every var os.UserConfigDir consults so it fails on this platform,
	// covering the "" fallback branch.
	for _, k := range []string{"HOME", "XDG_CONFIG_HOME", "AppData"} {
		t.Setenv(k, "")
	}
	if defaultPluginsDir() != "" {
		t.Skip("UserConfigDir still resolved on this platform")
	}
	// With no config dir, an unconfigured Settings resolves to "" too.
	if got := (&Settings{}).PluginsDirOrDefault(); got != "" {
		t.Fatalf("PluginsDirOrDefault = %q, want empty", got)
	}
}
