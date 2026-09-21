package main

import (
	"context"
	"log"
	"net/http"
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

var (
	expirationDesc = prometheus.NewDesc(
		"godaddy_domain_expiration_timestamp_seconds",
		"Domain expiration time as a Unix timestamp.",
		[]string{"domain"}, nil,
	)
	renewByDesc = prometheus.NewDesc(
		"godaddy_domain_renew_by_timestamp_seconds",
		"Domain renewal deadline as a Unix timestamp.",
		[]string{"domain"}, nil,
	)
	autoRenewDesc = prometheus.NewDesc(
		"godaddy_domain_auto_renew",
		"Whether automatic renewal is enabled for the domain.",
		[]string{"domain"}, nil,
	)
	statusDesc = prometheus.NewDesc(
		"godaddy_domain_status",
		"Current domain status as a labeled value of 1.",
		[]string{"domain", "status"}, nil,
	)
	scrapeSuccessDesc = prometheus.NewDesc(
		"godaddy_scrape_success",
		"Whether the complete GoDaddy scrape succeeded.",
		nil, nil,
	)
)

const scrapeTimeout = 8 * time.Second

type collector struct {
	client  *http.Client
	baseURL string
	pat     string
	timeout time.Duration
}

func (c *collector) Describe(descriptions chan<- *prometheus.Desc) {
	descriptions <- expirationDesc
	descriptions <- renewByDesc
	descriptions <- autoRenewDesc
	descriptions <- statusDesc
	descriptions <- scrapeSuccessDesc
}

func (c *collector) Collect(metrics chan<- prometheus.Metric) {
	ctx, cancel := context.WithTimeout(context.Background(), c.timeout)
	defer cancel()

	domains, err := fetchDomains(ctx, c.client, c.baseURL, c.pat)
	if err != nil {
		log.Printf("GoDaddy scrape failed: %v", err)
		metrics <- prometheus.MustNewConstMetric(scrapeSuccessDesc, prometheus.GaugeValue, 0)
		return
	}

	for _, d := range domains {
		autoRenew := 0.0
		if d.AutoRenew {
			autoRenew = 1
		}

		metrics <- prometheus.MustNewConstMetric(expirationDesc, prometheus.GaugeValue, float64(d.ExpiresAt.Unix()), d.Name)
		if d.RenewBy != nil {
			metrics <- prometheus.MustNewConstMetric(renewByDesc, prometheus.GaugeValue, float64(d.RenewBy.Unix()), d.Name)
		}
		metrics <- prometheus.MustNewConstMetric(autoRenewDesc, prometheus.GaugeValue, autoRenew, d.Name)
		metrics <- prometheus.MustNewConstMetric(statusDesc, prometheus.GaugeValue, 1, d.Name, d.Status)
	}

	metrics <- prometheus.MustNewConstMetric(scrapeSuccessDesc, prometheus.GaugeValue, 1)
}
