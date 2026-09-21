package main

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

const testPAT = "test-pat-secret"

func TestFetchDomainsPaginates(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		requests++
		if request.Header.Get("Authorization") != "Bearer "+testPAT {
			t.Errorf("unexpected Authorization header")
		}
		if request.URL.Path != "/v3/domains/domain-names" {
			t.Errorf("unexpected path: %s", request.URL.Path)
		}

		response.Header().Set("Content-Type", "application/json")
		if requests == 1 {
			if request.URL.Query().Get("pageSize") != "200" {
				t.Errorf("unexpected pageSize: %s", request.URL.Query().Get("pageSize"))
			}
			response.Write([]byte(`{"items":[{"domain":"first.example","expiresAt":"2030-01-02T03:04:05Z","renewBy":"2029-12-03T03:04:05Z","status":"ACTIVE","autoRenew":true}],"links":[{"rel":"next","href":"/v3/domains/domain-names?pageSize=200&pageToken=next"}]}`))
			return
		}

		if request.URL.Query().Get("pageToken") != "next" {
			t.Errorf("unexpected pageToken: %s", request.URL.Query().Get("pageToken"))
		}
		response.Write([]byte(`{"items":[{"domain":"second.example","expiresAt":"2031-01-02T03:04:05Z","status":"ACTIVE","autoRenew":false}],"links":[]}`))
	}))
	defer server.Close()

	domains, err := fetchDomains(context.Background(), server.Client(), server.URL, testPAT)
	if err != nil {
		t.Fatalf("fetch domains: %v", err)
	}
	if requests != 2 {
		t.Fatalf("expected 2 requests, got %d", requests)
	}
	if len(domains) != 2 || domains[0].Name != "first.example" || domains[1].Name != "second.example" {
		t.Fatalf("unexpected domains: %+v", domains)
	}
	if !domains[0].AutoRenew || domains[1].AutoRenew {
		t.Fatalf("unexpected auto-renew values")
	}
	if got, want := domains[0].ExpiresAt.Unix(), time.Date(2030, 1, 2, 3, 4, 5, 0, time.UTC).Unix(); got != want {
		t.Fatalf("unexpected expiration timestamp: got %d, want %d", got, want)
	}
	if domains[0].RenewBy == nil || domains[0].RenewBy.Unix() != time.Date(2029, 12, 3, 3, 4, 5, 0, time.UTC).Unix() {
		t.Fatal("unexpected renewal timestamp")
	}
	if domains[1].RenewBy != nil {
		t.Fatal("expected no renewal timestamp for second.example")
	}
}

func TestFetchDomainsRejectsInvalidData(t *testing.T) {
	tests := map[string]string{
		"null page":         `null`,
		"empty object":      `{}`,
		"null items":        `{"items":null,"links":[]}`,
		"null links":        `{"items":[],"links":null}`,
		"missing field":     `{"items":[{"domain":"example.test","expiresAt":"2030-01-02T03:04:05Z","renewBy":"2029-12-03T03:04:05Z","status":"ACTIVE"}],"links":[]}`,
		"invalid timestamp": `{"items":[{"domain":"example.test","expiresAt":"invalid","renewBy":"2029-12-03T03:04:05Z","status":"ACTIVE","autoRenew":true}],"links":[]}`,
		"invalid renewBy":   `{"items":[{"domain":"example.test","expiresAt":"2030-01-02T03:04:05Z","renewBy":"invalid","status":"ACTIVE","autoRenew":true}],"links":[]}`,
		"duplicate domain":  `{"items":[{"domain":"example.test","expiresAt":"2030-01-02T03:04:05Z","status":"ACTIVE","autoRenew":true},{"domain":"example.test","expiresAt":"2030-01-02T03:04:05Z","status":"ACTIVE","autoRenew":true}],"links":[]}`,
	}

	for name, body := range tests {
		t.Run(name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
				response.Header().Set("Content-Type", "application/json")
				response.Write([]byte(body))
			}))
			defer server.Close()

			domains, err := fetchDomains(context.Background(), server.Client(), server.URL, testPAT)
			if err == nil {
				t.Fatal("expected an error")
			}
			if domains != nil {
				t.Fatalf("expected no domains, got %+v", domains)
			}
		})
	}
}

func TestFetchDomainsUsesOneDeadline(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.URL.Query().Get("pageToken") == "next" {
			<-request.Context().Done()
			return
		}

		response.Header().Set("Content-Type", "application/json")
		response.Write([]byte(`{"items":[],"links":[{"rel":"next","href":"/v3/domains/domain-names?pageSize=200&pageToken=next"}]}`))
	}))
	defer server.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	domains, err := fetchDomains(ctx, server.Client(), server.URL, testPAT)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected deadline exceeded, got %v", err)
	}
	if domains != nil {
		t.Fatalf("expected no domains, got %+v", domains)
	}
}

func TestFetchDomainsRejectsExternalNextLink(t *testing.T) {
	called := make(chan struct{}, 1)
	target := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		called <- struct{}{}
		response.Write([]byte(`{"items":[],"links":[]}`))
	}))
	defer target.Close()

	source := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		response.Header().Set("Content-Type", "application/json")
		response.Write([]byte(`{"items":[],"links":[{"rel":"next","href":"` + target.URL + `"}]}`))
	}))
	defer source.Close()

	if _, err := fetchDomains(context.Background(), source.Client(), source.URL, testPAT); err == nil {
		t.Fatal("expected an error")
	}
	select {
	case <-called:
		t.Fatal("external next link was requested")
	default:
	}
}

func TestFetchDomainsDoesNotFollowRedirects(t *testing.T) {
	called := make(chan struct{}, 1)
	target := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		called <- struct{}{}
		response.Write([]byte(`{"items":[],"links":[]}`))
	}))
	defer target.Close()

	source := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		http.Redirect(response, request, target.URL, http.StatusFound)
	}))
	defer source.Close()

	if _, err := fetchDomains(context.Background(), source.Client(), source.URL, testPAT); err == nil {
		t.Fatal("expected an error")
	}
	select {
	case <-called:
		t.Fatal("redirect was followed")
	default:
	}
}

func TestCollectorAppliesDeadline(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		select {
		case <-request.Context().Done():
			return
		case <-time.After(500 * time.Millisecond):
			response.Header().Set("Content-Type", "application/json")
			response.Write([]byte(`{"items":[],"links":[]}`))
		}
	}))
	defer server.Close()

	registry := prometheus.NewRegistry()
	registry.MustRegister(&collector{
		client:  server.Client(),
		baseURL: server.URL,
		pat:     testPAT,
		timeout: 50 * time.Millisecond,
	})

	request := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	response := httptest.NewRecorder()
	promhttp.HandlerFor(registry, promhttp.HandlerOpts{}).ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", response.Code)
	}
	if !strings.Contains(response.Body.String(), "godaddy_scrape_success 0") {
		t.Error("collector deadline did not fail the GoDaddy scrape")
	}
}

func TestFetchDomainsIsAtomicOnAPIFailure(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.URL.Query().Get("pageToken") == "next" {
			response.WriteHeader(http.StatusInternalServerError)
			response.Write([]byte(testPAT))
			return
		}

		response.Header().Set("Content-Type", "application/json")
		response.Write([]byte(`{"items":[{"domain":"first.example","expiresAt":"2030-01-02T03:04:05Z","renewBy":"2029-12-03T03:04:05Z","status":"ACTIVE","autoRenew":true}],"links":[{"rel":"next","href":"/v3/domains/domain-names?pageSize=200&pageToken=next"}]}`))
	}))
	defer server.Close()

	domains, err := fetchDomains(context.Background(), server.Client(), server.URL, testPAT)
	if err == nil {
		t.Fatal("expected an error")
	}
	if domains != nil {
		t.Fatalf("expected no domains, got %+v", domains)
	}
	if strings.Contains(err.Error(), testPAT) {
		t.Fatal("error contains PAT")
	}
}

func TestMetrics(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
			response.Header().Set("Content-Type", "application/json")
			response.Write([]byte(`{"items":[{"domain":"example.test","expiresAt":"2030-01-02T03:04:05Z","renewBy":"2029-12-03T03:04:05Z","status":"ACTIVE","autoRenew":true},{"domain":"no-renew.example","expiresAt":"2031-01-02T03:04:05Z","status":"ACTIVE","autoRenew":false}],"links":[]}`))
		}))
		defer server.Close()

		body, status := gatherMetrics(t, server, testPAT)
		if status != http.StatusOK {
			t.Fatalf("expected status 200, got %d", status)
		}
		for _, metric := range []string{
			`godaddy_domain_expiration_timestamp_seconds{domain="example.test"}`,
			`godaddy_domain_expiration_timestamp_seconds{domain="no-renew.example"}`,
			`godaddy_domain_renew_by_timestamp_seconds{domain="example.test"}`,
			`godaddy_domain_auto_renew{domain="example.test"} 1`,
			`godaddy_domain_auto_renew{domain="no-renew.example"} 0`,
			`godaddy_domain_status{domain="example.test",status="ACTIVE"} 1`,
			`godaddy_scrape_success 1`,
		} {
			if !strings.Contains(body, metric) {
				t.Errorf("metrics do not contain %s", metric)
			}
		}
		if strings.Contains(body, `godaddy_domain_renew_by_timestamp_seconds{domain="no-renew.example"}`) {
			t.Error("renewal metric was published without renewBy")
		}
		if got, want := metricValue(t, body, `godaddy_domain_expiration_timestamp_seconds{domain="example.test"}`), float64(time.Date(2030, 1, 2, 3, 4, 5, 0, time.UTC).Unix()); got != want {
			t.Errorf("unexpected expiration metric: got %v, want %v", got, want)
		}
		if got, want := metricValue(t, body, `godaddy_domain_renew_by_timestamp_seconds{domain="example.test"}`), float64(time.Date(2029, 12, 3, 3, 4, 5, 0, time.UTC).Unix()); got != want {
			t.Errorf("unexpected renewal metric: got %v, want %v", got, want)
		}
	})

	t.Run("API failure", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
			response.WriteHeader(http.StatusInternalServerError)
		}))
		defer server.Close()

		body, status := gatherMetrics(t, server, testPAT)
		if status != http.StatusOK {
			t.Fatalf("expected status 200, got %d", status)
		}
		if !strings.Contains(body, "godaddy_scrape_success 0") {
			t.Error("metrics do not contain godaddy_scrape_success 0")
		}
		if strings.Contains(body, "godaddy_domain_") {
			t.Error("domain metrics were published after an API failure")
		}
	})
}

func gatherMetrics(t *testing.T, server *httptest.Server, pat string) (string, int) {
	t.Helper()
	registry := prometheus.NewRegistry()
	registry.MustRegister(&collector{
		client:  server.Client(),
		baseURL: server.URL,
		pat:     pat,
		timeout: scrapeTimeout,
	})

	request := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	response := httptest.NewRecorder()
	promhttp.HandlerFor(registry, promhttp.HandlerOpts{}).ServeHTTP(response, request)
	return response.Body.String(), response.Code
}

func metricValue(t *testing.T, body, metric string) float64 {
	t.Helper()
	for _, line := range strings.Split(body, "\n") {
		if !strings.HasPrefix(line, metric+" ") {
			continue
		}
		value, err := strconv.ParseFloat(strings.TrimPrefix(line, metric+" "), 64)
		if err != nil {
			t.Fatalf("parse metric %s: %v", metric, err)
		}
		return value
	}
	t.Fatalf("metric %s not found", metric)
	return 0
}
