package server

import (
	"fmt"
	"math/rand"
	"net/http"
	"os"
	"time"

	"github.com/prometheus/client_golang/prometheus/promhttp"
	"k8s.io/klog/v2"

	"github.com/Camp_K8S/session2/metrics"
)

func ListenAndServe(addr string) {
	mux := http.NewServeMux()
	mux.Handle("/", rootHandler())
	mux.Handle("/healthz", healthHandler())
	mux.Handle("/metrics", promhttp.Handler())

	s := &http.Server{
		Addr:    addr,
		Handler: mux,
	}

	klog.Fatal(s.ListenAndServe())
}

func randInt(min int, max int) int {
	rand.Seed(time.Now().UTC().UnixNano())
	return min + rand.Intn(max-min)
}

func rootHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		klog.Infof("Request from client, IP: %s, return code: %d", r.RemoteAddr, http.StatusOK)
		for key, _ := range r.Header {
			w.Header().Set(key, r.Header.Get(key))
		}

		w.Header().Set("version", os.Getenv("VERSION"))

		timer := metrics.NewTimer()
		defer timer.ObserveTotal()
		delay := randInt(10, 2000)
		time.Sleep(time.Millisecond * time.Duration(delay))
	})
}

func healthHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		klog.Infof("Request from client, IP: %s, return code: %d", r.RemoteAddr, http.StatusOK)
		fmt.Fprint(w, "200\n")
	})
}
