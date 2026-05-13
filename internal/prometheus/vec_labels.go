package prometheus

import (
	"encoding/json"

	"github.com/prometheus/client_golang/prometheus"
)

// labelsToStorageKey encodes label names and values as a stable string for map[...]time.Time.
// It uses prometheus.Labels (map name → value); json.Marshal sorts object keys, so the key is deterministic.
// Zero dimensions use "{}" (empty JSON object). Returns "" if len(names) != len(values).
func labelsToStorageKey(labelNames, labelValues []string) string {
	if len(labelNames) != len(labelValues) {
		return ""
	}
	if len(labelNames) == 0 {
		return "{}"
	}
	lb := make(prometheus.Labels, len(labelNames))
	for i := range labelNames {
		lb[labelNames[i]] = labelValues[i]
	}
	b, err := json.Marshal(lb)
	if err != nil {
		return ""
	}
	return string(b)
}

// labelValuesFromStorageKey decodes a key from labelsToStorageKey into the slice order expected by
// WithLabelValues / DeleteLabelValues for this metric's labelNames.
func labelValuesFromStorageKey(labelNames []string, key string) []string {
	if key == "" || key == "{}" {
		return nil
	}
	var lb prometheus.Labels
	if json.Unmarshal([]byte(key), &lb) != nil {
		return nil
	}
	out := make([]string, len(labelNames))
	for i, n := range labelNames {
		v, ok := lb[n]
		if !ok {
			return nil
		}
		out[i] = v
	}
	return out
}

func vecDeleteLabelValues(c prometheus.Collector, lvs []string) bool {
	switch v := c.(type) {
	case *prometheus.HistogramVec:
		return v.DeleteLabelValues(lvs...)
	case *prometheus.GaugeVec:
		return v.DeleteLabelValues(lvs...)
	case *prometheus.CounterVec:
		return v.DeleteLabelValues(lvs...)
	default:
		return false
	}
}
