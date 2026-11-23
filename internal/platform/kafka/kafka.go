package kafka

import (
	"context"
	"fmt"
	"time"

	kafkago "github.com/segmentio/kafka-go"
)

const (
	TopicPaymentEvents = "payments.events"
	TopicReviewEvents  = "reviews.events"
)

func NewWriter(brokers []string, topic string) *kafkago.Writer {
	return &kafkago.Writer{
		Addr:         kafkago.TCP(brokers...),
		Topic:        topic,
		Balancer:     &kafkago.Hash{},
		BatchTimeout: 10 * time.Millisecond,
		RequiredAcks: kafkago.RequireNone,
	}
}

func NewReader(brokers []string, topic, groupId string) *kafkago.Reader {
	return kafkago.NewReader(kafkago.ReaderConfig{
		Brokers:        brokers,
		Topic:          topic,
		GroupID:        groupId,
		MinBytes:       1,
		MaxBytes:       10 << 20,
		CommitInterval: time.Second,
	})
}

func EnsureTopics(ctx context.Context, broker string) (err error) {
	conn, err := kafkago.DialContext(ctx, "tcp", broker)
	if err != nil {
		return err
	}
	defer conn.Close()

	controller, err := conn.Controller()
	if err != nil {
		return fmt.Errorf("kafka: failed to get controller: %w", err)
	}

	ctrlConn, err := kafkago.DialContext(ctx, "tcp", fmt.Sprintf("%s:%d", controller.Host, controller.Port))
	if err != nil {
		return fmt.Errorf("kafka: failed to connect to controller: %w", err)
	}

	defer ctrlConn.Close()

	topics := []kafkago.TopicConfig{
		{
			Topic:             TopicPaymentEvents,
			NumPartitions:     3,
			ReplicationFactor: 1,
		},
		{
			Topic:             TopicReviewEvents,
			NumPartitions:     3,
			ReplicationFactor: 1,
		},
	}

	if err := ctrlConn.CreateTopics(topics...); err != nil {
		return fmt.Errorf("kafka: failed to create topics: %w", err)
	}

	return nil
}
