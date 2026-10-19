package store

import (
	"reflect"
	"testing"

	"github.com/escalated-dev/escalated-go/models"
)

func TestDecodeChatRuleAgentIDs(t *testing.T) {
	cases := map[string][]models.UserID{
		``:                 nil,
		`{}`:               nil,
		`[]`:               nil,
		`{1,2}`:            {"1", "2"},
		`{"a-b","c\"d",3}`: {"a-b", `c"d`, "3"},
		`["42","x-uuid"]`:  {"42", "x-uuid"},
		`[42, 7]`:          {"42", "7"},
		`{NULL}`:           nil,
		`[not json`:        nil,
	}
	for in, want := range cases {
		if got := decodeChatRuleAgentIDs(in); !reflect.DeepEqual(got, want) {
			t.Errorf("decode(%q) = %#v, want %#v", in, got, want)
		}
	}

	ids := []models.UserID{"42", "a,b", `q"x`}
	if got := decodeChatRuleAgentIDs(encodeChatRuleAgentIDs(ids)); !reflect.DeepEqual(got, ids) {
		t.Errorf("round trip = %#v, want %#v", got, ids)
	}
}
