package models

import "testing"

func strptr(s string) *string { return &s }

func TestNormalizeEmail(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"ALICE@Example.COM", "alice@example.com"},
		{"  alice@example.com  ", "alice@example.com"},
		{"  MiXeD@Case.COM  ", "mixed@case.com"},
		{"", ""},
	}
	for _, c := range cases {
		if got := NormalizeEmail(c.in); got != c.want {
			t.Errorf("NormalizeEmail(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestDecideContactAction_CreateWhenNoExisting(t *testing.T) {
	if got := DecideContactAction(nil, "Alice"); got != ContactActionCreate {
		t.Errorf("expected create, got %q", got)
	}
}

func TestDecideContactAction_ReturnExistingWhenHasName(t *testing.T) {
	existing := &Contact{Email: "alice@example.com", Name: strptr("Alice")}
	if got := DecideContactAction(existing, "Different"); got != ContactActionReturnExisting {
		t.Errorf("expected return-existing, got %q", got)
	}
}

func TestDecideContactAction_UpdateNameWhenBlank(t *testing.T) {
	nilName := &Contact{Email: "alice@example.com", Name: nil}
	if got := DecideContactAction(nilName, "Alice"); got != ContactActionUpdateName {
		t.Errorf("expected update-name for nil existing name, got %q", got)
	}
	emptyName := &Contact{Email: "alice@example.com", Name: strptr("")}
	if got := DecideContactAction(emptyName, "Alice"); got != ContactActionUpdateName {
		t.Errorf("expected update-name for empty existing name, got %q", got)
	}
}

func TestDecideContactAction_ReturnExistingWhenNoIncomingName(t *testing.T) {
	existing := &Contact{Email: "alice@example.com", Name: nil}
	if got := DecideContactAction(existing, ""); got != ContactActionReturnExisting {
		t.Errorf("expected return-existing, got %q", got)
	}
}
