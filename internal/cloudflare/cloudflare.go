// Package cloudflare is a minimal client for the Cloudflare DNS API, used for
// ACME DNS-01 challenges and dynamic DNS.
package cloudflare

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

var apiBase = "https://api.cloudflare.com/client/v4" // var for tests

type Client struct {
	Token string
	HTTP  *http.Client
}

func New(token string) *Client {
	return &Client{Token: token, HTTP: &http.Client{Timeout: 30 * time.Second}}
}

type apiResponse struct {
	Success bool                       `json:"success"`
	Errors  []struct{ Message string } `json:"errors"`
	Result  json.RawMessage            `json:"result"`
}

func (c *Client) do(ctx context.Context, method, path string, body, out any) error {
	var rd io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		rd = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, apiBase+path, rd)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.Token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	var ar apiResponse
	if err := json.NewDecoder(io.LimitReader(resp.Body, 4<<20)).Decode(&ar); err != nil {
		return fmt.Errorf("cloudflare %s %s: HTTP %d", method, path, resp.StatusCode)
	}
	if !ar.Success {
		var msgs []string
		for _, e := range ar.Errors {
			msgs = append(msgs, e.Message)
		}
		return fmt.Errorf("cloudflare %s %s: %s", method, path, strings.Join(msgs, "; "))
	}
	if out != nil {
		return json.Unmarshal(ar.Result, out)
	}
	return nil
}

// ZoneID finds the zone containing name by trying successively shorter
// suffixes (mail.example.com -> example.com).
func (c *Client) ZoneID(ctx context.Context, name string) (string, error) {
	labels := strings.Split(strings.TrimSuffix(name, "."), ".")
	for i := 0; i < len(labels)-1; i++ {
		candidate := strings.Join(labels[i:], ".")
		var zones []struct{ ID string }
		if err := c.do(ctx, "GET", "/zones?name="+url.QueryEscape(candidate), nil, &zones); err != nil {
			return "", err
		}
		if len(zones) > 0 {
			return zones[0].ID, nil
		}
	}
	return "", fmt.Errorf("no Cloudflare zone found for %s (check the API token's zone access)", name)
}

type Record struct {
	ID      string `json:"id,omitempty"`
	Type    string `json:"type"`
	Name    string `json:"name"`
	Content string `json:"content"`
	TTL     int    `json:"ttl"`
	Proxied *bool  `json:"proxied,omitempty"`
}

func (c *Client) CreateRecord(ctx context.Context, zoneID string, r Record) (string, error) {
	var out Record
	if err := c.do(ctx, "POST", "/zones/"+zoneID+"/dns_records", r, &out); err != nil {
		return "", err
	}
	return out.ID, nil
}

func (c *Client) DeleteRecord(ctx context.Context, zoneID, id string) error {
	return c.do(ctx, "DELETE", "/zones/"+zoneID+"/dns_records/"+id, nil, nil)
}

func (c *Client) ListRecords(ctx context.Context, zoneID, typ, name string) ([]Record, error) {
	var out []Record
	q := url.Values{"type": {typ}, "name": {name}}
	err := c.do(ctx, "GET", "/zones/"+zoneID+"/dns_records?"+q.Encode(), nil, &out)
	return out, err
}

// UpsertA points name's A record at ip. Mail hostnames must never be
// proxied (orange cloud): Cloudflare's proxy only carries HTTP.
func (c *Client) UpsertA(ctx context.Context, zoneID, name, ip string) (changed bool, err error) {
	recs, err := c.ListRecords(ctx, zoneID, "A", name)
	if err != nil {
		return false, err
	}
	off := false
	want := Record{Type: "A", Name: name, Content: ip, TTL: 300, Proxied: &off}
	if len(recs) == 0 {
		_, err := c.CreateRecord(ctx, zoneID, want)
		return err == nil, err
	}
	if recs[0].Content == ip && recs[0].Proxied != nil && !*recs[0].Proxied {
		return false, nil
	}
	return true, c.do(ctx, "PUT", "/zones/"+zoneID+"/dns_records/"+recs[0].ID, want, nil)
}

// PublicIPv4 asks Cloudflare's edge which address we're connecting from.
func PublicIPv4(ctx context.Context) (string, error) {
	dialer := &net.Dialer{Timeout: 10 * time.Second}
	tr := &http.Transport{DialContext: func(ctx context.Context, _, addr string) (net.Conn, error) {
		return dialer.DialContext(ctx, "tcp4", addr)
	}}
	hc := &http.Client{Transport: tr, Timeout: 15 * time.Second}
	req, _ := http.NewRequestWithContext(ctx, "GET", "https://1.1.1.1/cdn-cgi/trace", nil)
	resp, err := hc.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
	if err != nil {
		return "", err
	}
	for _, line := range strings.Split(string(b), "\n") {
		if ip, ok := strings.CutPrefix(line, "ip="); ok {
			if parsed := net.ParseIP(strings.TrimSpace(ip)); parsed != nil && parsed.To4() != nil {
				return parsed.String(), nil
			}
		}
	}
	return "", errors.New("could not determine public IPv4 address")
}
