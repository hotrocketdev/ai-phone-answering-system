package main

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/gorilla/websocket"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/voxlane/voice-gateway/internal/config"
	goredis "github.com/voxlane/voice-gateway/internal/redis"
	"github.com/voxlane/voice-gateway/internal/session"
	"github.com/voxlane/voice-gateway/internal/twilio"
)

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool { return true }, // Twilio doesn't send Origin
}

var activeSessions sync.Map

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("failed to load config: %v", err)
	}

	// Redis client (graceful if unavailable)
	var redisClient *goredis.Client
	redisClient, err = goredis.NewClient(goredis.Config{
		Addr:     cfg.RedisAddr,
		Password: cfg.RedisPassword,
	})
	if err != nil {
		log.Printf("WARNING: Redis unavailable (%v) — sessions will be in-memory only", err)
		redisClient = nil
	} else {
		log.Printf("Redis connected: %s", cfg.RedisAddr)
	}
	defer func() {
		if redisClient != nil {
			redisClient.Close()
		}
	}()

	mux := http.NewServeMux()

	// Health endpoint
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, `{"status":"ok"}`)
	})

	// Readiness endpoint
	mux.HandleFunc("GET /health/ready", func(w http.ResponseWriter, _ *http.Request) {
		redisOK := redisClient != nil
		if redisClient != nil {
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			redisOK = redisClient.Ping(ctx) == nil
		}

		count := 0
		activeSessions.Range(func(_, _ interface{}) bool { count++; return true })

		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"status":"ready","redis":%t,"activeSessions":%d}`, redisOK, count)
	})

	// Prometheus metrics
	mux.Handle("GET /metrics", promhttp.Handler())

	// Proxy Twilio voice webhooks to NestJS backend
	mux.HandleFunc("POST /api/public/voice/webhook", func(w http.ResponseWriter, r *http.Request) {
		proxyToBackend(w, r, cfg.NestJSUrl+"/api/public/voice/webhook")
	})
	mux.HandleFunc("POST /api/public/voice/status-callback", func(w http.ResponseWriter, r *http.Request) {
		proxyToBackend(w, r, cfg.NestJSUrl+"/api/public/voice/status-callback")
	})

	// WebSocket stream endpoint for Twilio Media Streams
	mux.HandleFunc("GET /stream/{callSid}", func(w http.ResponseWriter, r *http.Request) {
		callSid := r.PathValue("callSid")
		log.Printf("[%s] Twilio Media Stream connecting...", callSid)

		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			log.Printf("[%s] WebSocket upgrade failed: %v", callSid, err)
			return
		}

		// Create Twilio handler
		tw := twilio.NewHandler(conn, callSid)

		// Create session with Redis persistence
		sess := session.NewSession(callSid, tw, cfg, redisClient)

		// Track active session
		activeSessions.Store(callSid, sess)
		defer activeSessions.Delete(callSid)

		// Start Twilio read loop
		go tw.ReadLoop()

		// Run session lifecycle
		log.Printf("[%s] session starting", callSid)
		if err := sess.Run(r.Context()); err != nil {
			log.Printf("[%s] session error: %v", callSid, err)
		}
		log.Printf("[%s] session ended", callSid)
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
		log.Printf("received signal %v, draining %d active sessions...", sig, countSessions())

		// Drain: stop accepting new connections, wait for active calls
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		activeSessions.Range(func(key, value interface{}) bool {
			if s, ok := value.(*session.Session); ok {
				s.Stop()
			}
			return true
		})

		if err := server.Shutdown(ctx); err != nil {
			log.Printf("forced shutdown: %v", err)
		}
	}()

	log.Printf("VoxLane Voice Gateway starting on :%d", cfg.Port)
	log.Printf("  Health:   http://localhost:%d/health", cfg.Port)
	log.Printf("  Metrics:  http://localhost:%d/metrics", cfg.Port)
	log.Printf("  Stream:   ws://localhost:%d/stream/{callSid}", cfg.Port)
	log.Printf("  NestJS:   %s", cfg.NestJSUrl)
	if redisClient != nil {
		log.Printf("  Redis:    %s", cfg.RedisAddr)
	} else {
		log.Printf("  Redis:    unavailable (in-memory mode)")
	}

	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatalf("server error: %v", err)
	}

	log.Println("server stopped gracefully")
}

func countSessions() int {
	count := 0
	activeSessions.Range(func(_, _ interface{}) bool { count++; return true })
	return count
}

// proxyToBackend forwards HTTP requests to the NestJS backend.
func proxyToBackend(w http.ResponseWriter, r *http.Request, targetURL string) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "read body failed", http.StatusInternalServerError)
		return
	}
	defer r.Body.Close()

	req, err := http.NewRequestWithContext(r.Context(), r.Method, targetURL, bytes.NewReader(body))
	if err != nil {
		http.Error(w, "proxy request failed", http.StatusInternalServerError)
		return
	}
	req.Header.Set("Content-Type", r.Header.Get("Content-Type"))

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		log.Printf("proxy to %s failed: %v", targetURL, err)
		http.Error(w, "backend unavailable", http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)
	w.Header().Set("Content-Type", resp.Header.Get("Content-Type"))
	w.WriteHeader(resp.StatusCode)
	w.Write(respBody)
}
