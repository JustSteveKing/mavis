package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestMissingFileGivesDefaults(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	c, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if c.Thresholds != (Thresholds{14, 30, 60}) {
		t.Fatalf("thresholds = %+v", c.Thresholds)
	}
	if c.DayMinutes() != 450 {
		t.Fatalf("day = %d minutes", c.DayMinutes())
	}
}

func TestPartialThresholdsKeepDefaults(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	write(t, filepath.Join(dir, "mavis", "config.yaml"), "root: /vault\nthresholds:\n  warm_to_cold: 90\n")

	c, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if c.Root != "/vault" || c.Thresholds != (Thresholds{14, 30, 90}) {
		t.Fatalf("got %+v", c)
	}
}

func TestRootPrecedence(t *testing.T) {
	c := &Config{Root: "/from-config"}

	t.Setenv("MAVIS_ROOT", "/from-env")
	if got, _ := c.ResolveRoot("/from-flag"); got != "/from-flag" {
		t.Errorf("flag should win, got %s", got)
	}
	if got, _ := c.ResolveRoot(""); got != "/from-env" {
		t.Errorf("env should beat config, got %s", got)
	}
	t.Setenv("MAVIS_ROOT", "")
	if got, _ := c.ResolveRoot(""); got != "/from-config" {
		t.Errorf("config should be the fallback, got %s", got)
	}
	if _, err := (&Config{}).ResolveRoot(""); err == nil {
		t.Error("want an error with no root anywhere")
	}
}

func TestSaveRootKeepsOtherSettings(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	path := filepath.Join(dir, "mavis", "config.yaml")
	write(t, path, "thresholds:\n  warm_to_cold: 90\n")

	c, _ := Load()
	if err := c.SaveRoot("/vault"); err != nil {
		t.Fatal(err)
	}
	again, _ := Load()
	if again.Root != "/vault" || again.Thresholds.WarmToCold != 90 {
		t.Fatalf("got %+v", again)
	}
}

func write(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestDayHours(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	write(t, filepath.Join(dir, "mavis", "config.yaml"), "day_hours: 8\n")
	c, _ := Load()
	if c.DayMinutes() != 480 {
		t.Fatalf("day = %d", c.DayMinutes())
	}
}
