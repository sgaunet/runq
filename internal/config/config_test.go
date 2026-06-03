package config_test

import (
	"strings"
	"testing"
	"time"

	"github.com/sgaunet/runq/internal/config"
)

func TestDefaults_MatchesContract(t *testing.T) {
	d := config.Defaults()
	if d.Parallel != 10 {
		t.Errorf("Parallel = %d, want 10", d.Parallel)
	}
	if d.KillGrace != 5*time.Second {
		t.Errorf("KillGrace = %v, want 5s", d.KillGrace)
	}
	if d.MaxQueue != 50 {
		t.Errorf("MaxQueue = %d, want 50", d.MaxQueue)
	}
	if d.OutputFormat != config.OutputText {
		t.Errorf("OutputFormat = %q, want %q", d.OutputFormat, config.OutputText)
	}
}

func TestValidate(t *testing.T) {
	valid := func() config.Config {
		c := config.Defaults()
		c.Args = []string{"echo hi"}
		return c
	}

	cases := []struct {
		name    string
		mutate  func(c *config.Config)
		wantErr string // substring; empty = no error expected
	}{
		{"baseline ok", func(_ *config.Config) {}, ""},
		{"parallel too low", func(c *config.Config) { c.Parallel = 0 }, "--parallel"},
		{"parallel too high", func(c *config.Config) { c.Parallel = 1001 }, "--parallel"},
		{"max-queue zero", func(c *config.Config) { c.MaxQueue = 0 }, "--max-queue"},
		{"negative kill-grace", func(c *config.Config) { c.KillGrace = -time.Second }, "--kill-grace"},
		{"negative timeout", func(c *config.Config) { c.Timeout = -time.Second }, "--timeout"},
		{"bad output", func(c *config.Config) { c.OutputFormat = "xml" }, "--output"},
		{"stdin+file conflict", func(c *config.Config) {
			c.FromStdin = true
			c.FromFile = "x"
			c.Args = nil
		}, "mutually exclusive"},
		{"stdin+args conflict", func(c *config.Config) {
			c.FromStdin = true
		}, "mutually exclusive"},
		{"relative log-dir rejected", func(c *config.Config) {
			c.LogDir = "relative/path"
		}, "--log-dir"},
		{"absolute log-dir ok", func(c *config.Config) {
			c.LogDir = "/tmp/runq/logs"
		}, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := valid()
			tc.mutate(&c)
			err := c.Validate()
			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("Validate() = %v, want nil", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("Validate() = nil, want error containing %q", tc.wantErr)
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Errorf("Validate() = %v, want error containing %q", err, tc.wantErr)
			}
		})
	}
}
