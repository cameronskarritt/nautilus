package otel

import "strings"

func TracewayTraceEndpoint(endpoint string) string {
	endpoint = strings.TrimRight(endpoint, "/")
	if endpoint == "" || strings.HasSuffix(endpoint, "/v1/traces") {
		return endpoint
	}
	return endpoint + "/v1/traces"
}
