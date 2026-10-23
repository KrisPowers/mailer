package cloudflare

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"net/url"
	"strings"
)

type Zone struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Account struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	} `json:"account"`
}

// Verify checks that the token is valid and active.
func (c *Client) Verify(ctx context.Context) error {
	var r struct{ Status string }
	if err := c.do(ctx, "GET", "/user/tokens/verify", nil, &r); err != nil {
		return err
	}
	if r.Status != "active" {
		return fmt.Errorf("token status is %q", r.Status)
	}
	return nil
}

// Zones lists the zones the token can see.
func (c *Client) Zones(ctx context.Context) ([]Zone, error) {
	var zones []Zone
	err := c.do(ctx, "GET", "/zones?per_page=50", nil, &zones)
	return zones, err
}

// ---- tunnels ----

type Tunnel struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Status      string `json:"status"` // inactive, degraded, healthy, down
	Connections []struct {
		ColoName string `json:"colo_name"`
	} `json:"connections"`
	DeletedAt *string `json:"deleted_at"`
}

func (c *Client) CreateTunnel(ctx context.Context, accountID, name string) (*Tunnel, error) {
	var t Tunnel
	err := c.do(ctx, "POST", "/accounts/"+accountID+"/cfd_tunnel",
		map[string]string{"name": name, "config_src": "cloudflare"}, &t)
	return &t, err
}

func (c *Client) GetTunnel(ctx context.Context, accountID, id string) (*Tunnel, error) {
	var t Tunnel
	err := c.do(ctx, "GET", "/accounts/"+accountID+"/cfd_tunnel/"+id, nil, &t)
	return &t, err
}

func (c *Client) TunnelToken(ctx context.Context, accountID, id string) (string, error) {
	var tok string
	err := c.do(ctx, "GET", "/accounts/"+accountID+"/cfd_tunnel/"+id+"/token", nil, &tok)
	return tok, err
}

// SetTunnelIngress routes hostname to service and 404s everything else.
func (c *Client) SetTunnelIngress(ctx context.Context, accountID, id, hostname, service string) error {
	body := map[string]any{"config": map[string]any{"ingress": []map[string]string{
		{"hostname": hostname, "service": service},
		{"service": "http_status:404"},
	}}}
	return c.do(ctx, "PUT", "/accounts/"+accountID+"/cfd_tunnel/"+id+"/configurations", body, nil)
}

// ---- DNS ----

// UpsertRecord creates or replaces the record of r.Type and r.Name for which
// match returns true (or the first one if match is nil).
func (c *Client) UpsertRecord(ctx context.Context, zoneID string, r Record, match func(Record) bool) error {
	recs, err := c.ListRecords(ctx, zoneID, r.Type, r.Name)
	if err != nil {
		return err
	}
	for _, old := range recs {
		if match == nil || match(old) {
			if old.Content == r.Content && (r.Proxied == nil || (old.Proxied != nil && *old.Proxied == *r.Proxied)) {
				return nil
			}
			return c.do(ctx, "PUT", "/zones/"+zoneID+"/dns_records/"+old.ID, r, nil)
		}
	}
	_, err = c.CreateRecord(ctx, zoneID, r)
	return err
}

// ---- Workers ----

// UploadWorker deploys an ES-module Worker with plain-text and secret bindings.
func (c *Client) UploadWorker(ctx context.Context, accountID, name, script string, vars, secrets map[string]string) error {
	var bindings []map[string]string
	for k, v := range vars {
		bindings = append(bindings, map[string]string{"type": "plain_text", "name": k, "text": v})
	}
	for k, v := range secrets {
		bindings = append(bindings, map[string]string{"type": "secret_text", "name": k, "text": v})
	}
	meta, _ := json.Marshal(map[string]any{
		"main_module":        "inbound.js",
		"compatibility_date": "2025-06-01",
		"bindings":           bindings,
	})

	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	mh := textproto.MIMEHeader{}
	mh.Set("Content-Disposition", `form-data; name="metadata"`)
	mh.Set("Content-Type", "application/json")
	pw, _ := mw.CreatePart(mh)
	pw.Write(meta)
	sh := textproto.MIMEHeader{}
	sh.Set("Content-Disposition", `form-data; name="inbound.js"; filename="inbound.js"`)
	sh.Set("Content-Type", "application/javascript+module")
	pw, _ = mw.CreatePart(sh)
	io.WriteString(pw, script)
	mw.Close()

	return c.doRaw(ctx, "PUT", "/accounts/"+accountID+"/workers/scripts/"+url.PathEscape(name),
		mw.FormDataContentType(), &buf, nil)
}

// ---- Email Routing ----

type EmailRouting struct {
	Enabled bool   `json:"enabled"`
	Status  string `json:"status"` // ready, unconfigured, misconfigured, ...
}

func (c *Client) EmailRoutingStatus(ctx context.Context, zoneID string) (*EmailRouting, error) {
	var er EmailRouting
	err := c.do(ctx, "GET", "/zones/"+zoneID+"/email/routing", nil, &er)
	return &er, err
}

// EnableEmailRouting turns on Email Routing, which publishes Cloudflare's MX
// and SPF records for the zone.
func (c *Client) EnableEmailRouting(ctx context.Context, zoneID, zoneName string) error {
	if er, err := c.EmailRoutingStatus(ctx, zoneID); err == nil && er.Enabled {
		return nil
	}
	err := c.do(ctx, "POST", "/zones/"+zoneID+"/email/routing/dns", map[string]string{"name": zoneName}, nil)
	if err != nil {
		// Older endpoint, still accepted on some accounts.
		if err2 := c.do(ctx, "POST", "/zones/"+zoneID+"/email/routing/enable", map[string]any{}, nil); err2 != nil {
			return err
		}
	}
	return nil
}

// RouteAllToWorker sets the catch-all rule to deliver everything to worker.
func (c *Client) RouteAllToWorker(ctx context.Context, zoneID, worker string) error {
	body := map[string]any{
		"name":     "mailer",
		"enabled":  true,
		"matchers": []map[string]string{{"type": "all"}},
		"actions":  []map[string]any{{"type": "worker", "value": []string{worker}}},
	}
	return c.do(ctx, "PUT", "/zones/"+zoneID+"/email/routing/rules/catch_all", body, nil)
}

// CatchAllWorker returns the Worker the catch-all rule points at, if any.
func (c *Client) CatchAllWorker(ctx context.Context, zoneID string) (string, bool, error) {
	var r struct {
		Enabled bool `json:"enabled"`
		Actions []struct {
			Type  string   `json:"type"`
			Value []string `json:"value"`
		} `json:"actions"`
	}
	if err := c.do(ctx, "GET", "/zones/"+zoneID+"/email/routing/rules/catch_all", nil, &r); err != nil {
		return "", false, err
	}
	for _, a := range r.Actions {
		if a.Type == "worker" && len(a.Value) > 0 {
			return a.Value[0], r.Enabled, nil
		}
	}
	return "", r.Enabled, nil
}

func (c *Client) doRaw(ctx context.Context, method, path, contentType string, body io.Reader, out any) error {
	req, err := http.NewRequestWithContext(ctx, method, apiBase+path, body)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.Token)
	req.Header.Set("Content-Type", contentType)
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
