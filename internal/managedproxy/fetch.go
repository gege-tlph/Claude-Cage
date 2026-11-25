package managedproxy

import (
	"bytes"
	"context"
	"crypto/tls"
	"errors"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/miekg/dns"
	"github.com/sylvinus/agent-vm/internal/hostproxy"
)

// BootstrapClient serves only explicitly host-routed subscription requests.
func BootstrapClient() *http.Client { return BootstrapClientWith(BootstrapDNSURLs()) }

// BootstrapClientWith downloads over the host network, resolving names with
// the given numeric DoH endpoints.
func BootstrapClientWith(servers []string) *http.Client {
	if len(servers) == 0 {
		servers = BootstrapDNSURLs()
	}
	return &http.Client{Transport: subscriptionHostTransport{hostproxy.NewTransport(), servers}, Timeout: 30 * time.Second, CheckRedirect: SubscriptionRedirect}
}

// SubscriptionRedirect follows at most three HTTPS redirects without
// credentials. A subscription URL often carries its token in the path or
// query, so it is never sent on to the next server as the Referer.
func SubscriptionRedirect(r *http.Request, via []*http.Request) error {
	if len(via) >= 3 || r.URL.Scheme != "https" || r.URL.User != nil {
		return errSubscriptionRedirect
	}
	r.Header.Del("Referer")
	return nil
}

var (
	errSubscriptionRedirect = errors.New("订阅重定向被拒绝")
	errPrivateSubscription  = errors.New("订阅服务器必须使用公网地址")
	errInfrastructureName   = errors.New("代理基础设施域名无效")
	errInfrastructureDNS    = errors.New("代理基础设施 DNS 均不可用或未返回公网 IPv4；请检查主机网络后重试，未使用系统 DNS 回退")
)

// infrastructureDNSTimeout is errInfrastructureDNS when no resolver answered
// in time. Fetch does not retry it: another attempt would wait just as long.
type infrastructureDNSTimeout struct{}

func (infrastructureDNSTimeout) Error() string        { return errInfrastructureDNS.Error() }
func (infrastructureDNSTimeout) Is(target error) bool { return target == errInfrastructureDNS }
func (infrastructureDNSTimeout) Timeout() bool        { return true }

type subscriptionHostTransport struct {
	host      *http.Transport
	resolvers []string
}

func (t subscriptionHostTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	// Resolve routing for the original hostname (including on redirects).
	// Pin both DoH and the subscription transfer to the selected route.
	routing := req.Clone(req.Context())
	routing.URL.Path, routing.URL.RawPath, routing.URL.RawQuery = "", "", ""
	route, err := t.host.Proxy(routing)
	if err != nil {
		return nil, err
	}
	tr := t.host.Clone()
	tr.Proxy = http.ProxyURL(route)
	tr.DisableKeepAlives = true
	host := req.URL.Hostname()
	ip, err := netip.ParseAddr(host)
	if err != nil {
		dnsClient := &http.Client{Transport: tr, CheckRedirect: func(*http.Request, []*http.Request) error { return errors.New("DNS redirect refused") }}
		ip, err = resolveInfrastructureWith(req.Context(), host, dnsClient, t.resolvers)
		if err != nil {
			return nil, err
		}
	}
	if !publicIP(ip) {
		return nil, errPrivateSubscription
	}

	// CONNECT (or SOCKS) must address the validated IP, while HTTP Host and
	// TLS SNI/certificate checks still use the subscription's original name.
	// DoH requests may still be finishing, so configure a separate transport.
	target := tr.Clone()
	if target.TLSClientConfig == nil {
		target.TLSClientConfig = &tls.Config{}
	}
	if route != nil && route.Scheme == "https" {
		// The outer TLS handshake authenticates the proxy, independently of
		// the inner TLS session established through its CONNECT tunnel.
		outer := target.TLSClientConfig.Clone()
		outer.ServerName, outer.NextProtos = route.Hostname(), []string{"http/1.1"}
		target.DialTLSContext = (&tls.Dialer{Config: outer, NetDialer: &net.Dialer{Timeout: 10 * time.Second}}).DialContext
	}
	target.TLSClientConfig.ServerName = host
	pinned := req.Clone(req.Context())
	pinned.Host = req.URL.Host
	pinned.URL.Host = ip.String()
	if port := req.URL.Port(); port != "" {
		pinned.URL.Host = net.JoinHostPort(ip.String(), port)
	}
	return target.RoundTrip(pinned)
}

func usageSummary(raw string) string {
	parts := []string{}
	for _, field := range strings.Split(raw, ";") {
		kv := strings.SplitN(strings.TrimSpace(field), "=", 2)
		if len(kv) != 2 {
			continue
		}
		if kv[0] != "upload" && kv[0] != "download" && kv[0] != "total" && kv[0] != "expire" {
			continue
		}
		if _, err := strconv.ParseUint(kv[1], 10, 64); err == nil {
			parts = append(parts, kv[0]+"="+kv[1])
		}
	}
	return strings.Join(parts, "; ")
}
func publicIP(ip netip.Addr) bool {
	return ip.Is4() && ip.IsGlobalUnicast() && !ip.IsPrivate() && !ip.IsLoopback() && !ip.IsLinkLocalUnicast() && !netip.MustParsePrefix("100.64.0.0/10").Contains(ip) && !netip.MustParsePrefix("198.18.0.0/15").Contains(ip)
}

// BootstrapDNSURLs is shared by host downloads and the core's node resolver.
// Numeric HTTPS endpoints need neither the host resolver nor a working proxy.
// Keep in step with cage.DefaultBootstrapDNS (#470).
func BootstrapDNSURLs() []string {
	return []string{"https://1.1.1.1/dns-query", "https://223.5.5.5/dns-query", "https://223.6.6.6/dns-query", "https://1.12.12.12/dns-query"}
}

func resolveInfrastructureWith(ctx context.Context, host string, client *http.Client, servers []string) (netip.Addr, error) {
	parent := ctx
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	q := new(dns.Msg)
	q.SetQuestion(dns.Fqdn(host), dns.TypeA)
	wire, err := q.Pack()
	if err != nil {
		return netip.Addr{}, errInfrastructureName
	}
	type result struct {
		ip  netip.Addr
		err error
	}
	results := make(chan result, len(servers))
	// A blocked resolver must not delay a reachable one. Cancel the remaining
	// HTTPS requests once a usable public answer is available.
	for _, server := range servers {
		go func() {
			ip, err := queryInfrastructureDNS(ctx, client, server, q, wire)
			results <- result{ip, err}
		}()
	}
	for range servers {
		select {
		case <-ctx.Done():
			if err := parent.Err(); err != nil {
				return netip.Addr{}, err
			}
			return netip.Addr{}, infrastructureDNSTimeout{}
		case result := <-results:
			if result.err == nil {
				return result.ip, nil
			}
		}
	}
	return netip.Addr{}, errInfrastructureDNS
}

func queryInfrastructureDNS(ctx context.Context, client *http.Client, server string, q *dns.Msg, wire []byte) (netip.Addr, error) {
	req, err := http.NewRequestWithContext(ctx, "POST", server, bytes.NewReader(wire))
	if err != nil {
		return netip.Addr{}, err
	}
	req.Header.Set("Content-Type", "application/dns-message")
	req.Header.Set("Accept", "application/dns-message")
	resp, err := client.Do(req)
	if err != nil {
		return netip.Addr{}, errors.New("代理基础设施域名解析失败")
	}
	defer resp.Body.Close()
	wire, err = io.ReadAll(io.LimitReader(resp.Body, 65536))
	var answer dns.Msg
	if err != nil || resp.StatusCode != 200 || answer.Unpack(wire) != nil || answer.Id != q.Id ||
		!answer.Response || answer.Truncated || answer.Rcode != dns.RcodeSuccess || len(answer.Question) != 1 ||
		!strings.EqualFold(answer.Question[0].Name, q.Question[0].Name) || answer.Question[0].Qtype != dns.TypeA || answer.Question[0].Qclass != dns.ClassINET {
		return netip.Addr{}, errors.New("代理基础设施 DNS 响应无效")
	}
	for _, rr := range answer.Answer {
		if a, ok := rr.(*dns.A); ok {
			if ip, ok := netip.AddrFromSlice(a.A); ok && publicIP(ip.Unmap()) {
				return ip.Unmap(), nil
			}
		}
	}
	return netip.Addr{}, errors.New("代理基础设施域名没有可用公网 IPv4")
}
// subscriptionUserAgent matches the bundled Mihomo core version without
// exposing any product-specific identifier to subscription providers.
const subscriptionUserAgent = "clash.meta/v1.19.33"

func Fetch(ctx context.Context, client *http.Client, rawURL string) ([]byte, string, error) {
	u, err := url.Parse(rawURL)
	if err != nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil || u.Fragment != "" {
		return nil, "", errors.New("订阅需要完整的 HTTPS 网址，不支持地址内用户名和密码")
	}
	for attempt := 0; ; attempt++ {
		data, usage, retry, err := fetchSubscription(ctx, client, u.String())
		if err == nil || !retry || attempt == len(subscriptionRetryDelays) {
			return data, usage, err
		}
		timer := time.NewTimer(subscriptionRetryDelays[attempt])
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil, "", err
		case <-timer.C:
		}
	}
}

// subscriptionRetryDelays: subscription servers behind busy CDNs sometimes
// answer 5xx, 408 or 429, or drop the connection. Two quick retries ride
// that out.
var subscriptionRetryDelays = []time.Duration{300 * time.Millisecond, 800 * time.Millisecond}

// fetchSubscription makes one download attempt. retry reports a failure that
// another attempt soon after may not repeat; refusals and timeouts are final.
func fetchSubscription(ctx context.Context, client *http.Client, target string) (data []byte, usage string, retry bool, err error) {
	req, err := http.NewRequestWithContext(ctx, "GET", target, nil)
	if err != nil {
		return nil, "", false, errors.New("订阅网址无效")
	}
	req.Header.Set("User-Agent", subscriptionUserAgent)
	res, err := client.Do(req)
	if err != nil {
		retry = transient(ctx, err)
		for _, final := range []error{errSubscriptionRedirect, errPrivateSubscription, errInfrastructureName} {
			if errors.Is(err, final) {
				return nil, "", false, final
			}
		}
		if errors.Is(err, errInfrastructureDNS) {
			return nil, "", retry, errInfrastructureDNS
		}
		return nil, "", retry, errors.New("订阅下载失败，请检查下载路径和服务地址")
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		_, _ = io.Copy(io.Discard, io.LimitReader(res.Body, 4096))
		code := res.StatusCode
		return nil, "", code == http.StatusRequestTimeout || code == http.StatusTooManyRequests || code >= 500, errors.New("订阅服务器返回 HTTP " + strconv.Itoa(code))
	}
	b, err := io.ReadAll(io.LimitReader(res.Body, MaxImport+1))
	if err != nil || len(b) > MaxImport {
		return nil, "", len(b) <= MaxImport && transient(ctx, err), errors.New("订阅下载中断或超过 8 MiB")
	}
	if raw := res.Header.Get("Subscription-Userinfo"); len(raw) < 300 {
		// Keep only known numeric metadata; never echo arbitrary headers.
		usage = usageSummary(raw)
	}
	return b, usage, false, nil
}

func transient(ctx context.Context, err error) bool {
	var timeout interface{ Timeout() bool }
	return ctx.Err() == nil && !(errors.As(err, &timeout) && timeout.Timeout())
}
