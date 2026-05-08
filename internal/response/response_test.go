package response

import (
	"encoding/json"
	"errors"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestHTTPResponseMarshalJSON(t *testing.T) {
	tests := []struct {
		name string
		in   HTTPResponse
		want string
	}{
		{
			name: "data only",
			in:   HTTPResponse{Data: map[string]int{"x": 1}},
			want: `{"data":{"x":1}}`,
		},
		{
			name: "error only",
			in:   HTTPResponse{Error: errors.New("boom")},
			want: `{"data":null,"error":"boom"}`,
		},
		{
			name: "data and error",
			in:   HTTPResponse{Data: 42, Error: errors.New("oops")},
			want: `{"data":42,"error":"oops"}`,
		},
		{
			name: "nil",
			in:   HTTPResponse{},
			want: `{"data":null}`,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			b, err := json.Marshal(tc.in)
			if err != nil {
				t.Fatalf("marshal: %v", err)
			}
			if string(b) != tc.want {
				t.Fatalf("got %s, want %s", b, tc.want)
			}
		})
	}
}

func TestErrorWritesEnvelope(t *testing.T) {
	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/", nil)
	Error(w, r, 400, errors.New("bad"))
	if w.Code != 400 {
		t.Fatalf("status: got %d want 400", w.Code)
	}
	body := w.Body.String()
	if !strings.Contains(body, `"error":"bad"`) {
		t.Fatalf("body missing error: %s", body)
	}
}

func TestOKWritesEnvelope(t *testing.T) {
	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/", nil)
	OK(w, r, map[string]string{"hi": "yo"})
	if w.Code != 200 {
		t.Fatalf("status: got %d want 200", w.Code)
	}
	if !strings.Contains(w.Body.String(), `"data":{"hi":"yo"}`) {
		t.Fatalf("body: %s", w.Body.String())
	}
}

func TestCreatedSets201(t *testing.T) {
	w := httptest.NewRecorder()
	r := httptest.NewRequest("POST", "/", nil)
	Created(w, r, "ok")
	if w.Code != 201 {
		t.Fatalf("status: got %d want 201", w.Code)
	}
}
