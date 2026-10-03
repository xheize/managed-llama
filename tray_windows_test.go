package main

import (
	"encoding/binary"
	"testing"
)

func TestLocalDashboardURL(t *testing.T) {
	tests := map[string]string{
		"127.0.0.1:3030": "http://127.0.0.1:3030",
		"0.0.0.0:3030":   "http://127.0.0.1:3030",
		":3030":          "http://127.0.0.1:3030",
		"[::]:3030":      "http://127.0.0.1:3030",
		"[::1]:3030":     "http://[::1]:3030",
	}
	for input, want := range tests {
		t.Run(input, func(t *testing.T) {
			if got := localDashboardURL(input); got != want {
				t.Fatalf("localDashboardURL(%q) = %q, want %q", input, got, want)
			}
		})
	}
}

func TestManagedLlamaIcon(t *testing.T) {
	icon := managedLlamaIcon()
	if len(icon) < 22 {
		t.Fatalf("icon is too short: %d", len(icon))
	}
	if got := binary.LittleEndian.Uint16(icon[2:4]); got != 1 {
		t.Fatalf("icon type = %d, want 1", got)
	}
	if got := binary.LittleEndian.Uint16(icon[4:6]); got != 1 {
		t.Fatalf("image count = %d, want 1", got)
	}
	if icon[6] != 32 || icon[7] != 32 {
		t.Fatalf("icon dimensions = %dx%d, want 32x32", icon[6], icon[7])
	}
}

func TestCompactStatusLabel(t *testing.T) {
	tests := map[string]string{
		"ready":                  "준비됨",
		"setup_required":         "설정 필요",
		"empty_library":          "모델 없음",
		"default_model_required": "기본 모델 필요",
		"invalid_default_model":  "모델 확인 필요",
		"error":                  "오류",
		"service_unavailable":    "서비스 연결 대기 중",
		"unknown":                "중지됨",
	}
	for phase, want := range tests {
		if got := compactStatusLabel(phase); got != want {
			t.Errorf("compactStatusLabel(%q) = %q, want %q", phase, got, want)
		}
	}
}
