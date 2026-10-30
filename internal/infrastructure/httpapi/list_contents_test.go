package httpapi

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// The list card decides which verdict a switch sends from these bytes: a
// definition row is switched off by exclude, a row without the flag that
// stands on an include verdict is withdrawn by auto.
func TestListContentsNamesTheRowsOfTheDefinition(t *testing.T) {
	server, err := New("http://127.0.0.1:8765", testBackend(), nil)
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodGet, "http://127.0.0.1:8765/v1/lists/example/contents", nil)
	request.RemoteAddr = "127.0.0.1:1"
	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, request)
	body := response.Body.String()
	if response.Code != http.StatusOK {
		t.Fatalf("code=%d body=%s", response.Code, body)
	}
	if !strings.Contains(body, `{"value":"example.com","kind":"domain","origin":"catalog","enabled":true,"definition":true}`) {
		t.Fatalf("definition row lacks its flag: %s", body)
	}
	if !strings.Contains(body, `{"value":"198.51.100.7","kind":"ip","origin":"manual","enabled":true}`) {
		t.Fatalf("an added row claims the definition: %s", body)
	}
}
