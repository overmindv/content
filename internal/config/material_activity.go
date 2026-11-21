package config

import (
	"errors"
	"regexp"
	"strings"

	"github.com/overmindv/content/pkg/api/activity"
)

type MaterialActivityConfig struct {
	Enabled       bool
	MaterialTopic string
	LearningTopic string
}

func loadMaterialActivity() MaterialActivityConfig {
	return MaterialActivityConfig{
		Enabled:       envBool("CONTENT_ACTIVITY_ENABLED", false),
		MaterialTopic: env("KAFKA_CONTENT_MATERIAL_TOPIC", activity.DefaultMaterialTopic),
		LearningTopic: env("KAFKA_CONTENT_LEARNING_TOPIC", activity.DefaultLearningTopic),
	}
}

var topicName = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,248}$`)

func (cfg KafkaConfig) ActivityTopics() (string, string, error) {
	if !cfg.Activity.Enabled {
		return "", "", nil
	}
	if !cfg.Enabled {
		return "", "", errors.New("CONTENT_ACTIVITY_ENABLED requires KAFKA_ENABLED")
	}
	seen := make(map[string]bool, 3)
	for _, topic := range []string{cfg.Topic, cfg.Activity.MaterialTopic, cfg.Activity.LearningTopic} {
		key := strings.ReplaceAll(topic, ".", "_")
		if !topicName.MatchString(topic) || seen[key] {
			return "", "", errors.New("material, learning and legacy Kafka topics must have distinct valid names")
		}
		seen[key] = true
	}

	return cfg.Activity.MaterialTopic, cfg.Activity.LearningTopic, nil
}
