package config

import (
	"os"
	"os/exec"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestString(t *testing.T) {
	t.Run("returns env var when set", func(t *testing.T) {
		t.Setenv("TEST_KEY", "custom_val")
		assert.Equal(t, "custom_val", String("TEST_KEY", "default_val"))
	})

	t.Run("returns default when unset", func(t *testing.T) {
		assert.Equal(t, "default_val", String("NON_EXISTING_KEY", "default_val"))
	})
}

func TestInt(t *testing.T) {
	tests := []struct {
		name     string
		envVal   string
		setEnv   bool
		def      int
		expected int
	}{
		{
			name:     "returns parsed int when set and valid",
			envVal:   "42",
			setEnv:   true,
			def:      10,
			expected: 42,
		},
		{
			name:     "returns default when set but invalid",
			envVal:   "invalid",
			setEnv:   true,
			def:      10,
			expected: 10,
		},
		{
			name:     "returns default when unset",
			setEnv:   false,
			def:      10,
			expected: 10,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.setEnv {
				t.Setenv("TEST_INT_KEY", tt.envVal)
				assert.Equal(t, tt.expected, Int("TEST_INT_KEY", tt.def))
			} else {
				assert.Equal(t, tt.expected, Int("NON_EXISTING_INT_KEY", tt.def))
			}
		})
	}
}

func TestDuration(t *testing.T) {
	tests := []struct {
		name     string
		envVal   string
		setEnv   bool
		def      time.Duration
		expected time.Duration
	}{
		{
			name:     "returns parsed duration when set and valid",
			envVal:   "5s",
			setEnv:   true,
			def:      1 * time.Second,
			expected: 5 * time.Second,
		},
		{
			name:     "returns default when set but invalid",
			envVal:   "invalid_duration",
			setEnv:   true,
			def:      1 * time.Second,
			expected: 1 * time.Second,
		},
		{
			name:     "returns default when unset",
			setEnv:   false,
			def:      1 * time.Second,
			expected: 1 * time.Second,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.setEnv {
				t.Setenv("TEST_DUR_KEY", tt.envVal)
				assert.Equal(t, tt.expected, Duration("TEST_DUR_KEY", tt.def))
			} else {
				assert.Equal(t, tt.expected, Duration("NON_EXISTING_DUR_KEY", tt.def))
			}
		})
	}
}

func TestMustString(t *testing.T) {
	t.Run("returns value when set", func(t *testing.T) {
		t.Setenv("TEST_MUST_KEY", "must_val")
		assert.Equal(t, "must_val", MustString("TEST_MUST_KEY"))
	})

	t.Run("exits with 1 when unset", func(t *testing.T) {
		if os.Getenv("BE_CRASHER") == "1" {
			MustString("UNSET_CRITICAL_KEY")
			return
		}
		cmd := exec.Command(os.Args[0], "-test.run=TestMustString")
		cmd.Env = append(os.Environ(), "BE_CRASHER=1")
		err := cmd.Run()
		if e, ok := err.(*exec.ExitError); ok && !e.Success() {
			return
		}
		t.Fatalf("process ran with err %v, want exit status 1", err)
	})
}
