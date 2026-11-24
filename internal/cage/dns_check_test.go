package cage

import (
	"crypto/tls"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/miekg/dns"
)

// dnsAnswerServer answers every DoH query with answer applied to the reply.
func dnsAnswerServer(t *testing.T, answer func(q, r *dns.Msg)) *httptest.Server {
	t.Helper()
	s := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		q := new(dns.Msg)
		if q.Unpack(body) != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		reply := new(dns.Msg).SetReply(q)
		answer(q, reply)
		body, _ = reply.Pack()
		w.Write(body)
	}))
	t.Cleanup(s.Close)
	return s
}

// Each configured endpoint is checked through the proxy and reported with
// what went wrong: a failed TLS exchange, an error answer, no address, or the
// address it returned (#815).
func TestCheckDNSEndpointsClassifiesEachEndpoint(t *testing.T) {
	good := dnsAnswerServer(t, func(q, r *dns.Msg) {
		r.Answer = append(r.Answer, &dns.A{Hdr: dns.RR_Header{Name: q.Question[0].Name, Rrtype: dns.TypeA, Class: dns.ClassINET, Ttl: 60}, A: net.IPv4(151, 101, 2, 132)})
	})
	refused := dnsAnswerServer(t, func(_, r *dns.Msg) { r.Rcode = dns.RcodeRefused })
	empty := dnsAnswerServer(t, func(*dns.Msg, *dns.Msg) {})
	unavailable := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	t.Cleanup(unavailable.Close)
	// A plain HTTP server behind an https URL: the TLS handshake fails, as
	// when a proxy exit breaks TLS to the endpoint.
	plain := httptest.NewServer(http.NotFoundHandler())
	t.Cleanup(plain.Close)
	broken := "https://" + strings.TrimPrefix(plain.URL, "http://")

	p, connects := testProxy(t)
	defer p.Close()
	c := DefaultConfig()
	c.ProxyURL, c.DNSURL = p.URL, broken
	c.DNS = NewDNSOptions([]string{good.URL, refused.URL, empty.URL, unavailable.URL}, nil, false)
	e, err := NewEgress(c)
	if err != nil {
		t.Fatal(err)
	}
	defer e.client.CloseIdleConnections()
	roots := good.Client().Transport.(*http.Transport).TLSClientConfig.RootCAs.Clone()
	for _, s := range []*httptest.Server{refused, empty, unavailable} {
		roots.AddCert(s.Certificate())
	}
	e.client.Transport.(*http.Transport).TLSClientConfig = &tls.Config{RootCAs: roots}

	checks := e.CheckDNSEndpoints(t.Context(), DNSCheckHost)
	want := []struct{ endpoint, result, address, detail string }{
		{broken, "connection", "", "HTTPS"},
		{good.URL, "ok", "151.101.2.132", ""},
		{refused.URL, "response", "", "REFUSED"},
		{empty.URL, "no_address", "", ""},
		{unavailable.URL, "response", "", "HTTP 503"},
	}
	if len(checks) != len(want) {
		t.Fatalf("checks = %+v", checks)
	}
	for i, w := range want {
		got := checks[i]
		if got.Endpoint != w.endpoint || got.Result != w.result || got.Address != w.address || !strings.Contains(got.Detail, w.detail) {
			t.Errorf("check %d = %+v, want %+v", i, got, w)
		}
		if strings.Contains(got.Detail, w.endpoint) {
			t.Errorf("check %d repeats the endpoint in its detail: %q", i, got.Detail)
		}
	}
	if connects.Load() == 0 {
		t.Fatal("endpoints were not reached through the proxy")
	}
}
