package metrics

import (
	"context"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"google.golang.org/grpc"
	"google.golang.org/grpc/status"
)

var (
	grpcHandled = NewCounter(
		"grpc_server_handled_total",
		"gRPC request completed, labeled by method and status code",
		"method", "code")

	grpcDuration = NewHistogram(
		"grpc_server_duration_seconds",
		"gRPC request duration by method.",
		"method")
)

func UnaryServerInterceptor() grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {
		start := time.Now()
		resp, err := handler(ctx, req)

		grpcHandled.With(prometheus.Labels{
			"method": info.FullMethod,
			"code":   status.Code(err).String(),
		}).Inc()

		grpcDuration.With(prometheus.Labels{
			"method": info.FullMethod,
		}).Observe(time.Since(start).Seconds())

		return resp, err
	}
}
