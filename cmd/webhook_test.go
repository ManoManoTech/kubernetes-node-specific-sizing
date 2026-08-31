package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	admissionv1 "k8s.io/api/admission/v1"
	"k8s.io/apimachinery/pkg/runtime"
)

func TestServe_EmptyBody(t *testing.T) {
	whsvr := &WebhookServer{}
	req := httptest.NewRequest(http.MethodPost, "/mutate", nil)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	whsvr.serve(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected status %d, got %d", http.StatusBadRequest, rec.Code)
	}
}

func TestServe_WrongContentType(t *testing.T) {
	whsvr := &WebhookServer{}
	req := httptest.NewRequest(http.MethodPost, "/mutate", strings.NewReader("{}"))
	req.Header.Set("Content-Type", "text/plain")
	rec := httptest.NewRecorder()

	whsvr.serve(rec, req)

	if rec.Code != http.StatusUnsupportedMediaType {
		t.Fatalf("expected status %d, got %d", http.StatusUnsupportedMediaType, rec.Code)
	}
}

func TestServe_UndecodableBody(t *testing.T) {
	whsvr := &WebhookServer{}
	req := httptest.NewRequest(http.MethodPost, "/mutate", strings.NewReader("not valid json"))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	whsvr.serve(rec, req)

	// serve always responds 200 with an AdmissionReview carrying the error in its Response.Result,
	// even when the incoming body could not be decoded.
	if rec.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, rec.Code)
	}

	var ar admissionv1.AdmissionReview
	if err := json.Unmarshal(rec.Body.Bytes(), &ar); err != nil {
		t.Fatalf("could not unmarshal response body: %v", err)
	}
	if ar.Response == nil || ar.Response.Result == nil || ar.Response.Result.Message == "" {
		t.Fatalf("expected an error message in the response, got %+v", ar.Response)
	}
}

func TestMutate_UnmarshalFailure(t *testing.T) {
	whsvr := &WebhookServer{}
	ar := &admissionv1.AdmissionReview{
		Request: &admissionv1.AdmissionRequest{
			Object: runtime.RawExtension{Raw: []byte("not valid json")},
		},
	}

	response := whsvr.mutate(context.Background(), ar)

	if response.Allowed {
		t.Fatal("expected the response to not be allowed")
	}
	if response.Result == nil || response.Result.Message == "" {
		t.Fatalf("expected an error message in the response, got %+v", response)
	}
}
