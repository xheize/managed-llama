package hf

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"managed-llama/internal/models"
)

type Model struct {
	ID           string    `json:"id"`
	Downloads    int64     `json:"downloads"`
	Likes        int64     `json:"likes"`
	LastModified time.Time `json:"lastModified"`
	Tags         []string  `json:"tags"`
}
type File struct {
	Name string `json:"name"`
	Size int64  `json:"size"`
}
type Job struct {
	ID          string    `json:"id"`
	Repo        string    `json:"repo"`
	File        string    `json:"file"`
	Destination string    `json:"destination"`
	State       string    `json:"state"`
	Error       string    `json:"error,omitempty"`
	Downloaded  int64     `json:"downloaded"`
	Total       int64     `json:"total"`
	StartedAt   time.Time `json:"started_at"`
}

type Client struct {
	Base, Token string
	HTTP        *http.Client
	mu          sync.Mutex
	jobs        map[string]*Job
}

func New(base, token string) *Client {
	return &Client{Base: strings.TrimRight(base, "/"), Token: token, HTTP: &http.Client{Timeout: 20 * time.Second}, jobs: map[string]*Job{}}
}

// Configure updates credentials without discarding in-flight download jobs.
func (c *Client) Configure(base, token string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.Base = strings.TrimRight(base, "/")
	c.Token = token
}

func (c *Client) settings() (string, string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.Base, c.Token
}

func (c *Client) Enabled() bool {
	_, token := c.settings()
	return strings.TrimSpace(token) != ""
}

func (c *Client) Search(ctx context.Context, query string, limit int) ([]Model, error) {
	if limit < 1 || limit > 50 {
		limit = 20
	}
	v := url.Values{"filter": {"gguf"}, "sort": {"downloads"}, "direction": {"-1"}, "limit": {fmt.Sprint(limit)}, "full": {"true"}}
	if query != "" {
		v.Set("search", query)
	}
	base, token := c.settings()
	var result []Model
	if err := c.getJSON(ctx, base+"/api/models?"+v.Encode(), token, &result); err != nil {
		return nil, err
	}
	return result, nil
}

func (c *Client) Files(ctx context.Context, repo string) ([]File, error) {
	var response struct {
		Siblings []struct {
			RFilename string `json:"rfilename"`
			Size      int64  `json:"size"`
			LFS       *struct {
				Size int64 `json:"size"`
			} `json:"lfs"`
		} `json:"siblings"`
	}
	base, token := c.settings()
	if err := c.getJSON(ctx, base+"/api/models/"+escapeRepo(repo), token, &response); err != nil {
		return nil, err
	}
	var files []File
	for _, item := range response.Siblings {
		if strings.EqualFold(filepath.Ext(item.RFilename), ".gguf") {
			size := item.Size
			if item.LFS != nil && item.LFS.Size > 0 {
				size = item.LFS.Size
			}
			files = append(files, File{Name: item.RFilename, Size: size})
		}
	}
	return files, nil
}

func (c *Client) StartDownload(repo, file, destination string) (*Job, error) {
	if repo == "" || file == "" {
		return nil, errors.New("repo and file are required")
	}
	if _, err := os.Stat(destination); err == nil {
		return nil, fmt.Errorf("destination already exists: %s", filepath.Base(destination))
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	c.mu.Lock()
	for _, active := range c.jobs {
		if active.Destination == destination && (active.State == "queued" || active.State == "downloading") {
			c.mu.Unlock()
			return nil, errors.New("the same file is already downloading")
		}
	}
	id := fmt.Sprintf("%d", time.Now().UnixNano())
	job := &Job{ID: id, Repo: repo, File: file, Destination: destination, State: "queued", StartedAt: time.Now()}
	c.jobs[id] = job
	c.mu.Unlock()
	copy := *job
	go c.download(job)
	return &copy, nil
}

func (c *Client) Jobs() []Job {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]Job, 0, len(c.jobs))
	for _, job := range c.jobs {
		out = append(out, *job)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].StartedAt.After(out[j].StartedAt) })
	return out
}

func (c *Client) download(job *Job) {
	c.update(job.ID, func(j *Job) { j.State = "downloading" })
	base, token := c.settings()
	downloadURL := base + "/" + escapeRepo(job.Repo) + "/resolve/main/" + escapePath(job.File) + "?download=true"
	req, _ := http.NewRequest(http.MethodGet, downloadURL, nil)
	authorize(req, token)
	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		c.fail(job.ID, err)
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		c.fail(job.ID, fmt.Errorf("Hugging Face returned %s", resp.Status))
		return
	}
	if err := os.MkdirAll(filepath.Dir(job.Destination), 0o755); err != nil {
		c.fail(job.ID, err)
		return
	}
	tmp := job.Destination + ".part"
	out, err := os.Create(tmp)
	if err != nil {
		c.fail(job.ID, err)
		return
	}
	c.update(job.ID, func(j *Job) { j.Total = resp.ContentLength })
	w := &progressWriter{writer: out, update: func(n int64) { c.update(job.ID, func(j *Job) { j.Downloaded = n }) }}
	_, err = io.Copy(w, resp.Body)
	closeErr := out.Close()
	if err == nil {
		err = closeErr
	}
	if err == nil {
		if _, valid := models.Inspect(tmp); !valid {
			err = errors.New("downloaded file does not contain a supported GGUF header")
		}
	}
	if err == nil {
		err = os.Rename(tmp, job.Destination)
	}
	if err != nil {
		os.Remove(tmp)
		c.fail(job.ID, err)
		return
	}
	c.update(job.ID, func(j *Job) {
		j.Downloaded = w.written
		j.State = "completed"
	})
}

func (c *Client) getJSON(ctx context.Context, endpoint, token string, target any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return err
	}
	authorize(req, token)
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("Hugging Face returned %s", resp.Status)
	}
	return json.NewDecoder(io.LimitReader(resp.Body, 10<<20)).Decode(target)
}
func authorize(req *http.Request, token string) {
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	req.Header.Set("User-Agent", "managed-llama/0.1")
}
func (c *Client) update(id string, fn func(*Job)) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if job := c.jobs[id]; job != nil {
		fn(job)
	}
}
func (c *Client) fail(id string, err error) {
	c.update(id, func(j *Job) { j.State = "failed"; j.Error = err.Error() })
}
func escapeRepo(repo string) string {
	parts := strings.Split(repo, "/")
	for i := range parts {
		parts[i] = url.PathEscape(parts[i])
	}
	return strings.Join(parts, "/")
}
func escapePath(path string) string {
	parts := strings.Split(filepath.ToSlash(path), "/")
	for i := range parts {
		parts[i] = url.PathEscape(parts[i])
	}
	return strings.Join(parts, "/")
}

type progressWriter struct {
	writer  io.Writer
	written int64
	update  func(int64)
	last    time.Time
}

func (w *progressWriter) Write(p []byte) (int, error) {
	n, err := w.writer.Write(p)
	w.written += int64(n)
	if time.Since(w.last) > 250*time.Millisecond || err != nil {
		w.update(w.written)
		w.last = time.Now()
	}
	return n, err
}
