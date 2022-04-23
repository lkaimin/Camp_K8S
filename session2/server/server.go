package server

import (
	"fmt"
	"net/http"
	"os"

	"k8s.io/klog/v2"
)

func ListenAndServe(addr string) {
	mux := http.NewServeMux()
	mux.Handle("/", rootHandler())
	mux.Handle("/healthz", healthHandler())

	s := &http.Server{
		Addr:    addr,
		Handler: mux,
	}

	klog.Fatal(s.ListenAndServe())
}

func rootHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		klog.Infof("Request from client, IP: %s, return code: %d", r.RemoteAddr, http.StatusOK)
		for key, _ := range r.Header {
			w.Header().Set(key, r.Header.Get(key))
		}

		w.Header().Set("version", os.Getenv("VERSION"))
	})
}

func healthHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		klog.Infof("Request from client, IP: %s, return code: %d", r.RemoteAddr, http.StatusOK)
		fmt.Fprint(w, "200\n")
	})
}
