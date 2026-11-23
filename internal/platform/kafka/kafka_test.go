package kafka_test

import (
	"context"
	"testing"
	"time"

	"github.com/luizflm/payment-processor/internal/platform/kafka"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	tckafka "github.com/testcontainers/testcontainers-go/modules/kafka"
)

func TestNewWriterAndReader(t *testing.T) {
	brokers := []string{"localhost:9092"}
	topic := "test.topic"
	groupID := "test-group"

	writer := kafka.NewWriter(brokers, topic)
	assert.NotNil(t, writer)
	assert.Equal(t, topic, writer.Topic)

	reader := kafka.NewReader(brokers, topic, groupID)
	assert.NotNil(t, reader)
	assert.Equal(t, topic, reader.Config().Topic)
	assert.Equal(t, groupID, reader.Config().GroupID)
}

func TestEnsureTopics_Error(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()

	err := kafka.EnsureTopics(ctx, "127.0.0.1:59998")
	assert.Error(t, err)
}

func TestEnsureTopics_WithContainer(t *testing.T) {
	ctx := context.Background()
	kafkaContainer, err := tckafka.Run(ctx, "confluentinc/cp-kafka:7.5.0",
		tckafka.WithClusterID("test-cluster"),
	)
	if err != nil {
		t.Skipf("skipping kafka testcontainer: %v", err)
	}
	defer func() {
		_ = kafkaContainer.Terminate(ctx)
	}()

	brokers, err := kafkaContainer.Brokers(ctx)
	require.NoError(t, err)
	require.NotEmpty(t, brokers)

	err = kafka.EnsureTopics(ctx, brokers[0])
	assert.NoError(t, err)
}
