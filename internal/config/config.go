package config

import (
	"os"
	"strconv"
	"time"
)

const ServiceName = "content"

type Config struct {
	GRPC        GRPCConfig
	Kafka       KafkaConfig
	EntitiesURL string
	MediaURL    string
	MediaToken  string
}

type GRPCConfig struct {
	Addr            string
	ServiceToken    string
	ModerationToken string
	TLSCertFile     string
	TLSKeyFile      string
}

type KafkaConfig struct {
	Enabled bool
	Topic   string
	// PollInterval — период опроса transaction outbox диспетчером.
	PollInterval time.Duration
	Activity     MaterialActivityConfig
}

func Load() Config {
	return Config{
		EntitiesURL: os.Getenv("CONTENT_ENTITIES_URL"),
		MediaURL:    os.Getenv("CONTENT_MEDIA_URL"),
		MediaToken:  os.Getenv("CONTENT_MEDIA_TOKEN"),
		GRPC: GRPCConfig{
			Addr:            env("GRPC_ADDR", ":9090"),
			ServiceToken:    os.Getenv("CONTENT_GRPC_SERVICE_TOKEN"),
			ModerationToken: os.Getenv("CONTENT_GRPC_MODERATION_TOKEN"),
			TLSCertFile:     os.Getenv("CONTENT_GRPC_TLS_CERT_FILE"),
			TLSKeyFile:      os.Getenv("CONTENT_GRPC_TLS_KEY_FILE"),
		},
		Kafka: KafkaConfig{
			Enabled:      envBool("KAFKA_ENABLED", true),
			Topic:        env("KAFKA_TOPIC", "content.content-item-events"),
			PollInterval: envDuration("KAFKA_OUTBOX_POLL_INTERVAL", 500*time.Millisecond),
			Activity:     loadMaterialActivity(),
		},
	}
}

// envDuration разбирает duration из окружения или возвращает fallback.
func envDuration(key string, fallback time.Duration) time.Duration {
	value, ok := os.LookupEnv(key)
	if !ok || value == "" {
		return fallback
	}
	duration, err := time.ParseDuration(value)
	if err != nil {
		return fallback
	}

	return duration
}

func env(key, fallback string) string {
	if value, ok := os.LookupEnv(key); ok && value != "" {
		return value
	}

	return fallback
}

func envBool(key string, fallback bool) bool {
	value, err := strconv.ParseBool(env(key, strconv.FormatBool(fallback)))
	if err != nil {
		return fallback
	}

	return value
}
