package gresbase

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"
)

// Client is the Go SDK client for Gresbase.
type Client struct {
	BaseURL    string
	HTTPClient *http.Client
	Token      string
}

// NewClient creates a new Gresbase SDK client.
func NewClient(baseURL string) *Client {
	return &Client{
		BaseURL: baseURL,
		HTTPClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

// SetToken sets the authentication token.
func (c *Client) SetToken(token string) {
	c.Token = token
}

// do performs an HTTP request.
func (c *Client) do(method, path string, body any, result any) error {
	var bodyReader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("failed to marshal body: %w", err)
		}
		bodyReader = bytes.NewReader(data)
	}

	req, err := http.NewRequest(method, c.BaseURL+path, bodyReader)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	if c.Token != "" {
		req.Header.Set("Authorization", "Bearer "+c.Token)
	}

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		var apiErr struct {
			Error struct {
				Message string `json:"message"`
			} `json:"error"`
		}
		json.NewDecoder(resp.Body).Decode(&apiErr)
		msg := apiErr.Error.Message
		if msg == "" {
			msg = fmt.Sprintf("HTTP %d", resp.StatusCode)
		}
		return fmt.Errorf("api error: %s", msg)
	}

	if result != nil {
		if err := json.NewDecoder(resp.Body).Decode(result); err != nil {
			return fmt.Errorf("failed to decode response: %w", err)
		}
	}

	return nil
}

// Health checks the server health.
func (c *Client) Health() (map[string]any, error) {
	var result map[string]any
	err := c.do("GET", "/api/v1/health", nil, &result)
	return result, err
}

// Login authenticates with email and password.
func (c *Client) Login(email, password string) (*AuthResponse, error) {
	var result AuthResponse
	err := c.do("POST", "/api/v1/auth/login", map[string]string{
		"email":    email,
		"password": password,
	}, &result)
	if err != nil {
		return nil, err
	}
	c.SetToken(result.Token)
	return &result, nil
}

// Record represents a record in a collection.
type Record map[string]any

// AuthResponse is the response from login/refresh.
type AuthResponse struct {
	Token        string `json:"token"`
	RefreshToken string `json:"refreshToken"`
	Admin        any    `json:"admin"`
}

// ListResult is a paginated list response.
type ListResult struct {
	Items      []Record `json:"items"`
	Page       int      `json:"page"`
	PerPage    int      `json:"perPage"`
	TotalItems int64    `json:"totalItems"`
	TotalPages int      `json:"totalPages"`
}

// GetRecords fetches records from a collection.
func (c *Client) GetRecords(collection string, params url.Values) (*ListResult, error) {
	path := fmt.Sprintf("/api/v1/records/%s", collection)
	if len(params) > 0 {
		path += "?" + params.Encode()
	}

	var result ListResult
	err := c.do("GET", path, nil, &result)
	return &result, err
}

// CreateRecord creates a new record.
func (c *Client) CreateRecord(collection string, data Record) (Record, error) {
	var result Record
	err := c.do("POST", fmt.Sprintf("/api/v1/records/%s", collection), data, &result)
	return result, err
}

// UpdateRecord updates an existing record.
func (c *Client) UpdateRecord(collection string, id string, data Record) (Record, error) {
	var result Record
	err := c.do("PUT", fmt.Sprintf("/api/v1/records/%s/%s", collection, id), data, &result)
	return result, err
}

// DeleteRecord deletes a record.
func (c *Client) DeleteRecord(collection string, id string) error {
	return c.do("DELETE", fmt.Sprintf("/api/v1/records/%s/%s", collection, id), nil, nil)
}

// GetCollections fetches all collections.
func (c *Client) GetCollections() ([]map[string]any, error) {
	var result []map[string]any
	err := c.do("GET", "/api/v1/collections", nil, &result)
	return result, err
}
