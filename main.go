package main

import (
	"log"
	"net/http"
	"os"
	"strings"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

const godaddyAPIURL = "https://api.godaddy.com"

func main() {
	pat := strings.TrimSpace(os.Getenv("GODADDY_PAT"))
	if pat == "" {
		log.Fatal("GODADDY_PAT is required")
	}

	prometheus.MustRegister(&collector{
		client:  &http.Client{},
		baseURL: godaddyAPIURL,
		pat:     pat,
		timeout: scrapeTimeout,
	})

	http.Handle("/metrics", promhttp.Handler())
	log.Printf("listening on :8080")
	log.Fatal(http.ListenAndServe(":8080", nil))
}
