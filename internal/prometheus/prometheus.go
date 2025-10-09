package prometheus

import (
	"fmt"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/mux"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/prometheus/client_golang/prometheus/push"
	"github.com/prometheus/common/expfmt"
)

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

func Push(endpoint, jobName string) {
	var wg sync.WaitGroup

	graphql := newGraphqlCollector()
	pushGateWayClient := push.New(endpoint, jobName).Format(expfmt.FmtText).Collector(graphql)

	ticker := time.NewTicker(1 * time.Second)
	wg.Add(1)
	go func() {
		for range ticker.C {
			if err := pushGateWayClient.Push(); err != nil {
				slog.Error(fmt.Sprintf("Could not push to push gateway %v", err))
			}
		}
	}()
	wg.Wait()
}
