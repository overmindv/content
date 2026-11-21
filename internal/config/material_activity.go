package config

import (
	"errors"
	"regexp"
	"strings"

	"github.com/overmindv/content/pkg/api/activity"
)

type MaterialActivityConfig struct {
	Enabled             bool
	ContributionEnabled bool
	MaterialTopic       string
	LearningTopic       string
	ContributionTopic   string
}

func loadMaterialActivity() MaterialActivityConfig {
	return MaterialActivityConfig{
		Enabled:             envBool("CONTENT_ACTIVITY_ENABLED", false),
		ContributionEnabled: envBool("CONTENT_CONTRIBUTION_ENABLED", false),
		MaterialTopic:       env("KAFKA_CONTENT_MATERIAL_TOPIC", activity.DefaultMaterialTopic),
		LearningTopic:       env("KAFKA_CONTENT_LEARNING_TOPIC", activity.DefaultLearningTopic),
		ContributionTopic:   env("KAFKA_CONTENT_CONTRIBUTION_TOPIC", activity.DefaultContributionTopic),
	}
}

var topicName = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,248}$`)

type ActivityTopics struct {
	Material, Learning, Contribution string
}

func (cfg KafkaConfig) ActivityTopics() (ActivityTopics, error) {
	if !cfg.Activity.Enabled {
		if cfg.Activity.ContributionEnabled {
			return ActivityTopics{}, errors.New("CONTENT_CONTRIBUTION_ENABLED requires CONTENT_ACTIVITY_ENABLED")
		}

		return ActivityTopics{}, nil
	}
	if !cfg.Enabled {
		return ActivityTopics{}, errors.New("CONTENT_ACTIVITY_ENABLED requires KAFKA_ENABLED")
	}
	topics := ActivityTopics{Material: cfg.Activity.MaterialTopic, Learning: cfg.Activity.LearningTopic}
	names := []string{cfg.Topic, topics.Material, topics.Learning}
	if cfg.Activity.ContributionEnabled {
		topics.Contribution = cfg.Activity.ContributionTopic
		names = append(names, topics.Contribution)
	}
	seen := make(map[string]bool, len(names))
	for _, topic := range names {
		key := strings.ReplaceAll(topic, ".", "_")
		if !topicName.MatchString(topic) || seen[key] {
			return ActivityTopics{}, errors.New("activity and legacy topics must have distinct valid names")
		}
		seen[key] = true
	}

	return topics, nil
}
