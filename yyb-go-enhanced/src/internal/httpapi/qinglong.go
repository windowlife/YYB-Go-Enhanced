package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

type qingLongClient struct {
	baseURL      string
	clientID     string
	clientSecret string
	httpClient   *http.Client

	mu          sync.Mutex
	token       string
	tokenExpiry time.Time
}

type qingLongCron struct {
	ID                int64   `json:"id"`
	Name              string  `json:"name"`
	Command           string  `json:"command"`
	Schedule          string  `json:"schedule"`
	Status            float64 `json:"status"`
	IsDisabled        *int    `json:"isDisabled"`
	PID               any     `json:"pid"`
	LogPath           string  `json:"log_path"`
	LogName           string  `json:"log_name"`
	TaskBefore        string  `json:"task_before"`
	LastRunningTime   int64   `json:"last_running_time"`
	LastExecutionTime int64   `json:"last_execution_time"`
	CreatedAt         string  `json:"createdAt"`
	UpdatedAt         string  `json:"updatedAt"`
}

func (c qingLongCron) enabled() bool {
	if c.IsDisabled != nil {
		return *c.IsDisabled == 0
	}
	return c.Status == 0
}

func (c qingLongCron) running() bool {
	if c.IsDisabled != nil {
		return c.Status >= 0 && c.Status < 1
	}
	return c.PID != nil
}

type qingLongEnv struct {
	ID        int64  `json:"id"`
	Name      string `json:"name"`
	Value     string `json:"value"`
	Remarks   string `json:"remarks"`
	Status    int    `json:"status"`
	CreatedAt string `json:"createdAt"`
	UpdatedAt string `json:"updatedAt"`
}

type qingLongLogEntry struct {
	Title      string             `json:"title"`
	Key        string             `json:"key"`
	Type       string             `json:"type"`
	Parent     string             `json:"parent"`
	Size       int64              `json:"size"`
	CreateTime int64              `json:"createTime"`
	Children   []qingLongLogEntry `json:"children"`
}

type qingLongEnvelope struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data"`
}

func newQingLongClient(baseURL, clientID, clientSecret string, timeout time.Duration) *qingLongClient {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	return &qingLongClient{
		baseURL:      baseURL,
		clientID:     strings.TrimSpace(clientID),
		clientSecret: strings.TrimSpace(clientSecret),
		httpClient:   &http.Client{Timeout: timeout},
	}
}

func (c *qingLongClient) configured() bool {
	if c == nil {
		return false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.baseURL != "" && c.clientID != "" && c.clientSecret != ""
}

func (c *qingLongClient) configuration() (string, string, string) {
	if c == nil {
		return "", "", ""
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.baseURL, c.clientID, c.clientSecret
}

func (c *qingLongClient) reconfigure(baseURL, clientID, clientSecret string) {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	clientID = strings.TrimSpace(clientID)
	clientSecret = strings.TrimSpace(clientSecret)
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.baseURL == baseURL && c.clientID == clientID && c.clientSecret == clientSecret {
		return
	}
	c.baseURL = baseURL
	c.clientID = clientID
	c.clientSecret = clientSecret
	c.token = ""
	c.tokenExpiry = time.Time{}
}

func (c *qingLongClient) authenticate(ctx context.Context) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.baseURL == "" || c.clientID == "" || c.clientSecret == "" {
		return "", fmt.Errorf("青龙 OpenAPI 未配置")
	}
	if c.token != "" && time.Now().Before(c.tokenExpiry) {
		return c.token, nil
	}
	query := url.Values{"client_id": {c.clientID}, "client_secret": {c.clientSecret}}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/open/auth/token?"+query.Encode(), nil)
	if err != nil {
		return "", err
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("连接青龙失败: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return "", err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("青龙鉴权返回 HTTP %d", resp.StatusCode)
	}
	var envelope struct {
		Code int `json:"code"`
		Data struct {
			Token      string `json:"token"`
			Expiration int64  `json:"expiration"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		return "", fmt.Errorf("解析青龙鉴权响应失败: %w", err)
	}
	if envelope.Code != 0 && envelope.Code != 200 {
		return "", fmt.Errorf("青龙鉴权失败，状态码 %d", envelope.Code)
	}
	if envelope.Data.Token == "" {
		return "", fmt.Errorf("青龙鉴权未返回 token")
	}
	ttl := time.Duration(envelope.Data.Expiration) * time.Second
	if ttl <= time.Minute {
		ttl = 10 * time.Minute
	}
	c.token = envelope.Data.Token
	c.tokenExpiry = time.Now().Add(ttl - time.Minute)
	return c.token, nil
}

func (c *qingLongClient) request(ctx context.Context, method, path string, body any, out any) error {
	token, err := c.authenticate(ctx)
	if err != nil {
		return err
	}
	var reader io.Reader
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			return err
		}
		reader = bytes.NewReader(encoded)
	}
	baseURL, _, _ := c.configuration()
	req, err := http.NewRequestWithContext(ctx, method, baseURL+path, reader)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("请求青龙失败: %w", err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("青龙 API 返回 HTTP %d", resp.StatusCode)
	}
	var envelope qingLongEnvelope
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return fmt.Errorf("解析青龙响应失败: %w", err)
	}
	if envelope.Code != 0 && envelope.Code != 200 && envelope.Code != 201 {
		message := strings.TrimSpace(envelope.Message)
		if message == "" {
			message = fmt.Sprintf("状态码 %d", envelope.Code)
		}
		return fmt.Errorf("青龙 API 错误: %s", message)
	}
	if out == nil || len(envelope.Data) == 0 || string(envelope.Data) == "null" {
		return nil
	}
	if err := json.Unmarshal(envelope.Data, out); err != nil {
		return fmt.Errorf("解析青龙数据失败: %w", err)
	}
	return nil
}

func (c *qingLongClient) status(ctx context.Context) error {
	_, err := c.authenticate(ctx)
	return err
}

func (c *qingLongClient) listCrons(ctx context.Context, search string) ([]qingLongCron, error) {
	path := "/open/crons?" + url.Values{"searchValue": {search}}.Encode()
	var raw json.RawMessage
	if err := c.request(ctx, http.MethodGet, path, nil, &raw); err != nil {
		return nil, err
	}
	if len(raw) == 0 || string(raw) == "null" {
		return []qingLongCron{}, nil
	}
	var out []qingLongCron
	if err := json.Unmarshal(raw, &out); err == nil {
		return out, nil
	}
	var page struct {
		Data []qingLongCron `json:"data"`
		List []qingLongCron `json:"list"`
	}
	if err := json.Unmarshal(raw, &page); err != nil {
		return nil, err
	}
	if page.Data != nil {
		return page.Data, nil
	}
	return page.List, nil
}

func (c *qingLongClient) createCron(ctx context.Context, name, command, schedule, taskBefore, logName string) (*qingLongCron, error) {
	body := map[string]any{"name": name, "command": command, "schedule": schedule, "task_before": taskBefore, "log_name": logName}
	var raw json.RawMessage
	if err := c.request(ctx, http.MethodPost, "/open/crons", body, &raw); err != nil {
		return nil, err
	}
	var cron qingLongCron
	if err := json.Unmarshal(raw, &cron); err == nil && cron.ID != 0 {
		return &cron, nil
	}
	var list []qingLongCron
	if err := json.Unmarshal(raw, &list); err == nil && len(list) > 0 {
		return &list[0], nil
	}
	return nil, fmt.Errorf("青龙创建任务后未返回任务 ID")
}

func (c *qingLongClient) updateCron(ctx context.Context, id int64, name, command, schedule, taskBefore, logName string) error {
	body := map[string]any{"id": id, "name": name, "command": command, "schedule": schedule, "task_before": taskBefore, "log_name": logName}
	return c.request(ctx, http.MethodPut, "/open/crons", body, nil)
}

func (c *qingLongClient) setCronsEnabled(ctx context.Context, ids []int64, enabled bool) error {
	path := "/open/crons/disable"
	if enabled {
		path = "/open/crons/enable"
	}
	return c.request(ctx, http.MethodPut, path, ids, nil)
}

func (c *qingLongClient) runCrons(ctx context.Context, ids []int64) error {
	return c.request(ctx, http.MethodPut, "/open/crons/run", ids, nil)
}

func (c *qingLongClient) deleteCrons(ctx context.Context, ids []int64) error {
	if len(ids) == 0 {
		return nil
	}
	return c.request(ctx, http.MethodDelete, "/open/crons", ids, nil)
}

func (c *qingLongClient) cronLog(ctx context.Context, id int64) (string, error) {
	var log string
	if err := c.request(ctx, http.MethodGet, fmt.Sprintf("/open/crons/%d/log", id), nil, &log); err != nil {
		return "", err
	}
	return log, nil
}

func (c *qingLongClient) listLogs(ctx context.Context) ([]qingLongLogEntry, error) {
	var logs []qingLongLogEntry
	if err := c.request(ctx, http.MethodGet, "/open/logs", nil, &logs); err != nil {
		return nil, err
	}
	return logs, nil
}

func (c *qingLongClient) logDetail(ctx context.Context, path, file string) (string, error) {
	query := url.Values{"path": {path}, "file": {file}}
	var log string
	if err := c.request(ctx, http.MethodGet, "/open/logs/detail?"+query.Encode(), nil, &log); err != nil {
		return "", err
	}
	return log, nil
}

func (c *qingLongClient) listEnvs(ctx context.Context, search string) ([]qingLongEnv, error) {
	path := "/open/envs?" + url.Values{"searchValue": {search}}.Encode()
	var raw json.RawMessage
	if err := c.request(ctx, http.MethodGet, path, nil, &raw); err != nil {
		return nil, err
	}
	if len(raw) == 0 || string(raw) == "null" {
		return []qingLongEnv{}, nil
	}
	var out []qingLongEnv
	if err := json.Unmarshal(raw, &out); err == nil {
		return out, nil
	}
	var page struct {
		Data []qingLongEnv `json:"data"`
		List []qingLongEnv `json:"list"`
	}
	if err := json.Unmarshal(raw, &page); err != nil {
		return nil, err
	}
	if page.Data != nil {
		return page.Data, nil
	}
	return page.List, nil
}

func (c *qingLongClient) upsertEnv(ctx context.Context, name, value, remarks string) error {
	envs, err := c.listEnvs(ctx, name)
	if err != nil {
		return err
	}
	for _, env := range envs {
		if env.Name != name {
			continue
		}
		body := map[string]any{"id": env.ID, "name": name, "value": value, "remarks": remarks}
		if err := c.request(ctx, http.MethodPut, "/open/envs", body, nil); err != nil {
			return err
		}
		return c.setEnvsEnabled(ctx, []int64{env.ID}, true)
	}
	body := []map[string]any{{"name": name, "value": value, "remarks": remarks}}
	return c.request(ctx, http.MethodPost, "/open/envs", body, nil)
}

func (c *qingLongClient) updateEnv(ctx context.Context, env qingLongEnv, value string) error {
	body := map[string]any{"id": env.ID, "name": env.Name, "value": value, "remarks": env.Remarks}
	return c.request(ctx, http.MethodPut, "/open/envs", body, nil)
}

func (c *qingLongClient) setNamedEnvsEnabled(ctx context.Context, names []string, enabled bool) error {
	ids := make([]int64, 0, len(names))
	for _, name := range names {
		envs, err := c.listEnvs(ctx, name)
		if err != nil {
			return err
		}
		for _, env := range envs {
			if env.Name == name {
				ids = append(ids, env.ID)
			}
		}
	}
	if len(ids) == 0 {
		return nil
	}
	return c.setEnvsEnabled(ctx, ids, enabled)
}

func (c *qingLongClient) setEnvsEnabled(ctx context.Context, ids []int64, enabled bool) error {
	path := "/open/envs/disable"
	if enabled {
		path = "/open/envs/enable"
	}
	return c.request(ctx, http.MethodPut, path, ids, nil)
}
