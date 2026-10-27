package api

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestFailureSanitizesInternalErrors(t *testing.T) {
	w := httptest.NewRecorder()
	failure(w, http.StatusInternalServerError, errors.New("database path /secret leaked"))

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d", w.Code)
	}
	body := w.Body.String()
	if strings.Contains(body, "/secret") {
		t.Fatalf("internal error leaked: %s", body)
	}
	if !strings.Contains(body, `"error":"Internal Server Error"`) {
		t.Fatalf("body = %s", body)
	}
}

func TestFailureKeepsUnavailableReason(t *testing.T) {
	w := httptest.NewRecorder()
	failure(w, http.StatusServiceUnavailable, errors.New("启动配置不可用: 服务未启用"))

	body := w.Body.String()
	if !strings.Contains(body, "启动配置不可用") {
		t.Fatalf("body = %s", body)
	}
	if strings.Contains(body, "Service Unavailable") {
		t.Fatalf("controlled 503 was replaced: %s", body)
	}
}
