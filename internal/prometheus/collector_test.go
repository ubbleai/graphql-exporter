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
