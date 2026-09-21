package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

type domain struct {
	Name      string
	ExpiresAt time.Time
	RenewBy   *time.Time
	Status    string
	AutoRenew bool
}

type apiPage struct {
	Items []apiDomain `json:"items"`
	Links []apiLink   `json:"links"`
}

type apiDomain struct {
	Name      string  `json:"domain"`
	ExpiresAt string  `json:"expiresAt"`
	RenewBy   *string `json:"renewBy"`
	Status    string  `json:"status"`
	AutoRenew *bool   `json:"autoRenew"`
}

type apiLink struct {
	Rel  string `json:"rel"`
	Href string `json:"href"`
}

func fetchDomains(ctx context.Context, client *http.Client, baseURL, pat string) ([]domain, error) {
	httpClient := *client
	httpClient.CheckRedirect = func(_ *http.Request, _ []*http.Request) error {
		return http.ErrUseLastResponse
	}

	nextURL := strings.TrimRight(baseURL, "/") + "/v3/domains/domain-names?pageSize=200"
	var domains []domain
	seenDomains := make(map[string]struct{})

	for nextURL != "" {
		request, err := http.NewRequestWithContext(ctx, http.MethodGet, nextURL, nil)
		if err != nil {
			return nil, fmt.Errorf("create GoDaddy request: %w", err)
		}
		request.Header.Set("Authorization", "Bearer "+pat)

		response, err := httpClient.Do(request)
		if err != nil {
			return nil, fmt.Errorf("request GoDaddy domains: %w", err)
		}
		body, readErr := io.ReadAll(response.Body)
		response.Body.Close()
		if readErr != nil {
			return nil, fmt.Errorf("read GoDaddy response: %w", readErr)
		}
		if response.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("GoDaddy API returned %s", response.Status)
		}

		var page apiPage
		if err := json.Unmarshal(body, &page); err != nil {
			return nil, fmt.Errorf("decode GoDaddy response: %w", err)
		}
		if page.Items == nil || page.Links == nil {
			return nil, fmt.Errorf("GoDaddy response contains an incomplete page")
		}

		for _, item := range page.Items {
			if item.Name == "" || item.Status == "" || item.AutoRenew == nil {
				return nil, fmt.Errorf("GoDaddy response contains an incomplete domain")
			}
			if _, exists := seenDomains[item.Name]; exists {
				return nil, fmt.Errorf("GoDaddy response contains duplicate domain %s", item.Name)
			}
			seenDomains[item.Name] = struct{}{}

			expiresAt, err := time.Parse(time.RFC3339, item.ExpiresAt)
			if err != nil {
				return nil, fmt.Errorf("domain %s has invalid expiresAt: %w", item.Name, err)
			}

			var renewBy *time.Time
			if item.RenewBy != nil {
				parsed, err := time.Parse(time.RFC3339, *item.RenewBy)
				if err != nil {
					return nil, fmt.Errorf("domain %s has invalid renewBy: %w", item.Name, err)
				}
				renewBy = &parsed
			}

			domains = append(domains, domain{
				Name:      item.Name,
				ExpiresAt: expiresAt,
				RenewBy:   renewBy,
				Status:    item.Status,
				AutoRenew: *item.AutoRenew,
			})
		}

		nextURL = ""
		for _, link := range page.Links {
			if link.Rel != "next" {
				continue
			}
			if link.Href == "" {
				return nil, fmt.Errorf("GoDaddy response contains an empty next link")
			}
			next, err := request.URL.Parse(link.Href)
			if err != nil {
				return nil, fmt.Errorf("parse GoDaddy next link: %w", err)
			}
			if next.Scheme != request.URL.Scheme || !strings.EqualFold(next.Host, request.URL.Host) {
				return nil, fmt.Errorf("GoDaddy next link points outside the API origin")
			}
			nextURL = next.String()
		}
	}

	return domains, nil
}
