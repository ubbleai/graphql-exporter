package prometheus

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/vinted/graphql-exporter/internal/config"
	"github.com/vinted/graphql-exporter/internal/graphql"
)

type Graphql struct {
	Errors []interface{}          `json:"errors"`
	Data   map[string]interface{} `json:"data"`
}

type QuerySet struct {
	Query       string
	Metrics     []*Metric
	PreviousRun time.Time
}

type Metric struct {
	Collector prometheus.Collector
	Labels    []string
	Config    config.Metric
	Extractor Extractor
}

type GraphqlCollector struct {
	cachedQuerySet   []*QuerySet
	cachedAt         int64
	updaterIsRunning bool
	updaterMu        sync.Mutex
	accessMu         sync.Mutex
	graphqlURL       string
}

// Build Prometheux MetricVec with label dimensions.
func newGraphqlCollector() *GraphqlCollector {
	var cachedQuerySet []*QuerySet

	for _, q := range config.Config.Queries {
		var metrics []*Metric
		for _, m := range q.Metrics {
			var collector prometheus.Collector
			var name string
			var labelNames []string

			extractor, err := NewExtractor(config.Config.LabelPathSeparator, m.Value, m.Labels)
			if err != nil {
				slog.Error(fmt.Sprintf("labels definition with error on %s: %s", m.Name, err))
			}
			if m.Name == "" {
				name = config.Config.MetricsPrefix + strings.Replace(m.Value, ",", "_", -1)

			} else {
				name = m.Name
			}

			sortedLabels := extractor.GetSortedLabels()
			for _, label := range sortedLabels {
				labelNames = append(labelNames, label.Alias)
			}

			switch {
			case m.MetricType == "histogram":
				if len(m.HistogramBuckets) == 0 {
					m.HistogramBuckets = prometheus.DefBuckets
				}
				collector = prometheus.NewHistogramVec(
					prometheus.HistogramOpts{
						Namespace: config.Config.MetricsPrefix,
						Subsystem: q.Subsystem,
						Name:      name,
						Help:      m.Description,
						Buckets:   m.HistogramBuckets,
					},
					labelNames)
			case m.MetricType == "counter":
				collector = prometheus.NewCounterVec(
					prometheus.CounterOpts{
						Namespace: config.Config.MetricsPrefix,
						Subsystem: q.Subsystem,
						Name:      name,
						Help:      m.Description,
					},
					labelNames)
			default:
				collector = prometheus.NewGaugeVec(
					prometheus.GaugeOpts{
						Namespace: config.Config.MetricsPrefix,
						Subsystem: q.Subsystem,
						Name:      name,
						Help:      m.Description,
					},
					labelNames,
				)
			}
			metrics = append(metrics, &Metric{
				Collector: collector,
				Config:    m,
				Extractor: extractor,
			})
		}
		querySet := &QuerySet{
			Query:   q.Query,
			Metrics: metrics,
			// PreviousRun: time.Now().Truncate(time.Hour * 24 * 180),
			PreviousRun: time.Now(),
		}
		cachedQuerySet = append(cachedQuerySet, querySet)
	}

	return &GraphqlCollector{
		cachedQuerySet: cachedQuerySet,
		updaterMu:      sync.Mutex{},
		accessMu:       sync.Mutex{},
		graphqlURL:     config.Config.GraphqlURL,
	}
}

func (collector *GraphqlCollector) getMetrics() error {
	var gql *Graphql

	for _, q := range collector.cachedQuerySet {
		// nextRun := q.PreviousRun.Add(5 * time.Minute)
		now := time.Now()
		nextRun := now.Add(time.Second * time.Duration(config.Config.CacheExpire))
		slog.Debug(fmt.Sprintf("previous run %s", q.PreviousRun.Format(time.RFC3339)))
		slog.Debug(fmt.Sprintf("next run %s", nextRun.Format(time.RFC3339)))
		ctx, cancel := context.WithTimeout(context.Background(), time.Second*time.Duration(config.Config.QueryTimeout))
		queryCtx := context.WithValue(ctx, "query", q.Query)
		result, err := graphql.GraphqlQuery(ctx, q.Query, q.PreviousRun, now, nextRun)
		cancel()
		slog.Debug(fmt.Sprintf("result found %+v", string(result)))
		if err != nil {
			if config.Config.FailFast {
				return err
			} else {
				slog.Error(fmt.Sprintf("query error: %s", err))
				continue
			}
		}

		err = json.Unmarshal(result, &gql)
		if err != nil {
			if config.Config.FailFast {
				return err
			} else {
				slog.Error(fmt.Sprintf("unmarshal error: %s", err))
				continue
			}
		}
		if len(gql.Errors) > 0 {
			slog.Error(fmt.Sprintf("graphql error %+v", gql.Errors))
		}
		data := gql.Data
		q.PreviousRun = now
		// q.PreviousRun = nextRun
		if data == nil {
			continue
		}
		slog.Debug(fmt.Sprintf("data found %+v", data))
		for _, m := range q.Metrics {
			metricCtx := context.WithValue(queryCtx, "metric", m.Config.Name)
			callbackFunc := func(value string, labels []string) {
				if value == "" || value == "<nil>" {
					return
				}
				switch v := m.Collector.(type) {
				case *prometheus.HistogramVec:
					f, err := strconv.ParseFloat(value, 64)
					if err != nil {
						slog.ErrorContext(metricCtx, "fail to convert metric to float", slog.String("value", value))
					}
					v.WithLabelValues(labels...).Observe(f)
				case *prometheus.GaugeVec:
					f, err := strconv.ParseFloat(value, 64)
					if err != nil {
						slog.ErrorContext(metricCtx, "fail to convert metric to float", slog.String("value", value))
					}
					v.WithLabelValues(labels...).Set(f)
				case *prometheus.CounterVec:
					f, err := strconv.ParseFloat(value, 64)
					if err != nil || f < 0 {
						f = 1
					}
					v.WithLabelValues(labels...).Add(f)
				default:
					slog.Error(fmt.Sprintf("unsuported collector type %v", v))
				}
			}
			m.Extractor.ExtractMetrics(data, callbackFunc)
		}
	}
	return nil
}

func (collector *GraphqlCollector) updateMetrics() error {
	if time.Now().Unix()-collector.cachedAt > config.Config.CacheExpire {
		collector.accessMu.Lock()
		defer collector.accessMu.Unlock()
		err := collector.getMetrics()
		if err != nil {
			slog.Error(fmt.Sprintf("error collecting metrics: %s", err))
			if config.Config.ExtendCacheOnError {
				collector.cachedAt = time.Now().Unix()
			}
			return err
		} else {
			collector.cachedAt = time.Now().Unix()
		}
	}
	return nil
}

func (collector *GraphqlCollector) atomicUpdate(ch chan<- prometheus.Metric) {
	collector.updaterMu.Lock()
	start := !collector.updaterIsRunning
	collector.updaterIsRunning = true
	collector.updaterMu.Unlock()
	if start {
		go func() {
			collector.updateMetrics()
			collector.updaterMu.Lock()
			collector.updaterIsRunning = false
			collector.updaterMu.Unlock()
		}()
	}
}

func (collector *GraphqlCollector) Describe(ch chan<- *prometheus.Desc) {}

func (collector *GraphqlCollector) Collect(ch chan<- prometheus.Metric) {
	collector.atomicUpdate(ch)
	collector.accessMu.Lock()
	defer collector.accessMu.Unlock()
	for _, querySet := range collector.cachedQuerySet {
		for _, metric := range querySet.Metrics {
			wrappedCh := make(chan prometheus.Metric)
			go func() {
				metric.Collector.Collect(wrappedCh)
				close(wrappedCh)
			}()
			for m := range wrappedCh {
				s := prometheus.NewMetricWithTimestamp(querySet.PreviousRun, m)
				ch <- s
			}

		}
	}
}
