package task

import (
	"errors"
	"testing"
	"time"
)

func TestCronAcceptsFiveSixAndSevenFieldForms(t *testing.T) {
	utc := time.UTC
	minute := time.Date(2026, time.January, 2, 3, 4, 0, 0, utc)
	if !MatchesCron("4 3 2 1 *", minute) {
		t.Fatal("five-field cron should match minute/hour/day-of-month")
	}
	second := time.Date(2026, time.January, 2, 3, 4, 5, 0, utc)
	if !MatchesCron("5 4 3 2 1 *", second) {
		t.Fatal("six-field cron should match second/minute/hour/day-of-month/month")
	}
	if !MatchesCron("5 4 3 2 1 * 2026", second) {
		t.Fatal("seven-field cron should match an explicit year")
	}
	if MatchesCron("5 4 3 2 1 * 2025", second) {
		t.Fatal("explicit year must be enforced")
	}
	if HasSeconds("4 3 2 1 *") || !HasSeconds("5 4 3 2 1 *") {
		t.Fatal("seconds-field detection is incorrect")
	}
}

func TestCronValidationRejectsMalformedAndOutOfRangeFields(t *testing.T) {
	for _, expression := range []string{
		"", "* * * *", "* * * * * * * *", "*/0 * * * *", "60 * * * *",
		"1-0 * * * *", "1,,2 * * * *", "1/2/3 * * * *", "* * * * foo", "0 0 31 2 *",
	} {
		if ValidateCron(expression) == nil {
			t.Errorf("ValidateCron(%q) accepted malformed expression", expression)
		}
	}
	if ValidateCron("0 0 29 2 *") != nil {
		t.Fatal("February 29 expression should remain valid for leap years")
	}
}

func TestCronDayOfMonthAndWeekdayUseStandardORSemantics(t *testing.T) {
	// With both fields restricted, either field may trigger an occurrence.
	if !MatchesCron("0 0 0 15 * 1", time.Date(2026, time.January, 15, 0, 0, 0, 0, time.UTC)) {
		t.Fatal("day-of-month match should trigger when both day fields are restricted")
	}
	if !MatchesCron("0 0 0 15 * 1", time.Date(2026, time.January, 5, 0, 0, 0, 0, time.UTC)) {
		t.Fatal("day-of-week match should trigger when both day fields are restricted")
	}
	if MatchesCron("0 0 0 15 * 1", time.Date(2026, time.January, 6, 0, 0, 0, 0, time.UTC)) {
		t.Fatal("unrestricted day should not create a false match")
	}
}

func TestNextExecutionsUsesTimezoneAndFindsLeapDay(t *testing.T) {
	after := time.Date(2025, time.February, 28, 23, 59, 59, 0, time.UTC)
	got, err := NextExecutions("0 0 29 2 *", "UTC", after, 1)
	if err != nil {
		t.Fatal(err)
	}
	want := time.Date(2028, time.February, 29, 0, 0, 0, 0, time.UTC)
	if len(got) != 1 || !got[0].Equal(want) {
		t.Fatalf("leap-day result=%v, want %v", got, want)
	}
	shanghai, err := NextExecutions("0 9 * * *", "Asia/Shanghai", time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC), 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(shanghai) != 2 || shanghai[0].Location().String() != "Asia/Shanghai" || shanghai[0].Hour() != 9 {
		t.Fatalf("timezone preview=%v", shanghai)
	}
}

func TestNextExecutionsBoundsImpossibleCalendarDate(t *testing.T) {
	_, err := NextExecutions("0 0 31 2 *", "UTC", time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC), 1)
	if !errors.Is(err, ErrInvalidCronExpression) {
		t.Fatalf("impossible date error=%v, want ErrInvalidCronExpression", err)
	}
	if _, err := NextExecutions("* * * * *", "No/Such_Zone", time.Now(), 1); err == nil {
		t.Fatal("invalid timezone should be returned")
	}
}

func TestCronAliases(t *testing.T) {
	at := time.Date(2026, time.January, 4, 0, 0, 0, 0, time.UTC) // Sunday midnight.
	if !MatchesCron("@weekly", at) || !MatchesCron("@midnight", at) || !MatchesCron("@daily", at) {
		t.Fatal("supported aliases should match their canonical expressions")
	}
	if ValidateCron("@unknown") == nil {
		t.Fatal("unknown alias should be rejected")
	}
}
