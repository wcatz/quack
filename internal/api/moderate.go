package api

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"
)

func (s *Server) handleModerate(w http.ResponseWriter, r *http.Request) {
	apiKey := r.Header.Get("X-Anthropic-Key")
	if apiKey == "" {
		http.Error(w, `{"error":"X-Anthropic-Key header required"}`, http.StatusBadRequest)
		return
	}

	model := r.URL.Query().Get("model")
	if model == "" {
		model = "claude-haiku-4-5-20251001"
	}

	dryRun := r.URL.Query().Get("dry_run") == "true"
	filterType := r.URL.Query().Get("filter") // "images", "gifs", or "" for all

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, `{"error":"streaming not supported"}`, http.StatusInternalServerError)
		return
	}

	keys := s.scheduler.AllKeys()

	if filterType != "" {
		var filtered []string
		for _, k := range keys {
			if filterType == "images" && strings.HasPrefix(k, "images/") {
				filtered = append(filtered, k)
			} else if filterType == "gifs" && strings.HasPrefix(k, "gifs/") {
				filtered = append(filtered, k)
			}
		}
		keys = filtered
	}

	total := len(keys)

	w.Header().Set("Content-Type", "application/x-ndjson")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(http.StatusOK)

	json.NewEncoder(w).Encode(map[string]interface{}{
		"status": "started", "total": total, "model": model, "dry_run": dryRun,
	})
	flusher.Flush()

	type result struct {
		Key    string
		Action string
		Class  string
		Error  string
	}

	results := make(chan result, 20)

	go func() {
		sem := make(chan struct{}, 5)
		var wg sync.WaitGroup

	loop:
		for _, key := range keys {
			if r.Context().Err() != nil {
				break loop
			}

			select {
			case <-r.Context().Done():
				break loop
			case sem <- struct{}{}:
			}

			wg.Add(1)
			go func(key string) {
				defer wg.Done()
				defer func() { <-sem }()

				res := result{Key: key}

				data, err := s.s3.ReadAll(r.Context(), key)
				if err != nil {
					res.Action = "error"
					res.Error = err.Error()
					results <- res
					return
				}

				if len(data) > 10*1024*1024 {
					res.Action = "skipped"
					res.Error = "too large"
					results <- res
					return
				}

				ct := detectMediaType(key)

				class, err := classifyImage(r.Context(), apiKey, data, ct, model)
				if err != nil {
					res.Action = "error"
					res.Error = err.Error()
					results <- res
					return
				}

				res.Class = class

				if strings.Contains(class, "cartoon") {
					if !dryRun {
						if err := s.s3.Delete(r.Context(), key); err != nil {
							res.Action = "error"
							res.Error = err.Error()
							results <- res
							return
						}
						s.scheduler.DeleteKey(key)
					}
					res.Action = "deleted"
				} else {
					res.Action = "kept"
				}

				results <- res
			}(key)
		}

		wg.Wait()
		close(results)
	}()

	var deleted, kept, errored, processed int
	for res := range results {
		processed++
		switch res.Action {
		case "deleted":
			deleted++
		case "kept":
			kept++
		default:
			errored++
		}

		evt := map[string]interface{}{
			"key":      res.Key,
			"action":   res.Action,
			"progress": processed,
			"total":    total,
			"deleted":  deleted,
			"kept":     kept,
			"errored":  errored,
		}
		if res.Class != "" {
			evt["class"] = res.Class
		}
		if res.Error != "" {
			evt["error"] = res.Error
		}
		json.NewEncoder(w).Encode(evt)
		flusher.Flush()
	}

	json.NewEncoder(w).Encode(map[string]interface{}{
		"status":  "completed",
		"deleted": deleted,
		"kept":    kept,
		"errored": errored,
		"total":   total,
	})
	flusher.Flush()

	s.logger.Info("moderation completed",
		"deleted", deleted, "kept", kept, "errored", errored, "total", total, "dry_run", dryRun,
	)
}

func detectMediaType(key string) string {
	lower := strings.ToLower(key)
	switch {
	case strings.HasSuffix(lower, ".png"):
		return "image/png"
	case strings.HasSuffix(lower, ".gif"):
		return "image/gif"
	case strings.HasSuffix(lower, ".webp"):
		return "image/webp"
	default:
		return "image/jpeg"
	}
}

func classifyImage(ctx context.Context, apiKey string, data []byte, mediaType, model string) (string, error) {
	b64 := base64.StdEncoding.EncodeToString(data)

	reqBody, err := json.Marshal(map[string]interface{}{
		"model":      model,
		"max_tokens": 10,
		"messages": []map[string]interface{}{
			{
				"role": "user",
				"content": []map[string]interface{}{
					{
						"type": "image",
						"source": map[string]interface{}{
							"type":       "base64",
							"media_type": mediaType,
							"data":       b64,
						},
					},
					{
						"type": "text",
						"text": "Is this a photograph of a real duck, duckling, or waterfowl? Reply 'photo' if it shows real birds in a real photo. Reply 'cartoon' if it is a cartoon, illustration, drawing, digital art, meme, clipart, or any non-photographic image. Reply ONLY 'photo' or 'cartoon'.",
					},
				},
			},
		},
	})
	if err != nil {
		return "", err
	}

	for attempt := 0; attempt < 3; attempt++ {
		class, retryable, err := callClaude(ctx, apiKey, reqBody)
		if err != nil {
			if retryable && attempt < 2 {
				select {
				case <-ctx.Done():
					return "", ctx.Err()
				case <-time.After(time.Duration(attempt+1) * 2 * time.Second):
					continue
				}
			}
			return "", err
		}
		return class, nil
	}
	return "", fmt.Errorf("max retries exceeded")
}

func callClaude(ctx context.Context, apiKey string, body []byte) (string, bool, error) {
	req, err := http.NewRequestWithContext(ctx, "POST", "https://api.anthropic.com/v1/messages", bytes.NewReader(body))
	if err != nil {
		return "", false, err
	}
	req.Header.Set("x-api-key", apiKey)
	req.Header.Set("anthropic-version", "2023-06-01")
	req.Header.Set("content-type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", false, err
	}
	defer resp.Body.Close()

	if resp.StatusCode == 429 || resp.StatusCode == 529 {
		return "", true, fmt.Errorf("rate limited: %d", resp.StatusCode)
	}

	if resp.StatusCode != 200 {
		respBody, _ := io.ReadAll(resp.Body)
		return "", false, fmt.Errorf("claude API %d: %s", resp.StatusCode, string(respBody))
	}

	var result struct {
		Content []struct {
			Text string `json:"text"`
		} `json:"content"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", false, err
	}

	if len(result.Content) == 0 {
		return "", false, fmt.Errorf("empty response")
	}

	return strings.TrimSpace(strings.ToLower(result.Content[0].Text)), false, nil
}
