package kafka

import (
	"context"
	"encoding/json"
	"time"

	"github.com/google/uuid"
	kafkago "github.com/segmentio/kafka-go"
)

// Writer 统一封装模块的数据层 Kafka 生产者。
type Writer struct {
	client *kafkago.Writer
}

const vehicleEventBatchTimeout = 10 * time.Millisecond

// NewWriter 创建共享 Kafka 生产者。
func NewWriter(brokers []string) *Writer {
	if len(brokers) == 0 {
		return nil
	}
	return &Writer{client: &kafkago.Writer{
		Addr:         kafkago.TCP(brokers...),
		Balancer:     &kafkago.Hash{},
		BatchTimeout: vehicleEventBatchTimeout,
	}}
}

// Close 释放生产者连接。
func (w *Writer) Close() error {
	return w.client.Close()
}

// Publish 按统一事件信封发送业务事件。
func (w *Writer) Publish(ctx context.Context, topic, key string, payload []byte) error {
	payloadValue := any(string(payload))
	if json.Valid(payload) {
		payloadValue = json.RawMessage(payload)
	}
	event := map[string]any{
		"event_id": uuid.NewString(), "event_type": topic,
		"occurred_at": time.Now().UTC(), "biz_id": key,
		"payload": payloadValue,
	}
	body, err := json.Marshal(event)
	if err != nil {
		return err
	}
	return w.client.WriteMessages(ctx, kafkago.Message{
		Topic: topic, Key: []byte(key), Value: body,
	})
}
