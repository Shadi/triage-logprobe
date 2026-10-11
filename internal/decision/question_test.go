package decision

import (
	"errors"
	"testing"
)

func TestQuestionValidate(t *testing.T) {
	two := []Option{{Key: "a", Description: "a"}, {Key: "b", Description: "b"}}
	tests := []struct {
		name string
		q    Question
		ok   bool
	}{
		{"choice ok", Question{Kind: KindChoice, Instructions: "q", Options: two}, true},
		{"score ok", Question{Kind: KindScore, Instructions: "q", Options: two}, true},
		{"choice without instructions", Question{Kind: KindChoice, Options: two}, false},
		{"noul ok", Question{Kind: KindNoul, Instructions: "Customers can notice this problem."}, true},
		{"choice one option", Question{Kind: KindChoice, Instructions: "q", Options: two[:1]}, false},
		{"score ten levels", Question{Kind: KindScore, Instructions: "q", Options: make([]Option, 10)}, false},
		{"noul with options", Question{Kind: KindNoul, Instructions: "x", Options: two}, false},
		{"noul without statement", Question{Kind: KindNoul, Instructions: "  "}, false},
		{"unknown kind", Question{Kind: "rank", Instructions: "q", Options: two}, false},
		{"empty kind", Question{Instructions: "q", Options: two}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.q.Validate()
			if tt.ok && err != nil {
				t.Errorf("unexpected error: %v", err)
			}
			if !tt.ok && !errors.Is(err, ErrInvalidQuestion) {
				t.Errorf("err = %v, want ErrInvalidQuestion", err)
			}
		})
	}
}

func TestQuestionOptions_NoulSynthesized(t *testing.T) {
	got := Question{Kind: KindNoul, Instructions: "x"}.options()
	if len(got) != 2 || got[0].Key != NoulYes || got[1].Key != NoulNo {
		t.Errorf("noul options = %+v, want yes/no", got)
	}
}
