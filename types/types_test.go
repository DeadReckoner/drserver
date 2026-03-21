package types

import (
	"testing"

	"github.com/stretchr/testify/assert"
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

func TestUserParseFullName(t *testing.T) {

	tests := []struct {
		name      string
		user      User
		wantFirst string
		wantLast  string
	}{
		{"empty name", User{}, "", ""},
		{"happy path", User{FullName: "John Smith"}, "John", "Smith"},
		{"already exists", User{FullName: "John Smith", FirstName: "Steve", LastName: "Miller"}, "Steve", "Miller"},
		{"first name only", User{FullName: "John"}, "John", ""},
		{"multiple spaces", User{FullName: "John    Smith"}, "John", "Smith"},
		{"padding on left and right", User{FullName: "    John Smith    "}, "John", "Smith"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			test.user.ParseFullName()
			assert.Equal(t, test.wantFirst, test.user.FirstName)
			assert.Equal(t, test.wantLast, test.user.LastName)
		})
	}

}
