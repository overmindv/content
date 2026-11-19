package config

import (
	"os"
	"strconv"
	"time"

	"github.com/overmindv/content/pkg/api/activity"
)

const ServiceName = "content"

type Config struct {
	GRPC                  GRPCConfig
	Kafka                 KafkaConfig
	EntitiesURL           string
	ReviewCandidatesURL   string
	ReviewCandidatesToken string
	MediaURL              string
	MediaToken            string
}

type GRPCConfig struct {
	Addr            string
	ServiceToken    string
	ModerationToken string
	CatalogToken    string
}

type KafkaConfig struct {
	Enabled           bool
	Topic             string
	ActivityEnabled   bool
	MaterialTopic     string
	ContributionTopic string
	LearningTopic     string
	// PollInterval — период опроса transaction outbox диспетчером.
	PollInterval time.Duration
}

func Load() Config {
	return Config{
		EntitiesURL:           os.Getenv("CONTENT_ENTITIES_URL"),
		ReviewCandidatesURL:   os.Getenv("CONTENT_REVIEW_CANDIDATES_URL"),
		ReviewCandidatesToken: os.Getenv("CONTENT_REVIEW_CANDIDATES_TOKEN"),
		MediaURL:              os.Getenv("CONTENT_MEDIA_URL"),
		MediaToken:            os.Getenv("CONTENT_MEDIA_TOKEN"),
		GRPC: GRPCConfig{
			Addr:            env("GRPC_ADDR", ":9090"),
			ServiceToken:    os.Getenv("CONTENT_GRPC_SERVICE_TOKEN"),
			ModerationToken: os.Getenv("CONTENT_GRPC_MODERATION_TOKEN"),
			CatalogToken:    os.Getenv("CONTENT_GRPC_CATALOG_TOKEN"),
		},
		Kafka: KafkaConfig{
			Enabled:           envBool("KAFKA_ENABLED", true),
			Topic:             env("KAFKA_TOPIC", "content.content-item-events"),
			ActivityEnabled:   envBool("CONTENT_ACTIVITY_ENABLED", false),
			MaterialTopic:     env("KAFKA_CONTENT_MATERIAL_TOPIC", activity.DefaultMaterialTopic),
			ContributionTopic: env("KAFKA_CONTENT_CONTRIBUTION_TOPIC", activity.DefaultContributionTopic),
			LearningTopic:     env("KAFKA_CONTENT_LEARNING_TOPIC", activity.DefaultLearningTopic),
			PollInterval:      envDuration("KAFKA_OUTBOX_POLL_INTERVAL", 500*time.Millisecond),
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
