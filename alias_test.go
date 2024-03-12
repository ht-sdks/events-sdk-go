package htevents

import "testing"

func TestAliasMissingUserId(t *testing.T) {
	alias := Alias{
		PreviousId: "1",
	}

	if err := alias.Validate(); err == nil {
		t.Error("validating an invalid alias object succeeded:", alias)

	} else if e, ok := err.(FieldError); !ok {
		t.Error("invalid error type returned when validating alias:", err)

	} else if e != (FieldError{
		Type:  "htevents.Alias",
		Name:  "UserId",
		Value: "",
	}) {
		t.Error("invalid error value returned when validating alias:", err)
	}
}

func TestAliasMissingPreviousId(t *testing.T) {
	alias := Alias{
		UserId: "1",
	}

	if err := alias.Validate(); err == nil {
		t.Error("validating an invalid alias object succeeded:", alias)

	} else if e, ok := err.(FieldError); !ok {
		t.Error("invalid error type returned when validating alias:", err)

	} else if e != (FieldError{
		Type:  "htevents.Alias",
		Name:  "PreviousId",
		Value: "",
	}) {
		t.Error("invalid error value returned when validating alias:", err)
	}
}

func TestAliasValid(t *testing.T) {
	alias := Alias{
		PreviousId: "1",
		UserId:     "2",
	}

	if err := alias.Validate(); err != nil {
		t.Error("validating a valid alias object failed:", alias, err)
	}
}
