# GoDaddy Domain Exporter

Prometheus exporter for monitoring expiration and renewal dates of every domain returned by the GoDaddy Domains API v3.

## Behavior

- Reads all domains from the authenticated GoDaddy account on every Prometheus scrape.
- Follows GoDaddy cursor pagination sequentially with the maximum page size of 200.
- Converts expiresAt and an optional renewBy timestamp to Unix seconds.
- Publishes domain metrics only after every page and every required field have been validated.
- Publishes godaddy_scrape_success 0 without domain metrics when GoDaddy returns an error or invalid data.
- Keeps no database, cache, background worker or previous domain values.
- Never modifies or renews domains.

## Requirements

- A GoDaddy Personal Access Token with the domains.domain:read scope.
- Go 1.27 or Docker for local execution.

## Run locally

    GODADDY_PAT=your-token go run .

The exporter listens on port 8080 and exposes metrics at http://localhost:8080/metrics.

## Run with Docker

Release images are published for linux/amd64 and linux/arm64.

    docker run --rm -p 8080:8080 -e GODADDY_PAT ghcr.io/denopz/godaddy-domain-exporter:latest

The command passes GODADDY_PAT from the current environment without placing its value in the command line.

## Install with Helm

The chart creates one Secret, one Deployment and one ClusterIP Service. It does not create ServiceMonitor, VMServiceScrape or alert rules.

    helm upgrade --install godaddy-domain-exporter ./charts/godaddy-domain-exporter --set-string godaddyPat=${GODADDY_PAT}

The required godaddyPat value is stored in the Kubernetes Secret and in Helm release data. Image coordinates and container resources can be changed through the image and resources values.

## Metrics

- godaddy_domain_expiration_timestamp_seconds: Domain expiration time as a Unix timestamp. Label: domain.
- godaddy_domain_renew_by_timestamp_seconds: Domain renewal deadline as a Unix timestamp when GoDaddy provides renewBy. Label: domain.
- godaddy_domain_auto_renew: 1 when automatic renewal is enabled and 0 otherwise. Label: domain.
- godaddy_domain_status: Always 1 for the current domain status. Labels: domain and status.
- godaddy_scrape_success: 1 when the complete GoDaddy response is valid and 0 when the request or validation fails.

Prometheus up reports whether the exporter itself is reachable. godaddy_scrape_success separately reports whether the exporter could retrieve and validate the complete current state from GoDaddy.

Example expression for domains expiring within 30 days:

    godaddy_domain_expiration_timestamp_seconds - time() < 30 * 24 * 60 * 60

When GoDaddy reports a later expiresAt after renewal, the expression stops matching automatically.

Domain metrics are intentionally omitted when a GoDaddy scrape fails, so an expiration alert can stop firing while the source is unavailable. Always monitor godaddy_scrape_success == 0 separately and monitor up == 0 for exporter availability.

## Error handling

The metrics endpoint continues to return HTTP 200 when the GoDaddy API fails so Prometheus can ingest godaddy_scrape_success 0. Domain metrics are omitted for that scrape, which prevents a partial API response from being presented as a complete account state.

The complete GoDaddy scrape, including pagination, has an 8 second timeout. Errors are logged without the PAT or the GoDaddy response body.

## Development

Run the unit tests:

    go test ./...

Build the container:

    docker build -t godaddy-domain-exporter .

The unit tests use local HTTP servers with fictional domains and credentials. No GoDaddy account or network access is required.

## License

Apache License 2.0. See LICENSE.
