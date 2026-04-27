package common

import "testing"

func TestGetEnvWithDeprecatedPrefersNew(t *testing.T) {
	t.Setenv("ENV_NEW", "new-value")
	t.Setenv("ENV_OLD", "old-value")

	value := GetEnvWithDeprecated("ENV_NEW", "ENV_OLD", "default-value")
	if value != "new-value" {
		t.Fatalf("expected new env value, got %q", value)
	}
}

func TestGetEnvWithDeprecatedFallsBackToOld(t *testing.T) {
	t.Setenv("ENV_OLD", "old-value")

	value := GetEnvWithDeprecated("ENV_NEW", "ENV_OLD", "default-value")
	if value != "old-value" {
		t.Fatalf("expected old env fallback value, got %q", value)
	}
}

func TestGetEnvWithDeprecatedUsesDefault(t *testing.T) {
	value := GetEnvWithDeprecated("ENV_NEW", "ENV_OLD", "default-value")
	if value != "default-value" {
		t.Fatalf("expected default value, got %q", value)
	}
}

func TestGetEnvBoolWithDeprecatedPrefersNew(t *testing.T) {
	t.Setenv("BOOL_NEW", "true")
	t.Setenv("BOOL_OLD", "false")

	value, err := GetEnvBoolWithDeprecated("BOOL_NEW", "BOOL_OLD", false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !value {
		t.Fatalf("expected true, got false")
	}
}

func TestGetEnvBoolWithDeprecatedFallbackToOld(t *testing.T) {
	t.Setenv("BOOL_OLD", "true")

	value, err := GetEnvBoolWithDeprecated("BOOL_NEW", "BOOL_OLD", false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !value {
		t.Fatalf("expected true, got false")
	}
}

func TestGetEnvBoolWithDeprecatedInvalid(t *testing.T) {
	t.Setenv("BOOL_OLD", "not-a-bool")

	_, err := GetEnvBoolWithDeprecated("BOOL_NEW", "BOOL_OLD", false)
	if err == nil {
		t.Fatalf("expected parse error, got nil")
	}
}
