package metrics

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestHandler(t *testing.T) {
	h := Handler()
	require.NotNil(t, h)

	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
}

func TestServe(t *testing.T) {
	t.Run("successful serve and shutdown", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		logger := slog.New(slog.NewTextHandler(io.Discard, nil))
		addr := "127.0.0.1:19091"

		// Test with a context that is cancelled soon
		go Serve(ctx, addr, logger)

		// Wait briefly then cancel
		time.Sleep(50 * time.Millisecond)
		cancel()
		time.Sleep(50 * time.Millisecond)
	})

	t.Run("serve error on invalid address", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		logger := slog.New(slog.NewTextHandler(io.Discard, nil))
		Serve(ctx, "invalid_address:9999999", logger)
	})
}

func TestConstructors(t *testing.T) {
	t.Run("NewHistogram", func(t *testing.T) {
		h := NewHistogram("test_hist", "test histogram", "label1")
		assert.NotNil(t, h)
		h.With(prometheus.Labels{"label1": "val"}).Observe(0.5)
	})

	t.Run("NewCounter", func(t *testing.T) {
		c := NewCounter("test_counter", "test counter", "label1")
		assert.NotNil(t, c)
		c.With(prometheus.Labels{"label1": "val"}).Inc()
	})

	t.Run("NewGauge", func(t *testing.T) {
		g := NewGauge("test_gauge", "test gauge")
		assert.NotNil(t, g)
		g.Set(42)
	})

	t.Run("NewGaugeVec", func(t *testing.T) {
		gv := NewGaugeVec("test_gauge_vec", "test gauge vec", "label1")
		assert.NotNil(t, gv)
		gv.With(prometheus.Labels{"label1": "val"}).Set(100)
	})

	t.Run("NewGaugeFunc", func(t *testing.T) {
		NewGaugeFunc("test_gauge_func", "test gauge func", func() float64 {
			return 123.45
		})
	})
}

func TestUnaryServerInterceptor(t *testing.T) {
	interceptor := UnaryServerInterceptor()
	require.NotNil(t, interceptor)

	t.Run("successful unary call", func(t *testing.T) {
		info := &grpc.UnaryServerInfo{
			FullMethod: "/test.Service/TestMethod",
		}
		handler := func(ctx context.Context, req any) (any, error) {
			return "response", nil
		}

		resp, err := interceptor(context.Background(), "request", info, handler)
		assert.NoError(t, err)
		assert.Equal(t, "response", resp)
	})

	t.Run("failed unary call", func(t *testing.T) {
		info := &grpc.UnaryServerInfo{
			FullMethod: "/test.Service/TestFailMethod",
		}
		handler := func(ctx context.Context, req any) (any, error) {
			return nil, status.Error(codes.InvalidArgument, "bad arg")
		}

		resp, err := interceptor(context.Background(), "request", info, handler)
		assert.Error(t, err)
		assert.Nil(t, resp)
		assert.Equal(t, codes.InvalidArgument, status.Code(err))
	})
}
