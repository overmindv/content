package config

import (
	"os"
	"strconv"
)

const ServiceName = "content"

type Config struct {
	GRPC  GRPCConfig
	Kafka KafkaConfig
}

type GRPCConfig struct {
	Addr string
}

type KafkaConfig struct {
	Enabled bool
	Topic   string
}

func Load() Config {
	return Config{
		GRPC: GRPCConfig{
			Addr: env("GRPC_ADDR", ":9090"),
		},
		Kafka: KafkaConfig{
			Enabled: envBool("KAFKA_ENABLED", true),
			Topic:   env("KAFKA_TOPIC", "content.content-item-events"),
		},
	}
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
