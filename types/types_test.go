package types

import (
	"testing"
)

func TestMain(t *testing.T) {

}

func TestRecurrenceTypeValidate(t *testing.T) {

	tests := []struct {
		name     string
		input    RecurrenceType
		expected bool
	}{
		{"valid - none", RecurrenceNone, true},
		{"valid - daily", RecurrenceDaily, true},
		{"valid - weekly", RecurrenceWeekly, true},
		{"valid - monthly", RecurrenceMonthly, true},
		{"valid - empty string", RecurrenceType(""), false},
		{"valid - garbadge", RecurrenceType("garbage"), false},
		{"valid - typo", RecurrenceType("daly"), false},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			isValid := test.input.Validate()
			if isValid != test.expected {
				t.Errorf("Validate() = %v, want %v", isValid, test.expected)
			}
		})
	}

}
