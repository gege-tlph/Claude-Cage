package cage

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/miekg/dns"
)

// DNSCheckHost is the name the first installation resolves before anything
// else: APT's default Debian source.
const DNSCheckHost = "deb.debian.org"

// DNSEndpointCheck is one encrypted DNS endpoint's answer for a name, queried
// through the selected proxy as guest DNS is (#815).
type DNSEndpointCheck struct {
	Endpoint string `json:"endpoint"`
	// Result is "ok"; "connection" when no HTTPS exchange completed, such as
	// a TLS failure through the proxy; "response" for an HTTP or DNS error
	// answer; or "no_address" for a valid answer without an IPv4 address.
	Result  string `json:"result"`
	Address string `json:"address,omitempty"`
	Detail  string `json:"detail,omitempty"`
}

// CheckDNSEndpoints queries each configured encrypted DNS endpoint once for
// host's IPv4 address, in order, through the same proxy-only transport as
// guest DNS. It never uses the host resolver.
func (e *Egress) CheckDNSEndpoints(ctx context.Context, host string) []DNSEndpointCheck {
	checks := make([]DNSEndpointCheck, 0, len(e.dnsURLs))
	for _, endpoint := range e.dnsURLs {
		attempt, cancel := context.WithTimeout(ctx, 10*time.Second)
		checks = append(checks, e.checkDNSEndpoint(attempt, endpoint, host))
		cancel()
	}
	return checks
}

func (e *Egress) checkDNSEndpoint(ctx context.Context, endpoint, host string) DNSEndpointCheck {
	check := DNSEndpointCheck{Endpoint: endpoint}
	q := new(dns.Msg)
	q.SetQuestion(dns.Fqdn(host), dns.TypeA)
	wire, err := q.Pack()
	if err != nil {
		check.Result, check.Detail = "response", err.Error()
		return check
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(wire))
	if err != nil {
		check.Result, check.Detail = "connection", err.Error()
		return check
	}
	req.Header.Set("Content-Type", "application/dns-message")
	req.Header.Set("Accept", "application/dns-message")
	resp, err := e.client.Do(req)
	if err != nil {
		// The URL is shown beside the result; keep only the cause.
		var urlErr *url.Error
		if errors.As(err, &urlErr) {
			err = urlErr.Err
		}
		check.Result, check.Detail = "connection", err.Error()
		return check
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		check.Result, check.Detail = "response", fmt.Sprintf("HTTP %d", resp.StatusCode)
		return check
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 65536))
	if err != nil {
		check.Result, check.Detail = "connection", err.Error()
		return check
	}
	answer := new(dns.Msg)
	if err = answer.Unpack(body); err != nil || !answer.Response || answer.Id != q.Id {
		check.Result, check.Detail = "response", "invalid DNS response"
		return check
	}
	if answer.Rcode != dns.RcodeSuccess {
		check.Result, check.Detail = "response", strings.ToUpper(dns.RcodeToString[answer.Rcode])
		return check
	}
	for _, record := range answer.Answer {
		if a, ok := record.(*dns.A); ok {
			check.Result, check.Address = "ok", a.A.String()
			return check
		}
	}
	check.Result = "no_address"
	return check
}

// CheckDNS runs CheckDNSEndpoints for a configuration's endpoints and proxy.
func CheckDNS(ctx context.Context, c Config, host string) ([]DNSEndpointCheck, error) {
	e, err := NewEgress(c)
	if err != nil {
		return nil, err
	}
	defer e.client.CloseIdleConnections()
	return e.CheckDNSEndpoints(ctx, host), nil
}
