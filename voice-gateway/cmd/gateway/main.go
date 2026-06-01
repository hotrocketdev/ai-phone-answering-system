package main

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log"
	"math"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/gorilla/websocket"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/voxlane/voice-gateway/internal/audio"
	"github.com/voxlane/voice-gateway/internal/config"
	"github.com/voxlane/voice-gateway/internal/provider"
	providertelnyx "github.com/voxlane/voice-gateway/internal/provider/telnyx"
	providertwilio "github.com/voxlane/voice-gateway/internal/provider/twilio"
	goredis "github.com/voxlane/voice-gateway/internal/redis"
	cartesiarend "github.com/voxlane/voice-gateway/internal/renderer/cartesia"
	"github.com/voxlane/voice-gateway/internal/runtime"
	dgagent "github.com/voxlane/voice-gateway/internal/runtime/deepgram"
	"github.com/voxlane/voice-gateway/internal/session"
)

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool { return true }, // Twilio doesn't send Origin
}

var activeSessions sync.Map
var buildCommit = "dev"

func main() {
	// Log to file for diagnostics
	logFile, _ := os.OpenFile("C:\\Builds\\AI-Phone-Answer-System\\gateway.log", os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if logFile != nil {
		log.SetOutput(io.MultiWriter(os.Stderr, logFile))
		defer logFile.Close()
	}

	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("failed to load config: %v", err)
	}
	log.Printf("gateway build commit: %s", buildCommit)

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

	// Log all requests for debugging
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		log.Printf("REQUEST: %s %s from %s", r.Method, r.URL.Path, r.RemoteAddr)
	})

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

		// Create provider adapter
		var adapter provider.Adapter
		pCfg := provider.Config{
			ProviderType: provider.Type(cfg.VoiceProvider),
			Twilio: provider.TwilioConfig{
				AccountSID: cfg.TwilioAccountSID,
				AuthToken:  cfg.TwilioAuthToken,
			},
			Telnyx: provider.TelnyxConfig{
				APIKey:             cfg.TelnyxAPIKey,
				ConnectionID:       cfg.TelnyxConnectionID,
				StreamCodec:        cfg.TelnyxStreamCodec,
				BidirectionalCodec: cfg.TelnyxBidirectionalCodec,
			},
		}
		switch provider.Type(cfg.VoiceProvider) {
		case provider.TypeTwilio:
			adapter = providertwilio.New(conn, callSid, pCfg.Twilio)
		case provider.TypeTelnyx:
			adapter = providertelnyx.New(conn, callSid, pCfg.Telnyx)
		default:
			log.Printf("[%s] unsupported provider: %s", callSid, cfg.VoiceProvider)
			conn.Close()
			return
		}

		// Start provider read loop (both Twilio and Telnyx expose ReadLoop)
		switch a := adapter.(type) {
		case *providertwilio.Adapter:
			go a.ReadLoop()
		case *providertelnyx.Adapter:
			go a.ReadLoop()
		}

		if os.Getenv("DEBUG_TELNYX_TEST_TONE") == "true" {
			if telnyxAdapter, ok := adapter.(*providertelnyx.Adapter); ok {
				log.Printf("[%s] DEBUG: Telnyx PCMU test tone", callSid)
				runTelnyxTestTone(r.Context(), callSid, telnyxAdapter)
				return
			}
		}

		// Cartesia direct greeting bypass (Twilio-specific debug path)
		if os.Getenv("DEBUG_CARTESIA_DIRECT_GREETING") == "true" {
			if twAdapter, ok := adapter.(*providertwilio.Adapter); ok {
				log.Printf("[%s] DEBUG: Cartesia direct greeting", callSid)
				runCartesiaDirectGreeting(r.Context(), callSid, twAdapter, cfg)
				return
			}
		}

		// Deepgram runtime path (Twilio only)
		twAdapter, _ := adapter.(*providertwilio.Adapter)
		if cfg.VoiceRuntime == "deepgram_agent" {
			if twAdapter == nil {
				log.Printf("[%s] deepgram requires Twilio provider", callSid)
				conn.Close()
				return
			}
			log.Printf("[%s] using Deepgram Voice Agent", callSid)
			dgCfg := runtime.Config{
				Provider:              runtime.ProviderDeepgramAgent,
				DeepgramAPIKey:        cfg.DeepgramAPIKey,
				DeepgramListenModel:   cfg.DeepgramListenModel,
				DeepgramListenLang:    cfg.DeepgramListenLang,
				DeepgramTTSModel:      cfg.DeepgramTTSModel,
				DeepgramThinkProvider: cfg.DeepgramThinkProvider,
				DeepgramThinkModel:    cfg.DeepgramThinkModel,
				OpenAIAPIKey:          cfg.OpenAIAPIKey,
				BusinessName:          cfg.BusinessName,
			}
			agent, err := dgagent.New(r.Context(), dgCfg)
			if err != nil {
				log.Printf("[%s] deepgram init failed: %v", callSid, err)
				return
			}
			defer agent.Close()
			activeSessions.Store(callSid, agent)
			defer activeSessions.Delete(callSid)

			if err := agent.Start(r.Context()); err != nil {
				log.Printf("[%s] deepgram start failed: %v", callSid, err)
				return
			}

			// Relay: Twilio inbound → Deepgram, Deepgram outbound → Twilio
			runDeepgramRelay(r.Context(), callSid, twAdapter, agent)
			return
		}

		// Create session (custom runtime)
		sess := session.NewSession(callSid, adapter, cfg, redisClient)

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
	log.Printf("  Safe config status:")
	log.Printf("    VOICE_RUNTIME=%s", cfg.VoiceRuntime)
	log.Printf("    VOICE_RENDERER=%s", cfg.VoiceRenderer)
	log.Printf("    CARTESIA_API_KEY present=%t", cfg.CartesiaAPIKey != "")
	log.Printf("    CARTESIA_VOICE_ID present=%t", cfg.CartesiaVoiceID != "")
	log.Printf("    CARTESIA_MODEL=%s", cfg.CartesiaModel)
	log.Printf("    CARTESIA_LANGUAGE=%s", cfg.CartesiaLanguage)
	log.Printf("    CARTESIA_SPEED=%.2f", cfg.CartesiaSpeed)
	log.Printf("    CARTESIA_VOLUME=%.2f", cfg.CartesiaVolume)
	log.Printf("    CARTESIA_EMOTION=%s", cfg.CartesiaEmotion)
	log.Printf("    OPENAI_API_KEY present=%t", cfg.OpenAIAPIKey != "")
	if len(cfg.OpenAIAPIKey) >= 4 {
		log.Printf("    OPENAI_API_KEY suffix=%s", cfg.OpenAIAPIKey[len(cfg.OpenAIAPIKey)-4:])
	}
	log.Printf("    BUSINESS_NAME=%s", cfg.BusinessName)
	if os.Getenv("BUSINESS_NAME") != "" {
		log.Printf("    BUSINESS_NAME source=env")
	} else {
		log.Printf("    BUSINESS_NAME source=default")
	}
	log.Printf("  Model:    %s", cfg.OpenAIRealtimeModel)
	log.Printf("  Voice:    marin")
	log.Printf("  Runtime:  %s", cfg.VoiceRuntime)
	log.Printf("  VAD:      semantic_vad (medium eagerness)")
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

// runDeepgramRelay relays audio between Twilio and Deepgram.
// Twilio: u-law 8kHz. Deepgram: linear16 48kHz in, linear16 24kHz out.
func runDeepgramRelay(ctx context.Context, callSid string, tw *providertwilio.Adapter, agent *dgagent.Agent) {
	outPipe := audio.NewPipeline() // Twilio u-law → PCM16 24kHz → Deepgram

	// Twilio u-law 8kHz → PCM16 24kHz → Deepgram
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case frame, ok := <-tw.Frames:
				if !ok {
					return
				}
				pcm24k := outPipe.ProcessInboundBytes(frame.Payload)
				agent.SendAudio(pcm24k)
			}
		}
	}()

	// Deepgram u-law → Twilio (direct — no conversion needed)
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case audio, ok := <-agent.AudioOut():
				if !ok {
					return
				}
				// Deepgram sends u-law 8kHz — split into 160-byte Twilio frames
				for i := 0; i < len(audio); i += 160 {
					end := i + 160
					if end > len(audio) {
						end = len(audio)
					}
					frame := audio[i:end]
					if len(frame) < 160 {
						padded := make([]byte, 160)
						copy(padded, frame)
						frame = padded
					}
					msg, err := tw.EncodeAudio(provider.AudioFrame{
						Codec: "ulaw", SampleRate: 8000, Payload: frame, Direction: "outbound",
					})
					if err == nil && msg != nil {
						if err := tw.WriteRaw(msg); err != nil {
							log.Printf("[%s] outbound media write failed source=deepgram: %v", callSid, err)
						} else {
							log.Printf("[%s] outbound media frame sent source=deepgram", callSid)
						}
					}
				}
			}
		}
	}()
	<-ctx.Done()
	log.Printf("[%s] deepgram relay ended", callSid)
}

// runCartesiaDirectGreeting bypasses OpenAI and sends a fixed greeting directly to Cartesia.
func runCartesiaDirectGreeting(ctx context.Context, callSid string, tw *providertwilio.Adapter, cfg *config.Config) {
	r := cartesiarend.New(cartesiarend.Config{
		APIKey:   cfg.CartesiaAPIKey,
		VoiceID:  cfg.CartesiaVoiceID,
		ModelID:  cfg.CartesiaModel,
		Language: cfg.CartesiaLanguage,
		Speed:    cfg.CartesiaSpeed,
		Volume:   cfg.CartesiaVolume,
		Emotion:  cfg.CartesiaEmotion,
	})
	defer r.Close()

	text := fmt.Sprintf("Good afternoon, %s, how can I help?", cfg.CustomerName())
	log.Printf("[%s] cartesia direct: sending text (%d chars)", callSid, len(text))

	audioCh, err := r.RenderStream(ctx, text)
	if err != nil {
		log.Printf("[%s] cartesia direct failed: %v", callSid, err)
		return
	}

	chunkCount := 0
	for chunk := range audioCh {
		chunkCount++
		for i := 0; i < len(chunk); i += 160 {
			end := i + 160
			if end > len(chunk) {
				end = len(chunk)
			}
			frame := chunk[i:end]
			if len(frame) < 160 {
				padded := make([]byte, 160)
				copy(padded, frame)
				frame = padded
			}
			msg, err := tw.EncodeAudio(provider.AudioFrame{
				Codec: "ulaw", SampleRate: 8000, Payload: frame, Direction: "outbound",
			})
			if err == nil && msg != nil {
				if err := tw.WriteRaw(msg); err != nil {
					log.Printf("[%s] outbound media write failed source=cartesia: %v", callSid, err)
				} else {
					log.Printf("[%s] outbound media frame sent source=cartesia", callSid)
				}
			}
		}
	}
	log.Printf("[%s] cartesia direct: %d chunks, %d total bytes sent to Twilio", callSid, chunkCount, chunkCount*160)
}

func runTelnyxTestTone(ctx context.Context, callSid string, telnyx *providertelnyx.Adapter) {
	const (
		sampleRate = 8000
		frameSize  = 160
		frames     = 50
		frequency  = 440.0
		amplitude  = 12000.0
	)

	log.Printf("[%s] telnyx test_tone waiting for stream readiness", callSid)
	select {
	case <-ctx.Done():
		log.Printf("[%s] telnyx test_tone stopped before send: context done", callSid)
		return
	case <-time.After(2 * time.Second):
	}

	for frameIndex := 0; frameIndex < frames; frameIndex++ {
		select {
		case <-ctx.Done():
			log.Printf("[%s] telnyx test_tone stopped: context done", callSid)
			return
		default:
		}

		frame := make([]byte, frameSize)
		toneOn := (frameIndex/5)%2 == 0
		for i := 0; i < frameSize; i++ {
			if !toneOn {
				frame[i] = audio.EncodePCM16ToMulaw(0)
				continue
			}
			sampleIndex := frameIndex*frameSize + i
			sample := int16(math.Sin(2*math.Pi*frequency*float64(sampleIndex)/sampleRate) * amplitude)
			frame[i] = audio.EncodePCM16ToMulaw(sample)
		}

		msg, err := telnyx.EncodeAudio(provider.AudioFrame{
			Codec: "pcmu", SampleRate: sampleRate, Payload: frame, Direction: "outbound",
		})
		if err != nil {
			log.Printf("[%s] telnyx test_tone encode failed: %v", callSid, err)
			return
		}
		if err := telnyx.WriteRaw(msg); err != nil {
			log.Printf("[%s] telnyx test_tone write failed: %v", callSid, err)
			return
		}
		if frameIndex < 5 {
			log.Printf("[%s] outbound media frame sent source=test_tone payload_len=%d", callSid, len(frame))
		}
		time.Sleep(20 * time.Millisecond)
	}

	log.Printf("[%s] telnyx test_tone complete: %d frames sent", callSid, frames)
	time.Sleep(2 * time.Second)
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
