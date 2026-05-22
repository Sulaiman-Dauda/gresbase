package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"

	"github.com/gresbase/gresbase/internal/app"
	"github.com/jackc/pgx/v5"
	"github.com/rs/zerolog/log"
)

// BatchRequest represents a single request in a batch.
type BatchRequest struct {
	Method  string            `json:"method"`
	URL     string            `json:"url"`
	Headers map[string]string `json:"headers,omitempty"`
	Body    json.RawMessage   `json:"body,omitempty"`
}

// BatchResponse represents a single response in a batch.
type BatchResponse struct {
	Status  int               `json:"status"`
	Headers map[string]string `json:"headers,omitempty"`
	Body    json.RawMessage   `json:"body,omitempty"`
	Error   string            `json:"error,omitempty"`
}

// BatchPayload is the top-level batch request/response wrapper.
type BatchPayload struct {
	Requests  []BatchRequest  `json:"requests"`
	Responses []BatchResponse `json:"responses,omitempty"`
}

// HandleBatch processes a batch of requests sequentially within a transaction.
// POST /api/v1/batch
func (h *Handlers) HandleBatch(w http.ResponseWriter, r *http.Request) {
	var payload BatchPayload
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		writeError(w, 400, "Invalid batch payload")
		return
	}

	if len(payload.Requests) == 0 {
		writeError(w, 400, "Batch must contain at least one request")
		return
	}

	if len(payload.Requests) > 100 {
		writeError(w, 400, "Batch limit is 100 requests")
		return
	}

	// Execute in a database transaction if requested
	useTransaction := r.URL.Query().Get("transactional") == "true"

	var responses []BatchResponse
	appCtx := h.app

	if useTransaction {
		responses = executeBatchTransactional(appCtx, r, payload.Requests)
	} else {
		responses = executeBatch(appCtx, r, payload.Requests)
	}

	writeJSON(w, 200, BatchPayload{
		Requests:  payload.Requests,
		Responses: responses,
	})
}

// executeBatch runs batch requests sequentially.
func executeBatch(appCtx *app.App, originalReq *http.Request, requests []BatchRequest) []BatchResponse {
	responses := make([]BatchResponse, len(requests))

	for i, req := range requests {
		responses[i] = executeSingleRequest(appCtx, originalReq, req)
	}

	return responses
}

// executeBatchTransactional runs batch requests within a single DB transaction.
func executeBatchTransactional(appCtx *app.App, originalReq *http.Request, requests []BatchRequest) []BatchResponse {
	responses := make([]BatchResponse, len(requests))

	err := appCtx.DB().RunInTransaction(context.Background(), func(tx pgx.Tx) error {
		for i, req := range requests {
			resp := executeSingleRequest(appCtx, originalReq, req)
			responses[i] = resp

			// If any request fails, abort the entire batch
			if resp.Status >= 400 {
				return fmt.Errorf("batch request %d failed with status %d: %s", i, resp.Status, resp.Error)
			}
		}
		return nil
	})

	if err != nil {
		// Mark remaining unprocessed responses as errors
		for i := range responses {
			if responses[i].Status == 0 {
				responses[i] = BatchResponse{
					Status: 500,
					Error:  fmt.Sprintf("Batch transaction aborted: %v", err),
				}
			}
		}
	}

	return responses
}

// executeSingleRequest executes a single sub-request using the app's internal router.
func executeSingleRequest(appCtx *app.App, originalReq *http.Request, req BatchRequest) BatchResponse {
	if req.Method == "" {
		req.Method = "GET"
	}

	url := req.URL
	if !strings.HasPrefix(url, "/") {
		url = "/" + url
	}

	// Ensure URL is within API boundaries
	if !strings.HasPrefix(url, "/api/") && url != "/health" {
		return BatchResponse{
			Status: 403,
			Error:  "Batch requests must target /api/ endpoints",
		}
	}

	var bodyReader io.Reader
	if req.Body != nil {
		bodyReader = bytes.NewReader(req.Body)
	}

	httpReq := httptest.NewRequest(req.Method, url, bodyReader)
	httpReq = httpReq.WithContext(originalReq.Context())

	// Copy auth headers from original request
	if auth := originalReq.Header.Get("Authorization"); auth != "" {
		httpReq.Header.Set("Authorization", auth)
	}
	if ct := originalReq.Header.Get("Content-Type"); ct != "" {
		httpReq.Header.Set("Content-Type", ct)
	}

	// Custom headers
	for k, v := range req.Headers {
		httpReq.Header.Set(k, v)
	}

	// Create response recorder
	w := httptest.NewRecorder()

	// Serve using the app's internal router
	appCtx.ServeHTTP(w, httpReq)

	// Build response
	resp := BatchResponse{
		Status: w.Code,
	}

	// Extract headers
	if headers := w.Header(); len(headers) > 0 {
		respHeaders := make(map[string]string)
		for k := range headers {
			respHeaders[k] = headers.Get(k)
		}
		resp.Headers = respHeaders
	}

	// Extract body
	body := w.Body.Bytes()
	if len(body) > 0 {
		resp.Body = json.RawMessage(body)
	}

	// Extract error from error responses
	if w.Code >= 400 {
		var errResp map[string]any
		if json.Unmarshal(body, &errResp) == nil {
			if msg, ok := errResp["message"].(string); ok {
				resp.Error = msg
			} else if msg, ok := errResp["error"].(string); ok {
				resp.Error = msg
			}
		}
	}

	return resp
}

// ---------------------------------------------------------------------------
// Parallel Batch Execution (experimental)
// ---------------------------------------------------------------------------

// ExecuteParallelBatch runs independent batch requests concurrently.
// Transactions are NOT supported in parallel mode.
func ExecuteParallelBatch(appCtx *app.App, originalReq *http.Request, requests []BatchRequest) []BatchResponse {
	responses := make([]BatchResponse, len(requests))
	var wg sync.WaitGroup
	var mu sync.Mutex

	for i, req := range requests {
		wg.Add(1)
		go func(idx int, r BatchRequest) {
			defer wg.Done()

			resp := executeSingleRequest(appCtx, originalReq, r)

			mu.Lock()
			responses[idx] = resp
			mu.Unlock()
		}(i, req)
	}

	wg.Wait()
	return responses
}

// Ensure unused import compiles
var _ = log.Logger
