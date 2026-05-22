package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/voxlane/voice-gateway/internal/config"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("failed to load config: %v", err)
	}

	mux := http.NewServeMux()

	// Health endpoint
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, `{"status":"ok"}`)
	})

	// Readiness endpoint
	mux.HandleFunc("GET /health/ready", func(w http.ResponseWriter, _ *http.Request) {
		// TODO: check Redis + OpenAI reachability
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, `{"status":"ready"}`)
	})

	// WebSocket stream endpoint (placeholder)
	mux.HandleFunc("GET /stream/{callSid}", func(w http.ResponseWriter, r *http.Request) {
		callSid := r.PathValue("callSid")
		log.Printf("stream request received for callSid=%s", callSid)
		// TODO: upgrade to WebSocket, create session, start audio pipeline
		w.WriteHeader(http.StatusNotImplemented)
		fmt.Fprintf(w, "WebSocket stream endpoint for callSid=%s (not yet implemented)", callSid)
	})

	server := &http.Server{
		Addr:         fmt.Sprintf(":%d", cfg.Port),
		Handler:      mux,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 10 * time.Second,
		IdleTimeout:  120 * time.Second,
	}

	// Graceful shutdown
	go func() {
		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
		sig := <-sigCh
		log.Printf("received signal %v, shutting down...", sig)

		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		if err := server.Shutdown(ctx); err != nil {
			log.Printf("forced shutdown: %v", err)
		}
	}()

	log.Printf("VoxLane Voice Gateway starting on :%d", cfg.Port)
	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatalf("server error: %v", err)
	}

	log.Println("server stopped gracefully")
}
