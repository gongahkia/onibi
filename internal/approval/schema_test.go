package approval

import (
	"strings"
	"testing"
)

func TestValidateEditedInputKnownTool(t *testing.T) {
	if err := ValidateEditedInput("Bash", `{"command":"ls"}`, `{"command":"echo ok","timeout":1000}`); err != nil {
		t.Fatal(err)
	}
	err := ValidateEditedInput("Bash", `{"command":"ls"}`, `{"cmd":"echo ok"}`)
	if err == nil || !strings.Contains(err.Error(), "missing required field") {
		t.Fatalf("err = %v", err)
	}
	err = ValidateEditedInput("Bash", `{"command":"ls"}`, `{"command":"echo ok","env":{"X":"1"}}`)
	if err == nil || !strings.Contains(err.Error(), "not allowed") {
		t.Fatalf("err = %v", err)
	}
}

func TestValidateEditedInputMultiEdit(t *testing.T) {
	valid := `{"file_path":"/tmp/a","edits":[{"old_string":"a","new_string":"b"}]}`
	if err := ValidateEditedInput("MultiEdit", `{}`, valid); err != nil {
		t.Fatal(err)
	}
	invalid := `{"file_path":"/tmp/a","edits":[{"old_string":"a"}]}`
	if err := ValidateEditedInput("MultiEdit", `{}`, invalid); err == nil {
		t.Fatal("expected invalid nested edit")
	}
}

func TestValidateEditedInputUnknownToolPreservesShape(t *testing.T) {
	if err := ValidateEditedInput("Unknown", `{"x":"a","n":1}`, `{"x":"b","n":2}`); err != nil {
		t.Fatal(err)
	}
	err := ValidateEditedInput("Unknown", `{"x":"a"}`, `{"x":"b","y":"c"}`)
	if err == nil || !strings.Contains(err.Error(), "preserve original JSON shape") {
		t.Fatalf("err = %v", err)
	}
}
