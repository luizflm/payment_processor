package config

import (
	"fmt"
	"os"
	"strconv"
	"time"
)

// String returns the value of the environment variable key or def if the variable is not set.
func String(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}

	return def
}

// Int returns the value of the environment variable key as an int or 0 if the variable is not set.
func Int(key string, def int) int {
	v := os.Getenv(key)
	if v == "" {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return def
	}
	return n
}

// Duration returns the value of the environment variable key as a time.Duration or def if the variable is not set.
func Duration(key string, def time.Duration) time.Duration {
	if v := os.Getenv(key); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			return d
		}
	}

	return def
}

// MustString returns the value of the environment variable key or panics if the variable is not set.
func MustString(key string) string {
	v := os.Getenv(key)
	if v == "" {
		fmt.Fprintf(os.Stderr, "missing required environment variable %s\n", key)
		os.Exit(1)
	}
	return v
}
