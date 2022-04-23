package main

import (
	"flag"

	"github.com/Camp_K8S/session2/server"
	"k8s.io/klog/v2"
)

func main() {
	klog.InitFlags(nil)
	flag.Parse()

	klog.Info("Listen and serve...")
	server.ListenAndServe(":5432")
}
