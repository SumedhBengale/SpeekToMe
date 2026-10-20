package main

import (
	"context"
	"flag"
	"golang.org/x/sys/unix"
	log "speektome-server/privacylog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"speektome-server/orchestrator"
	"syscall"
	"time"
)

func main() {
	path := flag.String("config", "", "fleet JSON configuration")
	check := flag.Bool("check", false, "validate config and credentials/discover managed instances without mutations")
	flag.Parse()
	c, e := orchestrator.LoadConfig(*path)
	if e != nil {
		log.Fatal(e)
	}
	cloud := orchestrator.NewAWSCloud()
	probe, e := orchestrator.NewHTTPProbe(c, cloud)
	if e != nil {
		log.Fatal(e)
	}
	if *check {
		ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
		defer cancel()
		snapshot, e := cloud.Discover(ctx, c)
		if e != nil {
			log.Fatal(e)
		}
		log.Printf("configuration valid; %d tagged instances discovered", len(snapshot.Workers))
		return
	}
	if e = os.MkdirAll(filepath.Dir(c.StateFile), 0700); e != nil {
		log.Fatal(e)
	}
	lock, e := os.OpenFile(c.StateFile+".lock", os.O_CREATE|os.O_RDWR, 0600)
	if e != nil {
		log.Fatal(e)
	}
	defer lock.Close()
	if e = unix.Flock(int(lock.Fd()), unix.LOCK_EX|unix.LOCK_NB); e != nil {
		log.Fatal("another controller owns this state file")
	}
	controller, e := orchestrator.NewController(c, cloud, probe)
	if e != nil {
		log.Fatal(e)
	}
	controller.Start()
	controller.StartEvents()
	defer controller.Stop()
	mux := http.NewServeMux()
	mux.Handle("/v1/", controller.ProxyHandler(probe))
	mux.Handle("/", controller.Handler())
	server := &http.Server{Addr: c.Listen, Handler: mux, ReadHeaderTimeout: 5 * time.Second, IdleTimeout: 30 * time.Second, MaxHeaderBytes: 16 << 10}
	done := make(chan os.Signal, 1)
	signal.Notify(done, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-done
		controller.Stop()
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		server.Shutdown(ctx)
	}()
	log.Printf("fixed-size fleet controller on %s", c.Listen)
	if e = server.ListenAndServe(); e != nil && e != http.ErrServerClosed {
		log.Fatal(e)
	}
}
