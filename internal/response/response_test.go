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
			name: "message only",
			in:   HTTPResponse{Message: "boom"},
			want: `{"data":null,"error":"boom"}`,
		},
		{
			name: "data and message",
			in:   HTTPResponse{Data: 42, Message: "oops"},
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

func TestErrorReturnsClientMessageAndHidesInternal(t *testing.T) {
	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/", nil)
	Error(w, r, 400, "bad input", errors.New("internal: pgx: foreign key violation on table xyz"))
	if w.Code != 400 {
		t.Fatalf("status: got %d want 400", w.Code)
	}
	body := w.Body.String()
	if !strings.Contains(body, `"error":"bad input"`) {
		t.Fatalf("body missing client message: %s", body)
	}
	if strings.Contains(body, "pgx") || strings.Contains(body, "foreign key") {
		t.Fatalf("internal error leaked to client: %s", body)
	}
}

func TestErrorDefaultsToStatusText(t *testing.T) {
	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/", nil)
	Error(w, r, 500, "", errors.New("boom"))
	if !strings.Contains(w.Body.String(), `"error":"Internal Server Error"`) {
		t.Fatalf("body: %s", w.Body.String())
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
