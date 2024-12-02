package prometheus

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/mux"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/vinted/graphql-exporter/internal/config"
	"github.com/vinted/graphql-exporter/internal/graphql"
)

var latencyHistogramBuckets = []float64{.1, .25, .5, 1, 2.5, 5, 10, 15, 20, 30, 40, 50, 60, 90, 150, 210, 270, 330, 390, 450, 500, 600, 1200, 1800, 2700, 3600}

type Graphql struct {
	Data map[string]interface{}
}

type QuerySet struct {
	Query   string
	Metrics []*Metric
}

type Metric struct {
	Collector prometheus.Collector
	Config    config.Metric
	Extractor Extractor
}
type Label struct {
	Name  string
	Value string
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
			var labels []string

			extractor, err := NewExtractor(config.Config.LabelPathSeparator, m.Value, m.Labels)
			if err != nil {
				slog.Error(fmt.Sprintf("labels definition with error on %s: %s", m.Name, err))
			}
			if m.Name == "" {
				name = config.Config.MetricsPrefix + strings.Replace(m.Value, ",", "_", -1)

			} else {
				name = m.Name
			}

			for _, label := range extractor.GetSortedPaths() {
				label = strings.Replace(label, ".*.", "_", -1)
				label = strings.Replace(label, ".", "_", -1)
				labels = append(labels, label)
			}
			switch {
			case m.MetricType == "histogram":
				collector = prometheus.NewHistogramVec(
					prometheus.HistogramOpts{
						Namespace: config.Config.MetricsPrefix,
						Subsystem: q.Subsystem,
						Name:      name,
						Help:      m.Description,
						Buckets:   latencyHistogramBuckets,
					},
					labels)
			default:
				collector = prometheus.NewGaugeVec(
					prometheus.GaugeOpts{
						Namespace: config.Config.MetricsPrefix,
						Subsystem: q.Subsystem,
						Name:      name,
						Help:      m.Description,
					},
					labels,
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
		ctx, cancel := context.WithTimeout(context.Background(), time.Second*time.Duration(config.Config.QueryTimeout))
		queryCtx := context.WithValue(ctx, "query", q.Query)
		result, err := graphql.GraphqlQuery(ctx, q.Query)
		cancel()
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
		data := gql.Data
		if data == nil {
			continue
		}
		for _, m := range q.Metrics {
			metricCtx := context.WithValue(queryCtx, "metric", m.Config.Name)
			callbackFunc := func(value string, labels []string) {
				if value == "" {
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

func (collector *GraphqlCollector) Describe(ch chan<- *prometheus.Desc) {}

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

func (collector *GraphqlCollector) Collect(ch chan<- prometheus.Metric) {
	collector.atomicUpdate(ch)
	collector.accessMu.Lock()
	defer collector.accessMu.Unlock()
	for _, querySet := range collector.cachedQuerySet {
		for _, metric := range querySet.Metrics {
			switch c := metric.Collector.(type) {
			case *prometheus.CounterVec, *prometheus.GaugeVec:
				c.Collect(ch)
			case *prometheus.HistogramVec:
				c.Collect(ch)
				c.Reset()
			default:
				slog.Error(fmt.Sprintf("bad metric type to collect: %+v", c))
			}

		}
	}
}

func staticPage(w http.ResponseWriter, req *http.Request) {
	page := `<html>
    <head><title>Graphql Exporter</title></head>
    <body>
    <h1>Graphql Exporter</h1>
    <p><a href='metrics'>Metrics</a></p>
    </body>
    </html>`
	fmt.Fprintln(w, page)
}

func Start(httpListenAddress string) {
	graphql := newGraphqlCollector()
	prometheus.MustRegister(graphql)

	router := mux.NewRouter()
	router.HandleFunc("/", staticPage)
	router.Path("/metrics").Handler(promhttp.Handler())
	slog.Info("Listening on " + httpListenAddress)
	slog.Error(fmt.Sprintf("%s", http.ListenAndServe(httpListenAddress, router)))
}
