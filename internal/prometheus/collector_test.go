package prometheus

import (
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
	"github.com/stretchr/testify/require"
)

func TestEvictStaleLabels_CounterVec(t *testing.T) {
	t.Parallel()
	c := prometheus.NewCounterVec(prometheus.CounterOpts{Name: "test_evict_counter", Help: "h"}, []string{"job"})
	reg := prometheus.NewPedanticRegistry()
	require.NoError(t, reg.Register(c))

	names := []string{"job"}
	now := time.Now()
	skKeep := labelsToStorageKey(names, []string{"keep"})
	skDrop := labelsToStorageKey(names, []string{"drop"})
	require.NotEmpty(t, skKeep)
	require.NotEmpty(t, skDrop)

	m := &Metric{
		Collector:     c,
		LabelNames:    names,
		Name:          "test_evict_counter",
		labelLastSeen: make(map[string]time.Time),
	}
	m.labelLastSeen[skKeep] = now.Add(-time.Minute)
	m.labelLastSeen[skDrop] = now.Add(-time.Hour)
	c.WithLabelValues("keep").Inc()
	c.WithLabelValues("drop").Inc()
	require.Equal(t, 2, countVecMetrics(reg))

	m.evictStaleLabels(now, 30*time.Minute)

	require.Equal(t, 1, countVecMetrics(reg))
	require.NotContains(t, m.labelLastSeen, skDrop)
	require.Contains(t, m.labelLastSeen, skKeep)
}

func TestEvictStaleLabels_HistogramVec(t *testing.T) {
	t.Parallel()
	h := prometheus.NewHistogramVec(
		prometheus.HistogramOpts{Name: "test_evict_hist", Help: "h", Buckets: []float64{1, 2}},
		[]string{"job"},
	)
	reg := prometheus.NewPedanticRegistry()
	require.NoError(t, reg.Register(h))

	names := []string{"job"}
	now := time.Now()
	skKeep := labelsToStorageKey(names, []string{"keep"})
	skDrop := labelsToStorageKey(names, []string{"drop"})

	m := &Metric{
		Collector:     h,
		LabelNames:    names,
		Name:          "test_evict_hist",
		labelLastSeen: make(map[string]time.Time),
	}
	m.labelLastSeen[skKeep] = now.Add(-time.Minute)
	m.labelLastSeen[skDrop] = now.Add(-time.Hour)
	h.WithLabelValues("keep").Observe(1)
	h.WithLabelValues("drop").Observe(1)
	require.Equal(t, 2, countVecMetrics(reg))

	m.evictStaleLabels(now, 30*time.Minute)

	require.Equal(t, 1, countVecMetrics(reg))
	require.NotContains(t, m.labelLastSeen, skDrop)
	require.Contains(t, m.labelLastSeen, skKeep)
}

func TestEvictStaleLabels_KeepFreshLabel(t *testing.T) {
	t.Parallel()
	g := prometheus.NewGaugeVec(prometheus.GaugeOpts{Name: "test_evict_keep", Help: "h"}, []string{"job"})
	reg := prometheus.NewPedanticRegistry()
	require.NoError(t, reg.Register(g))

	names := []string{"job"}
	now := time.Now()
	skFresh := labelsToStorageKey(names, []string{"fresh"})

	m := &Metric{
		Collector:     g,
		LabelNames:    names,
		Name:          "test_evict_keep",
		labelLastSeen: map[string]time.Time{skFresh: now.Add(-time.Second)},
	}
	g.WithLabelValues("fresh").Set(1)

	m.evictStaleLabels(now, time.Minute)

	require.Equal(t, 1, countVecMetrics(reg))
	require.Contains(t, m.labelLastSeen, skFresh)
	pb := &dto.Metric{}
	v, err := g.GetMetricWithLabelValues("fresh")
	require.NoError(t, err)
	require.NoError(t, v.Write(pb))
	require.Equal(t, 1.0, pb.GetGauge().GetValue())
}

func TestEvictStaleLabels_NoOpWhenTTLZero(t *testing.T) {
	t.Parallel()
	g := prometheus.NewGaugeVec(prometheus.GaugeOpts{Name: "test_evict_zero", Help: "h"}, []string{"job"})
	reg := prometheus.NewPedanticRegistry()
	require.NoError(t, reg.Register(g))

	names := []string{"job"}
	now := time.Now()
	skOld := labelsToStorageKey(names, []string{"old"})

	m := &Metric{
		Collector:     g,
		LabelNames:    names,
		Name:          "test_evict_zero",
		labelLastSeen: map[string]time.Time{skOld: now.Add(-time.Hour)},
	}
	g.WithLabelValues("old").Set(1)

	m.evictStaleLabels(now, 0)

	require.Equal(t, 1, countVecMetrics(reg))
	require.Contains(t, m.labelLastSeen, skOld)
}

func TestEvictStaleLabels_ZeroDimensionSkipped(t *testing.T) {
	t.Parallel()
	m := &Metric{
		LabelNames:    nil,
		Name:          "test_evict_zero_dim",
		labelLastSeen: nil,
	}
	require.NotPanics(t, func() {
		m.evictStaleLabels(time.Now(), time.Minute)
	})
}

func readCounter(c prometheus.Counter) float64 {
	pb := &dto.Metric{}
	if err := c.Write(pb); err != nil {
		return -1
	}
	return pb.GetCounter().GetValue()
}

func readGauge(g prometheus.Gauge) float64 {
	pb := &dto.Metric{}
	if err := g.Write(pb); err != nil {
		return -1
	}
	return pb.GetGauge().GetValue()
}

func TestEvictStaleLabels_UpdatesObservabilityMetrics(t *testing.T) {
	// Cannot use t.Parallel() — touches package-level promauto vecs whose state would race
	// with other tests on the same metric label.
	const metricName = "test_obs_metric_uniq"
	g := prometheus.NewGaugeVec(prometheus.GaugeOpts{Name: "test_obs", Help: "h"}, []string{"job"})

	names := []string{"job"}
	now := time.Now()
	skKeep := labelsToStorageKey(names, []string{"keep"})
	skDrop := labelsToStorageKey(names, []string{"drop"})

	m := &Metric{
		Collector:  g,
		Name:       metricName,
		LabelNames: names,
		labelLastSeen: map[string]time.Time{
			skKeep: now.Add(-time.Minute),
			skDrop: now.Add(-time.Hour),
		},
	}
	g.WithLabelValues("keep").Set(1)
	g.WithLabelValues("drop").Set(1)

	evictedBefore := readCounter(evictedTotal.WithLabelValues(metricName))

	m.evictStaleLabels(now, 30*time.Minute)

	require.Equal(t, evictedBefore+1, readCounter(evictedTotal.WithLabelValues(metricName)))
	require.Equal(t, 1.0, readGauge(trackedLabels.WithLabelValues(metricName)))
}

func TestZeroDimensionGuard_WritePathSkipsNilMap(t *testing.T) {
	t.Parallel()
	// Simulate the inner block of getMetrics() with a zero-dim metric: nil map should not panic
	// and should not be populated.
	m := &Metric{
		LabelNames:    nil,
		Name:          "test_zero_dim_write",
		labelLastSeen: nil,
	}
	// Mirror the production guard: only stamp if map is non-nil.
	stamp := func(labels []string) {
		if m.labelLastSeen == nil {
			return
		}
		sk := labelsToStorageKey(m.LabelNames, labels)
		require.NotEmpty(t, sk)
		m.labelLastSeen[sk] = time.Now()
	}
	require.NotPanics(t, func() { stamp(nil) })
	require.Nil(t, m.labelLastSeen)
}
