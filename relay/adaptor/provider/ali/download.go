// download.go 安全获取百炼生成的音频与图片
// Downloads untrusted media URLs without credentials or private network access.
// 版本: v0.0.28
// 日期: 2026-11-23
// 作者: Codex
package ali

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"time"

	"github.com/modelbus/one-api-pro/common/network"
)

const maxMediaBytes = 32 << 20

// Generated media is untrusted. Resolve and pin public addresses at dial time;
// deliberately do not use RELAY_PROXY, whose DNS resolution cannot be checked.
var mediaHTTPClient = &http.Client{
	Timeout:   60 * time.Second,
	Transport: &http.Transport{DialContext: dialPublicMedia, TLSHandshakeTimeout: 10 * time.Second, ResponseHeaderTimeout: 20 * time.Second},
	CheckRedirect: func(req *http.Request, via []*http.Request) error {
		if len(via) > 5 {
			return errors.New("too many media redirects")
		}
		req.Header.Del("Authorization")
		req.Header.Del("Cookie")
		req.Header.Del("Referer")
		return validateMediaURL(req.URL)
	},
}

func validateMediaURL(u *url.URL) error {
	if (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" || u.User != nil {
		return errors.New("invalid media URL")
	}
	if ip := net.ParseIP(u.Hostname()); ip != nil && network.IsPrivateIP(ip) {
		return errors.New("private media address is forbidden")
	}
	return nil
}

func dialPublicMedia(ctx context.Context, networkName, address string) (net.Conn, error) {
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return nil, errors.New("invalid media address")
	}
	ips, err := net.DefaultResolver.LookupIPAddr(ctx, host)
	if err != nil || len(ips) == 0 {
		return nil, errors.New("cannot resolve media host")
	}
	for _, ip := range ips {
		if network.IsPrivateIP(ip.IP) {
			return nil, errors.New("private media address is forbidden")
		}
	}
	dialer := net.Dialer{Timeout: 10 * time.Second}
	for _, ip := range ips {
		conn, dialErr := dialer.DialContext(ctx, networkName, net.JoinHostPort(ip.IP.String(), port))
		if dialErr == nil {
			return conn, nil
		}
	}
	return nil, errors.New("cannot connect to media host")
}

func downloadMedia(ctx context.Context, rawURL string) ([]byte, string, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return nil, "", errors.New("invalid media URL")
	}
	if err = validateMediaURL(u); err != nil {
		return nil, "", err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, "", errors.New("invalid media request")
	}
	// A new request contains no API key, cookies, or client authorization.
	resp, err := mediaHTTPClient.Do(req)
	if err != nil {
		return nil, "", errors.New("media download failed")
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, "", fmt.Errorf("media download returned status %d", resp.StatusCode)
	}
	if resp.ContentLength > maxMediaBytes {
		return nil, "", errors.New("media exceeds size limit")
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxMediaBytes+1))
	if err != nil {
		return nil, "", errors.New("media body read failed")
	}
	if len(data) == 0 || len(data) > maxMediaBytes {
		return nil, "", errors.New("empty or oversized media")
	}
	return data, resp.Header.Get("Content-Type"), nil
}
