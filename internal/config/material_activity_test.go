package config

import "testing"

func TestMaterialActivityTopics(t *testing.T) {
	t.Setenv("KAFKA_TOPIC", "content.legacy")
	t.Setenv("KAFKA_CONTENT_MATERIAL_TOPIC", "content.material")
	t.Setenv("KAFKA_CONTENT_LEARNING_TOPIC", "content.learning")
	t.Setenv("KAFKA_ENABLED", "true")
	t.Setenv("CONTENT_ACTIVITY_ENABLED", "false")
	cfg := Load().Kafka
	if material, learning, err := cfg.ActivityTopics(); err != nil || material != "" || learning != "" {
		t.Error("disabled activity must not emit events", err)
	}
	t.Setenv("CONTENT_ACTIVITY_ENABLED", "true")
	cfg = Load().Kafka
	if material, learning, err := cfg.ActivityTopics(); err != nil || material != "content.material" || learning != "content.learning" {
		t.Error("loaded unexpected activity topics", err)
	}
	cfg.Enabled = false
	if _, _, err := cfg.ActivityTopics(); err == nil {
		t.Error("activity cannot run without Kafka")
	}
	for _, bad := range []string{"content.legacy", "content_material", "not a topic"} {
		cfg = Load().Kafka
		cfg.Activity.LearningTopic = bad
		if _, _, err := cfg.ActivityTopics(); err == nil {
			t.Error("accepted an invalid or conflicting topic", bad)
		}
	}
}
