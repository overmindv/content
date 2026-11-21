package config

import "testing"

func TestMaterialActivityTopics(t *testing.T) {
	t.Setenv("KAFKA_TOPIC", "content.legacy")
	t.Setenv("KAFKA_CONTENT_MATERIAL_TOPIC", "content.material")
	t.Setenv("KAFKA_CONTENT_LEARNING_TOPIC", "content.learning")
	t.Setenv("KAFKA_ENABLED", "true")
	t.Setenv("CONTENT_ACTIVITY_ENABLED", "false")
	t.Setenv("CONTENT_CONTRIBUTION_ENABLED", "false")
	t.Setenv("KAFKA_CONTENT_CONTRIBUTION_TOPIC", "content.editorial")
	cfg := Load().Kafka
	if topics, err := cfg.ActivityTopics(); err != nil || topics != (ActivityTopics{}) {
		t.Error("disabled activity must not emit events", err)
	}
	t.Setenv("CONTENT_ACTIVITY_ENABLED", "true")
	cfg = Load().Kafka
	if topics, err := cfg.ActivityTopics(); err != nil || topics.Material != "content.material" ||
		topics.Learning != "content.learning" || topics.Contribution != "" {
		t.Error("loaded unexpected activity topics", err)
	}
	cfg.Enabled = false
	if _, err := cfg.ActivityTopics(); err == nil {
		t.Error("activity cannot run without Kafka")
	}
	for _, bad := range []string{"content.legacy", "content_material", "not a topic"} {
		cfg = Load().Kafka
		cfg.Activity.LearningTopic = bad
		if _, err := cfg.ActivityTopics(); err == nil {
			t.Error("accepted an invalid or conflicting topic", bad)
		}
	}
	t.Setenv("CONTENT_CONTRIBUTION_ENABLED", "true")
	cfg = Load().Kafka
	if topics, err := cfg.ActivityTopics(); err != nil || topics.Contribution != "content.editorial" {
		t.Error("editorial topic not configured", err)
	}
	for _, bad := range []string{cfg.Topic, cfg.Activity.MaterialTopic, "content_learning", "not a topic"} {
		cfg.Activity.ContributionTopic = bad
		if _, err := cfg.ActivityTopics(); err == nil {
			t.Error("accepted an invalid or shared editorial topic", bad)
		}
	}
	cfg = Load().Kafka
	cfg.Activity.Enabled = false
	if _, err := cfg.ActivityTopics(); err == nil {
		t.Error("editorial activity requires material activity")
	}
}
