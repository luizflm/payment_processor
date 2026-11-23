package postgres_test

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/luizflm/payment-processor/internal/platform/postgres"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"
)

func TestConnectErrors(t *testing.T) {
	canceled, cancel := context.WithCancel(t.Context())
	cancel()
	expired, cancelDeadline := context.WithDeadline(t.Context(), time.Unix(1, 0))
	defer cancelDeadline()

	const dsn = "postgres://user:password@127.0.0.1:5432/payments?sslmode=disable"
	tests := []struct {
		name    string
		ctx     context.Context
		dsn     string
		message string
		cause   error
	}{
		{"invalid format", t.Context(), "invalid-dsn", "failed to parse dsn", nil},
		{"invalid port", t.Context(), "postgres://localhost:invalid/payments", "failed to parse dsn", nil},
		{"invalid SSL mode", t.Context(), "postgres://localhost/payments?sslmode=invalid", "failed to parse dsn", nil},
		{"canceled context", canceled, dsn, "failed to ping", context.Canceled},
		{"expired context", expired, dsn, "failed to ping", context.DeadlineExceeded},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pool, err := postgres.Connect(tt.ctx, tt.dsn)
			if pool != nil {
				t.Cleanup(pool.Close)
			}
			require.Nil(t, pool)
			require.ErrorContains(t, err, tt.message)
			if tt.cause != nil {
				require.ErrorIs(t, err, tt.cause)
			}
		})
	}
}

func TestRegisterPoolMetrics(t *testing.T) {
	registry := prometheus.NewRegistry()
	previous := prometheus.DefaultRegisterer
	prometheus.DefaultRegisterer = registry
	t.Cleanup(func() { prometheus.DefaultRegisterer = previous })

	pool, err := pgxpool.New(t.Context(), "postgres://localhost/payments?pool_min_conns=0&pool_min_idle_conns=0")
	require.NoError(t, err)
	t.Cleanup(pool.Close)
	postgres.RegisterPoolMetrics(pool)

	families, err := registry.Gather()
	require.NoError(t, err)
	values := make(map[string]float64)
	for _, family := range families {
		require.Len(t, family.Metric, 1)
		require.NotNil(t, family.Metric[0].Gauge)
		values[family.GetName()] = family.Metric[0].Gauge.GetValue()
	}
	require.Equal(t, map[string]float64{
		"pgx_pool_total_conns":         0,
		"pgx_pool_idle_conns":          0,
		"pgx_pool_acquired_conns":      0,
		"pgx_pool_empty_acquire_conns": 0,
	}, values)
}
