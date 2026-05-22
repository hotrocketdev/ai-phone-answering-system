// Package metrics provides Prometheus metrics for the voice gateway.
package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
	CallsActive = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "voxlane_calls_active",
		Help: "Number of active calls",
	})

	CallsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "voxlane_calls_total",
		Help: "Total calls handled",
	}, []string{"outcome"})

	CallDuration = promauto.NewHistogram(prometheus.HistogramOpts{
		Name:    "voxlane_call_duration_seconds",
		Help:    "Call duration distribution",
		Buckets: []float64{30, 60, 90, 120, 180, 300, 600, 900, 1800},
	})

	BargeInTotal = promauto.NewCounter(prometheus.CounterOpts{
		Name: "voxlane_barge_in_total",
		Help: "Total barge-in events",
	})

	AudioInputSeconds = promauto.NewCounter(prometheus.CounterOpts{
		Name: "voxlane_audio_input_seconds_total",
		Help: "Total seconds of caller audio",
	})

	AudioOutputSeconds = promauto.NewCounter(prometheus.CounterOpts{
		Name: "voxlane_audio_output_seconds_total",
		Help: "Total seconds of AI audio played",
	})

	OpenAIReconnects = promauto.NewCounter(prometheus.CounterOpts{
		Name: "voxlane_openai_reconnects_total",
		Help: "Total OpenAI WebSocket reconnection attempts",
	})

	ToolCallTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "voxlane_tool_calls_total",
		Help: "Tool calls by tool name and result",
	}, []string{"tool", "result"})
)
