package observability

import (
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

const service = "dinapay-connector-binancepay-v2"

var limits = [...]float64{.005, .01, .025, .05, .1, .25, .5, 1, 2.5, 5, 10}

type requestKey struct{ method, route, status string }
type durationKey struct{ method, route string }
type providerKey struct{ operation, result string }
type histogram struct {
	count   uint64
	sum     float64
	buckets [len(limits)]uint64
}

var registry = struct {
	sync.RWMutex
	requests          map[requestKey]uint64
	durations         map[durationKey]histogram
	provider          map[providerKey]uint64
	providerDurations map[string]histogram
	operations        map[string]uint64
}{requests: map[requestKey]uint64{}, durations: map[durationKey]histogram{}, provider: map[providerKey]uint64{}, providerDurations: map[string]histogram{}, operations: map[string]uint64{}}

func ObserveProvider(operation string, err error, elapsed time.Duration) {
	result := "success"
	if err != nil {
		result = "error"
	}
	registry.Lock()
	defer registry.Unlock()
	registry.provider[providerKey{operation, result}]++
	h := registry.providerDurations[operation]
	seconds := elapsed.Seconds()
	h.count++
	h.sum += seconds
	for i, limit := range limits {
		if seconds <= limit {
			h.buckets[i]++
		}
	}
	registry.providerDurations[operation] = h
}
func Webhook(result string) {
	registry.Lock()
	defer registry.Unlock()
	registry.operations["webhook:"+result]++
}
func Publish(result string) {
	registry.Lock()
	defer registry.Unlock()
	registry.operations["publish:"+result]++
}

func ObserveHTTP(method, route string, status int, elapsed time.Duration) {
	if route == "" {
		route = "unmatched"
	}
	registry.Lock()
	defer registry.Unlock()
	registry.requests[requestKey{method, route, strconv.Itoa(status/100) + "xx"}]++
	key := durationKey{method, route}
	h := registry.durations[key]
	seconds := elapsed.Seconds()
	h.count++
	h.sum += seconds
	for i, limit := range limits {
		if seconds <= limit {
			h.buckets[i]++
		}
	}
	registry.durations[key] = h
}
func Handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
		registry.RLock()
		defer registry.RUnlock()
		var lines []string
		for k, v := range registry.requests {
			lines = append(lines, fmt.Sprintf("dinapay_http_requests_total{service=%q,method=%q,route=%q,status_class=%q} %d", service, k.method, k.route, k.status, v))
		}
		for k, h := range registry.durations {
			for i, limit := range limits {
				lines = append(lines, fmt.Sprintf("dinapay_http_request_duration_seconds_bucket{service=%q,method=%q,route=%q,le=%q} %d", service, k.method, k.route, strconv.FormatFloat(limit, 'g', -1, 64), h.buckets[i]))
			}
			lines = append(lines, fmt.Sprintf("dinapay_http_request_duration_seconds_bucket{service=%q,method=%q,route=%q,le=\"+Inf\"} %d", service, k.method, k.route, h.count), fmt.Sprintf("dinapay_http_request_duration_seconds_sum{service=%q,method=%q,route=%q} %g", service, k.method, k.route, h.sum), fmt.Sprintf("dinapay_http_request_duration_seconds_count{service=%q,method=%q,route=%q} %d", service, k.method, k.route, h.count))
		}
		for k, v := range registry.provider {
			lines = append(lines, fmt.Sprintf("dinapay_provider_requests_total{provider=\"binancepay\",operation=%q,result=%q} %d", k.operation, k.result, v))
		}
		for operation, h := range registry.providerDurations {
			for i, limit := range limits {
				lines = append(lines, fmt.Sprintf("dinapay_provider_request_duration_seconds_bucket{provider=\"binancepay\",operation=%q,le=%q} %d", operation, strconv.FormatFloat(limit, 'g', -1, 64), h.buckets[i]))
			}
			lines = append(lines, fmt.Sprintf("dinapay_provider_request_duration_seconds_bucket{provider=\"binancepay\",operation=%q,le=\"+Inf\"} %d", operation, h.count), fmt.Sprintf("dinapay_provider_request_duration_seconds_sum{provider=\"binancepay\",operation=%q} %g", operation, h.sum), fmt.Sprintf("dinapay_provider_request_duration_seconds_count{provider=\"binancepay\",operation=%q} %d", operation, h.count))
		}
		for key, v := range registry.operations {
			parts := strings.SplitN(key, ":", 2)
			name := map[string]string{"webhook": "dinapay_provider_webhooks_total", "publish": "dinapay_connector_event_publications_total"}[parts[0]]
			lines = append(lines, fmt.Sprintf("%s{provider=\"binancepay\",result=%q} %d", name, parts[1], v))
		}
		sort.Strings(lines)
		_, _ = fmt.Fprintln(w, strings.Join(lines, "\n"))
	})
}
