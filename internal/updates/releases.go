// Package updates reads public GitHub releases and verifies downloaded assets.
package updates

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"
)

const ExampleRepository = "https://github.com/OWNER/managed-llama"
const MaxBinarySize int64 = 256 << 20

var repositoryPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9-]*/[A-Za-z0-9_.-]+$`)
var versionPattern = regexp.MustCompile(`^v?(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$`)

func Repository(value string) (string, error) {
	value = strings.TrimSuffix(strings.TrimSpace(value), "/")
	value = strings.TrimPrefix(value, "https://github.com/")
	if !repositoryPattern.MatchString(value) || strings.Contains(value, "..") {
		return "", errors.New("업데이트 주소는 https://github.com/OWNER/REPO 형식이어야 합니다")
	}
	return value, nil
}

func Configured(value string) bool {
	repo, err := Repository(value)
	return err == nil && !strings.EqualFold(strings.Split(repo, "/")[0], "OWNER")
}

// Releases use stable vMAJOR.MINOR.PATCH tags. Numeric comparison prevents
// lexicographic downgrades (for example v1.10.0 versus v1.9.0).
func Newer(latest, current string) (bool, error) {
	parse := func(v string) ([3]uint64, error) {
		var parts [3]uint64
		match := versionPattern.FindStringSubmatch(v)
		if match == nil {
			return parts, errors.New("릴리스 버전은 v1.2.3 형식이어야 합니다")
		}
		for i := range parts {
			n, err := strconv.ParseUint(match[i+1], 10, 64)
			if err != nil {
				return parts, err
			}
			parts[i] = n
		}
		return parts, nil
	}
	next, err := parse(latest)
	if err != nil {
		return false, err
	}
	if current == "dev" {
		return true, nil
	}
	old, err := parse(current)
	if err != nil {
		return false, err
	}
	for i := range next {
		if next[i] != old[i] {
			return next[i] > old[i], nil
		}
	}
	return false, nil
}

type Release struct {
	Tag        string  `json:"tag_name"`
	Draft      bool    `json:"draft"`
	Prerelease bool    `json:"prerelease"`
	Assets     []Asset `json:"assets"`
}
type Asset struct {
	Name   string `json:"name"`
	URL    string `json:"browser_download_url"`
	Size   int64  `json:"size"`
	Digest string `json:"digest"`
}
type Candidate struct {
	Version string
	Asset   Asset
}

func NewClient() *http.Client {
	return &http.Client{Timeout: 10 * time.Minute, CheckRedirect: func(req *http.Request, via []*http.Request) error {
		if len(via) >= 5 {
			return errors.New("too many download redirects")
		}
		host := req.URL.Hostname()
		if req.URL.Scheme != "https" || req.URL.User != nil || req.URL.Port() != "" ||
			(host != "github.com" && host != "api.github.com" && host != "release-assets.githubusercontent.com" && host != "objects.githubusercontent.com") {
			return errors.New("release redirected outside GitHub HTTPS hosts")
		}
		return nil
	}}
}

func Latest(ctx context.Context, client *http.Client, repository, arch string) (Candidate, error) {
	var candidate Candidate
	repo, err := Repository(repository)
	if err != nil {
		return candidate, err
	}
	if !Configured(repository) {
		return candidate, errors.New("예시 URL입니다. 실제 릴리스 저장소를 설정하세요")
	}
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, "GET", "https://api.github.com/repos/"+repo+"/releases/latest", nil)
	if err != nil {
		return candidate, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	req.Header.Set("User-Agent", "Managed-Llama-Updater")
	res, err := client.Do(req)
	if err != nil {
		return candidate, err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return candidate, fmt.Errorf("GitHub 릴리스 조회 실패 (%d): 공개 릴리스와 API 제한을 확인하세요", res.StatusCode)
	}
	var release Release
	if err := json.NewDecoder(io.LimitReader(res.Body, 2<<20)).Decode(&release); err != nil {
		return candidate, err
	}
	if release.Draft || release.Prerelease {
		return candidate, errors.New("정식 공개 릴리스만 설치할 수 있습니다")
	}
	if _, err := Newer(release.Tag, "dev"); err != nil {
		return candidate, err
	}
	name := "managed-llama-windows-" + arch + ".exe"
	for _, asset := range release.Assets {
		if asset.Name != name {
			continue
		}
		expected := "https://github.com/" + repo + "/releases/download/" + url.PathEscape(release.Tag) + "/" + name
		if asset.URL != expected {
			return candidate, errors.New("릴리스 파일 주소가 저장소와 일치하지 않습니다")
		}
		digest, err := hex.DecodeString(strings.TrimPrefix(asset.Digest, "sha256:"))
		if !strings.HasPrefix(asset.Digest, "sha256:") || err != nil || len(digest) != sha256.Size {
			return candidate, errors.New("릴리스 파일에 SHA-256 digest가 필요합니다")
		}
		if asset.Size <= 0 || asset.Size > MaxBinarySize {
			return candidate, errors.New("릴리스 파일 크기가 허용 범위를 벗어났습니다")
		}
		return Candidate{Version: release.Tag, Asset: asset}, nil
	}
	return candidate, fmt.Errorf("릴리스에 %s 파일이 없습니다", name)
}

func Download(ctx context.Context, client *http.Client, candidate Candidate, dir string, progress func(int64)) (string, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", candidate.Asset.URL, nil)
	if err != nil {
		return "", err
	}
	res, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return "", fmt.Errorf("업데이트 다운로드 실패 (%d)", res.StatusCode)
	}
	file, err := os.CreateTemp(dir, ".managed-llama-update-*.exe")
	if err != nil {
		return "", err
	}
	ok := false
	defer func() {
		file.Close()
		if !ok {
			os.Remove(file.Name())
		}
	}()
	hash := sha256.New()
	counter := &progressWriter{notify: progress}
	n, err := io.Copy(io.MultiWriter(file, hash, counter), io.LimitReader(res.Body, candidate.Asset.Size+1))
	if err != nil {
		return "", err
	}
	if n != candidate.Asset.Size || "sha256:"+hex.EncodeToString(hash.Sum(nil)) != strings.ToLower(candidate.Asset.Digest) {
		return "", errors.New("업데이트 파일 크기 또는 SHA-256 검증에 실패했습니다")
	}
	if err := file.Sync(); err != nil {
		return "", err
	}
	if err := file.Close(); err != nil {
		return "", err
	}
	ok = true
	return file.Name(), nil
}

type progressWriter struct {
	count  int64
	notify func(int64)
}

func (w *progressWriter) Write(p []byte) (int, error) {
	w.count += int64(len(p))
	if w.notify != nil {
		w.notify(w.count)
	}
	return len(p), nil
}
