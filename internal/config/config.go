package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

const (
	ServiceName = "content"
	EnvPrefix   = "CONTENT"
)

type Config struct {
	Service  ServiceConfig
	HTTP     HTTPConfig
	GRPC     GRPCConfig
	Database DatabaseConfig
	Kafka    KafkaConfig
	Metrics  MetricsConfig
}

type ServiceConfig struct {
	Name        string
	Environment string
	Version     string
}

type HTTPConfig struct {
	Addr            string
	ReadTimeout     time.Duration
	WriteTimeout    time.Duration
	ShutdownTimeout time.Duration
}

type GRPCConfig struct {
	Addr string
}

type DatabaseConfig struct {
	DSN             string
	MaxOpenConns    int
	MaxIdleConns    int
	ConnMaxLifetime time.Duration
}

type KafkaConfig struct {
	Enabled  bool
	Brokers  []string
	Topic    string
	ClientID string
}

type MetricsConfig struct {
	Namespace string
}

func Load() Config {
	return Config{
		Service: ServiceConfig{
			Name:        env("SERVICE_NAME", ServiceName),
			Environment: env("ENV", "local"),
			Version:     env("VERSION", "dev"),
		},
		HTTP: HTTPConfig{
			Addr:            env("HTTP_ADDR", ":8080"),
			ReadTimeout:     envDuration("HTTP_READ_TIMEOUT", 5*time.Second),
			WriteTimeout:    envDuration("HTTP_WRITE_TIMEOUT", 10*time.Second),
			ShutdownTimeout: envDuration("SHUTDOWN_TIMEOUT", 10*time.Second),
		},
		GRPC: GRPCConfig{
			Addr: env("GRPC_ADDR", ":9090"),
		},
		Database: DatabaseConfig{
			DSN:             env("DB_DSN", "postgres://postgres:change-me@localhost:5432/content?sslmode=disable"),
			MaxOpenConns:    envInt("DB_MAX_OPEN_CONNS", 20),
			MaxIdleConns:    envInt("DB_MAX_IDLE_CONNS", 10),
			ConnMaxLifetime: envDuration("DB_CONN_MAX_LIFETIME", 30*time.Minute),
		},
		Kafka: KafkaConfig{
			Enabled:  envBool("KAFKA_ENABLED", true),
			Brokers:  envCSV("KAFKA_BROKERS", []string{"localhost:9092"}),
			Topic:    env("KAFKA_TOPIC", "content.content-item-events"),
			ClientID: env("KAFKA_CLIENT_ID", ServiceName),
		},
		Metrics: MetricsConfig{
			Namespace: env("METRICS_NAMESPACE", "content"),
		},
	}
}

func env(suffix, fallback string) string {
	key := fmt.Sprintf("%s_%s", EnvPrefix, suffix)
	if value, ok := os.LookupEnv(key); ok && value != "" {
		return value
	}

	return fallback
}

func envInt(suffix string, fallback int) int {
	value, err := strconv.Atoi(env(suffix, strconv.Itoa(fallback)))
	if err != nil {
		return fallback
	}

	return value
}

func envBool(suffix string, fallback bool) bool {
	value, err := strconv.ParseBool(env(suffix, strconv.FormatBool(fallback)))
	if err != nil {
		return fallback
	}

	return value
}

func envDuration(suffix string, fallback time.Duration) time.Duration {
	value, err := time.ParseDuration(env(suffix, fallback.String()))
	if err != nil {
		return fallback
	}

	return value
}

func envCSV(suffix string, fallback []string) []string {
	value := env(suffix, strings.Join(fallback, ","))
	parts := strings.Split(value, ",")
	result := make([]string, 0, len(parts))
	for _, item := range parts {
		trimmed := strings.TrimSpace(item)
		if trimmed != "" {
			result = append(result, trimmed)
		}
	}

	if len(result) == 0 {
		return fallback
	}

	return result
}
