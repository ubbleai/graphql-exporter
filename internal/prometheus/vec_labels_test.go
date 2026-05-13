package prometheus

import (
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
	"github.com/stretchr/testify/require"
)

func TestLabelsStorageKeyRoundTrip(t *testing.T) {
	t.Parallel()
	names := []string{"job", "stage", "ref"}
	cases := [][]string{
		{"a", "b", "c"},
		{"", "", ""},
		{`foo"bar`, "x\ny", "refs/heads/main"},
	}
	for _, values := range cases {
		key := labelsToStorageKey(names, values)
		require.NotEmpty(t, key)
		got := labelValuesFromStorageKey(names, key)
		require.Equal(t, values, got, "key=%s", key)
	}
}

func TestLabelsStorageKeyZeroDimensions(t *testing.T) {
	t.Parallel()
	require.Equal(t, "{}", labelsToStorageKey(nil, nil))
	require.Empty(t, labelValuesFromStorageKey(nil, "{}"))
}

func TestLabelsStorageKeyMismatch(t *testing.T) {
	t.Parallel()
	require.Equal(t, "", labelsToStorageKey([]string{"a"}, []string{"x", "y"}))
}

func countVecMetrics(reg prometheus.Gatherer) int {
	mfs, err := reg.Gather()
	if err != nil {
		return -1
	}
	n := 0
	for _, mf := range mfs {
		n += len(mf.Metric)
	}
	return n
}

func TestEvictStaleLabels_GaugeVec(t *testing.T) {
	t.Parallel()
	g := prometheus.NewGaugeVec(prometheus.GaugeOpts{Name: "test_evict_gauge", Help: "h"}, []string{"job"})
	reg := prometheus.NewPedanticRegistry()
	require.NoError(t, reg.Register(g))

	names := []string{"job"}
	now := time.Now()
	skKeep := labelsToStorageKey(names, []string{"keep"})
	skDrop := labelsToStorageKey(names, []string{"drop"})
	require.NotEmpty(t, skKeep)
	require.NotEmpty(t, skDrop)

	m := &Metric{
		Collector:     g,
		LabelNames:    names,
		labelLastSeen: make(map[string]time.Time),
	}
	m.labelLastSeen[skKeep] = now.Add(-time.Minute)
	m.labelLastSeen[skDrop] = now.Add(-time.Hour)
	g.WithLabelValues("keep").Set(1)
	g.WithLabelValues("drop").Set(2)

	require.Equal(t, 2, countVecMetrics(reg))

	m.evictStaleLabels(now, 30*time.Minute)

	require.Equal(t, 1, countVecMetrics(reg))
	pb := &dto.Metric{}
	v, err := g.GetMetricWithLabelValues("keep")
	require.NoError(t, err)
	require.NoError(t, v.Write(pb))
	require.Equal(t, 1.0, pb.GetGauge().GetValue())
}
