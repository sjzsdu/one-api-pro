// video_asset.go 视频资产安全流式落盘
// Streams generated video to server-owned storage without exposing signed URLs or credentials.
// 版本: v0.0.28
// 日期: 2026-11-23
// 作者: Codex
package ali

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// DownloadVideoAsset stores bounded media using a validated server-generated storage key.
// 版本: v0.0.28
// 日期: 2026-11-23
func DownloadVideoAsset(ctx context.Context, rawURL, dir, key string) (int64, string, error) {
	if key == "" || filepath.Base(key) != key {
		return 0, "", errors.New("invalid storage key")
	}
	u, err := url.Parse(rawURL)
	if err != nil {
		return 0, "", errors.New("invalid video URL")
	}
	if err = validateMediaURL(u); err != nil {
		return 0, "", err
	}
	ctx, cancel := context.WithTimeout(ctx, 90*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, "GET", u.String(), nil)
	if err != nil {
		return 0, "", errors.New("invalid video request")
	}
	resp, err := mediaHTTPClient.Do(req)
	if err != nil {
		return 0, "", errors.New("video download failed")
	}
	defer resp.Body.Close()
	const limit int64 = 512 << 20
	if resp.StatusCode != 200 || resp.ContentLength > limit {
		return 0, "", errors.New("invalid video download status or size")
	}
	typ := strings.Split(resp.Header.Get("Content-Type"), ";")[0]
	if typ != "" && typ != "application/octet-stream" && !strings.HasPrefix(typ, "video/") {
		return 0, "", errors.New("upstream asset is not video")
	}
	if err = os.MkdirAll(dir, 0700); err != nil {
		return 0, "", errors.New("asset storage unavailable")
	}
	file, err := os.CreateTemp(dir, ".video-*")
	if err != nil {
		return 0, "", errors.New("asset storage unavailable")
	}
	temporary := file.Name()
	defer os.Remove(temporary)
	n, copyErr := io.Copy(file, io.LimitReader(resp.Body, limit+1))
	closeErr := file.Close()
	if copyErr != nil || closeErr != nil || n == 0 || n > limit {
		return 0, "", errors.New("invalid or incomplete video download")
	}
	if err = os.Rename(temporary, filepath.Join(dir, key)); err != nil {
		return 0, "", errors.New("cannot finalize video asset")
	}
	if typ == "" || typ == "application/octet-stream" {
		typ = "video/mp4"
	}
	return n, typ, nil
}
