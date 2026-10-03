package config

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

var ollama = ProviderConfig{ID: "ollama", DisplayName: "Ollama (local)",
	BaseURL: "http://127.0.0.1:11434", DefaultModel: "llama3.1:8b", Enabled: true}

func loadTemp(t *testing.T) (*TOMLStore, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "llmctl", "config.toml")
	s, err := LoadTOML(path)
	if err != nil {
		t.Fatalf("LoadTOML: %v", err)
	}
	return s, path
}

func TestMissingFileIsAnEmptyConfig(t *testing.T) {
	s, _ := loadTemp(t)
	if got := s.Providers(); len(got) != 0 {
		t.Errorf("Providers = %+v, want none", got)
	}
	if _, err := s.GetProviderConfig("ollama"); !errors.Is(err, ErrNotConfigured) {
		t.Errorf("Get on empty config: got %v, want ErrNotConfigured", err)
	}
}

func TestProvidersSurviveReload(t *testing.T) {
	s, path := loadTemp(t)
	anthropic := ProviderConfig{ID: "anthropic", DisplayName: "Anthropic", DefaultModel: "claude-haiku-4-5"}
	for _, cfg := range []ProviderConfig{ollama, anthropic} {
		if err := s.SetProviderConfig(cfg); err != nil {
			t.Fatalf("Set %s: %v", cfg.ID, err)
		}
	}

	again, err := LoadTOML(path)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	got, err := again.GetProviderConfig("ollama")
	if err != nil || got != ollama {
		t.Errorf("ollama after reload = %+v, %v; want %+v", got, err, ollama)
	}
	list := again.Providers()
	if len(list) != 2 || list[0] != anthropic || list[1] != ollama {
		t.Errorf("Providers = %+v, want anthropic then ollama", list)
	}

	if err := again.DeleteProvider("anthropic"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if err := again.DeleteProvider("anthropic"); !errors.Is(err, ErrNotConfigured) {
		t.Errorf("second Delete: got %v, want ErrNotConfigured", err)
	}
	final, _ := LoadTOML(path)
	if list := final.Providers(); len(list) != 1 || list[0].ID != "ollama" {
		t.Errorf("after delete and reload: %+v", list)
	}
}

func TestSavedFileHoldsNoExtractionSettingsOrTempFiles(t *testing.T) {
	s, path := loadTemp(t)
	if err := s.SetProviderConfig(ollama); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	if !strings.Contains(text, "[providers.ollama]") || !strings.Contains(text, `default_model = "llama3.1:8b"`) {
		t.Errorf("unexpected file contents:\n%s", text)
	}
	if strings.Contains(text, "extraction") {
		t.Errorf("config must not carry a global extraction setting:\n%s", text)
	}
	entries, _ := os.ReadDir(filepath.Dir(path))
	if len(entries) != 1 {
		t.Errorf("config directory holds %d entries, want only config.toml", len(entries))
	}
}

func TestRejectsInvalidProviders(t *testing.T) {
	s, _ := loadTemp(t)
	bad := map[string]ProviderConfig{
		"empty ID":            {},
		"upper-case ID":       {ID: "Ollama"},
		"ID with a dot":       {ID: "a.b"},
		"control in name":     {ID: "x", DisplayName: "a\x1b[2J"},
		"overlong model":      {ID: "x", DefaultModel: strings.Repeat("m", maxFieldBytes+1)},
		"relative URL":        {ID: "x", BaseURL: "api.example.com/v1"},
		"plain http remote":   {ID: "x", BaseURL: "http://192.168.1.20:11434"},
		"credentials in URL":  {ID: "x", BaseURL: "https://user:key@example.com"},
		"query string in URL": {ID: "x", BaseURL: "https://example.com/v1?key=abc"},
		"other scheme":        {ID: "x", BaseURL: "ftp://example.com"},
	}
	for name, cfg := range bad {
		if err := s.SetProviderConfig(cfg); !errors.Is(err, ErrInvalid) {
			t.Errorf("%s: got %v, want ErrInvalid", name, err)
		}
	}
	if got := s.Providers(); len(got) != 0 {
		t.Errorf("rejected providers were kept: %+v", got)
	}
	good := []string{"", "https://api.anthropic.com", "http://localhost:11434", "http://[::1]:11434", "http://127.0.0.1:11434/"}
	for _, u := range good {
		if err := s.SetProviderConfig(ProviderConfig{ID: "x", BaseURL: u}); err != nil {
			t.Errorf("base URL %q rejected: %v", u, err)
		}
	}
}

func TestErrorNeverEchoesTheURL(t *testing.T) {
	s, _ := loadTemp(t)
	err := s.SetProviderConfig(ProviderConfig{ID: "x", BaseURL: "https://user:sk-secret@example.com"})
	if err == nil || strings.Contains(err.Error(), "sk-secret") {
		t.Errorf("error leaks the URL: %v", err)
	}
}

func TestHandEditedFileIsValidatedOnLoad(t *testing.T) {
	dir := t.TempDir()
	cases := map[string]string{
		"not TOML":     "[providers.ollama\n",
		"bad base URL": "[providers.ollama]\nbase_url = \"http://example.com\"\n",
	}
	for name, body := range cases {
		path := filepath.Join(dir, strings.ReplaceAll(name, " ", "_")+".toml")
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := LoadTOML(path); err == nil {
			t.Errorf("%s: LoadTOML accepted the file", name)
		}
	}
}

func TestFailedSaveLeavesConfigUnchanged(t *testing.T) {
	// The parent of the config path is a file, so the save cannot succeed.
	blocker := filepath.Join(t.TempDir(), "blocker")
	if err := os.WriteFile(blocker, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	s, err := LoadTOML(filepath.Join(blocker, "config.toml"))
	if err != nil {
		// Some systems report the bad parent on read; that is also acceptable.
		t.Skipf("LoadTOML already failed: %v", err)
	}
	if err := s.SetProviderConfig(ollama); err == nil {
		t.Fatal("save under a file succeeded")
	}
	if got := s.Providers(); len(got) != 0 {
		t.Errorf("provider kept after a failed save: %+v", got)
	}
}

func TestDefaultPaths(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("LLMCTL_CONFIG_DIR", dir)
	t.Setenv("LLMCTL_DB_PATH", "")
	p, err := DefaultPaths()
	if err != nil {
		t.Fatal(err)
	}
	want := Paths{Dir: dir, Config: filepath.Join(dir, "config.toml"),
		Secrets: filepath.Join(dir, "secrets.age"), DB: filepath.Join(dir, "llmctl.db")}
	if p != want {
		t.Errorf("DefaultPaths = %+v, want %+v", p, want)
	}

	other := filepath.Join(t.TempDir(), "elsewhere.db")
	t.Setenv("LLMCTL_DB_PATH", other)
	if p, _ := DefaultPaths(); p.DB != other || p.Config != want.Config {
		t.Errorf("with LLMCTL_DB_PATH: %+v", p)
	}

	t.Setenv("LLMCTL_CONFIG_DIR", "")
	t.Setenv("LLMCTL_DB_PATH", "")
	p, err = DefaultPaths()
	if err != nil {
		t.Fatal(err)
	}
	base, _ := os.UserConfigDir()
	if p.Dir != filepath.Join(base, "llmctl") {
		t.Errorf("default Dir = %q, want %q", p.Dir, filepath.Join(base, "llmctl"))
	}
}
