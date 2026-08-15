package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetricgrpc"
	"go.opentelemetry.io/otel/exporters/stdout/stdoutmetric"
	metricapi "go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	semconv "go.opentelemetry.io/otel/semconv/v1.43.0"
)

func main() {
	// Set up logging. Level is read from LOG_LEVEL (debug|info|warn|error)
	// and held in a LevelVar so it can be changed at runtime.
	level := new(slog.LevelVar)
	level.Set(parseLevel(os.Getenv("LOG_LEVEL")))
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: level})))

	// Use 3s/1m/etc. for SAMPLE_INTERVAL. Default is 1m.
	duration, err := time.ParseDuration(os.Getenv("SAMPLE_INTERVAL"))
	if err != nil {
		slog.Warn("invalid SAMPLE_INTERVAL, defaulting to 1m", "err", err)
		duration = 1 * time.Minute
	}

	// Create resource.
	res, err := newResource()
	if err != nil {
		panic(err)
	}

	// Create a meter provider.
	// You can pass this instance directly to your instrumented code if it
	// accepts a MeterProvider instance.
	meterProvider, err := newMeterProvider(res, duration)
	if err != nil {
		panic(err)
	}

	// Handle shutdown properly so nothing leaks.
	defer func() {
		if err := meterProvider.Shutdown(context.Background()); err != nil {
			slog.Error("meter provider shutdown failed", "err", err)
		}
	}()

	// Register as global meter provider so that it can be used via otel.Meter
	// and accessed using otel.GetMeterProvider.
	// Most instrumentation libraries use the global meter provider as default.
	// If the global meter provider is not set then a no-op implementation
	// is used, which fails to generate data.
	otel.SetMeterProvider(meterProvider)
	meter := otel.Meter("procmon/sysinfo")
	cpu, err := meter.Float64ObservableGauge("process.cpu.utilization", metricapi.WithDescription("CPU utilization of the process, in the range [0, 1]"), metricapi.WithUnit("1"))
	if err != nil {
		panic(err)
	}
	mem, err := meter.Float64ObservableGauge("process.memory.utilization", metricapi.WithDescription("Memory utilization of the process, in the range [0, 1]"), metricapi.WithUnit("1"))
	if err != nil {
		panic(err)
	}
	rss, err := meter.Int64ObservableGauge("process.memory.rss", metricapi.WithDescription("Resident set size of the process in bytes"), metricapi.WithUnit("By"))
	if err != nil {
		panic(err)
	}

	azureEnv, instanceId := getAzureEnvironmentInfo()
	slog.Info("Azure environment detected", "env", azureEnv, "instance_id", instanceId)

	sampler := NewSampler()
	meter.RegisterCallback(
		func(ctx context.Context, o metricapi.Observer) error {
			statsList, err := sampler.SampleProcs()
			if err != nil {
				slog.Error("sampling processes failed", "err", err)
				return err
			}
			for _, s := range statsList {
				attrList := []attribute.KeyValue{
					attribute.String("instance_id", instanceId),
					attribute.Int("pid", s.pid),
					attribute.String("name", s.name),
				}
				attrs := metricapi.WithAttributes(attrList...)

				o.ObserveFloat64(cpu, s.cpuUtilization, attrs)
				o.ObserveFloat64(mem, s.memUtilization, attrs)
				o.ObserveInt64(rss, int64(s.rssBytes), attrs)

				slog.Debug("sampled process",
					"pid", s.pid,
					"name", s.name,
					"cpu_util", s.cpuUtilization,
					"mem_util", s.memUtilization,
					"rss_bytes", s.rssBytes,
				)
			}
			return nil
		}, cpu, mem, rss)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	<-ctx.Done()
}

// parseLevel maps a LOG_LEVEL string to a slog.Level, defaulting to Info.
func parseLevel(s string) slog.Level {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "debug":
		return slog.LevelDebug
	case "warn", "warning":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}

func newResource() (*resource.Resource, error) {
	return resource.Merge(
		resource.Default(),
		resource.NewWithAttributes(
			semconv.SchemaURL,
			semconv.ServiceName("procmon"),
			semconv.ServiceVersion("0.1.0"),
		),
	)
}

func newMeterProvider(res *resource.Resource, duration time.Duration) (*metric.MeterProvider, error) {
	var metricExporter metric.Exporter
	metricExporter, err := otlpmetricgrpc.New(context.Background())
	if err != nil {
		slog.Warn("creating OTLP metric exporter failed, falling back to stdout", "err", err)
		metricExporter, err = stdoutmetric.New()
		if err != nil {
			return nil, err
		}
	}

	meterProvider := metric.NewMeterProvider(
		metric.WithResource(res),
		metric.WithReader(metric.NewPeriodicReader(metricExporter,
			metric.WithInterval(duration))),
	)

	return meterProvider, nil
}
