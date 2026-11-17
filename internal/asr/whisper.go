package asr

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"path/filepath"
	"time"
)

type Client struct {
	BaseURL    string
	APIKey     string
	Model      string
	HTTPClient *http.Client
}

func New(baseURL, apiKey, model string) *Client {
	return &Client{
		BaseURL: baseURL, APIKey: apiKey, Model: model,
		HTTPClient: &http.Client{Timeout: 120 * time.Second},
	}
}

type Result struct {
	Text string `json:"text"`
}

func (c *Client) Transcribe(ctx context.Context, filename string, audio []byte) (string, error) {
	if c.APIKey == "" {
		return "", fmt.Errorf("ASR_API_KEY / OPENAI_API_KEY not configured")
	}
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	_ = w.WriteField("model", c.Model)
	part, err := w.CreateFormFile("file", filepath.Base(filename))
	if err != nil {
		return "", err
	}
	if _, err := part.Write(audio); err != nil {
		return "", err
	}
	_ = w.Close()

	url := c.BaseURL + "/audio/transcriptions"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, &buf)
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+c.APIKey)
	req.Header.Set("Content-Type", w.FormDataContentType())
	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 300 {
		return "", fmt.Errorf("asr status %d: %s", resp.StatusCode, string(raw))
	}
	var out Result
	if err := json.Unmarshal(raw, &out); err != nil {
		return "", err
	}
	return out.Text, nil
}
