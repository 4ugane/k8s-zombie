package detector

import (
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestAgeString_ZeroTimeReturnsEmpty(t *testing.T) {
	if got := ageString(metav1.Time{}); got != "" {
		t.Errorf("want empty string for zero time, got %q", got)
	}
}

func TestAgeString_LessThanAnHourReturnsJustNow(t *testing.T) {
	got := ageString(metav1.NewTime(time.Now().Add(-10 * time.Minute)))
	if got != "just now" {
		t.Errorf("want %q, got %q", "just now", got)
	}
}

func TestAgeString_HoursFormatsAsH(t *testing.T) {
	got := ageString(metav1.NewTime(time.Now().Add(-5 * time.Hour)))
	if got != "5h" {
		t.Errorf("want %q, got %q", "5h", got)
	}
}

func TestAgeString_DaysFormatsAsD(t *testing.T) {
	got := ageString(metav1.NewTime(time.Now().Add(-72 * time.Hour)))
	if got != "3d" {
		t.Errorf("want %q, got %q", "3d", got)
	}
}
