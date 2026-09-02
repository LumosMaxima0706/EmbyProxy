package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"embyproxy/internal/admin"
	"embyproxy/internal/auth"
	"embyproxy/internal/buildinfo"
	"embyproxy/internal/capture"
	"embyproxy/internal/config"
	"embyproxy/internal/failover"
	"embyproxy/internal/identity"
	"embyproxy/internal/logging"
	"embyproxy/internal/mediaproxy"
	"embyproxy/internal/proxy"
	"embyproxy/internal/proxyadapter"
	"embyproxy/internal/requestlog"
	"embyproxy/internal/scheduler"
	"embyproxy/internal/statslog"
	"embyproxy/internal/storage"
	"embyproxy/internal/telegram"
)

func main() {
	if shouldPrintVersion(os.Args[1:]) {
		fmt.Println(buildinfo.String())
		return
	}

	cfg, err := config.Load()
	if err != nil {
		panic(err)
	}
	defaultSystemCfg := storage.DefaultSystemConfig()
	log := logging.New(defaultSystemCfg.LogLevel, defaultSystemCfg.LogAccess)
	defer log.Close()
	if err := log.EnableHistory(filepath.Join(cfg.CWD, "data", "console-logs.jsonl"), logging.DefaultHistoryEntriesFile, logging.DefaultHistoryRotatedFiles); err != nil {
		log.Warn("startup", "console log history disabled", map[string]any{"event": "consoleLogHistoryDisabled", "error": err.Error()})
	}
	logBuildInfo(log)
	if cfg.Admin2FADisabled {
		log.Warn("security", "administrator 2FA emergency mode enabled", map[string]any{"event": "admin2FAEmergencyModeEnabled"})
	}
	if errText := auth.ValidateAdminToken(cfg.AdminToken); errText != "" {
		log.Error("startup", "admin token config invalid", map[string]any{"event": "adminTokenConfigInvalid", "error": errText})
		os.Exit(1)
	}
	store, err := storage.New(cfg.DBPath)
	if err != nil {
		log.Error("startup", "database init failed", map[string]any{"event": "databaseInitFailed", "error": err.Error()})
		os.Exit(1)
	}
	defer store.Close()
	globalStats, err := statslog.Open(cfg.GlobalStatsDBPath)
	if err != nil {
		log.Error("startup", "global stats database init failed", map[string]any{"event": "globalStatsDatabaseInitFailed"})
		os.Exit(1)
	}
	defer globalStats.Close()
	applyRuntimeConfig(context.Background(), store, log)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	ids := identity.NewManager(store)
	if err := ids.Init(ctx); err != nil {
		log.Error("startup", "identity init failed", map[string]any{"event": "identityInitFailed", "error": err.Error()})
		os.Exit(1)
	}

	tg := telegram.New(store, log)
	checker := auth.NewChecker(cfg, store)
	proxyHandler := proxy.New(cfg, store, ids, log)
	adminHandler := admin.New(cfg, store, checker, tg, log, proxyHandler.ResetNodeRoutingState, proxyHandler)
	if cfg.PublicationAgentSocket != "" {
		publicationSyncer, syncerErr := admin.NewSocketPublicationSyncer(cfg.PublicationAgentSocket, 45*time.Second)
		if syncerErr != nil {
			log.Error("startup", "publication adapter config invalid", map[string]any{"event": "publicationAdapterConfigInvalid"})
			os.Exit(1)
		}
		adminHandler.SetPublicationSyncer(publicationSyncer)
	}
	adminHandler.SetGlobalStatsStore(globalStats)
	failoverNodes, err := isolatedFailoverFixtureNodes(cfg)
	if err != nil {
		log.Error("startup", "isolated failover fixture config invalid", map[string]any{"event": "failoverMockFixtureConfigInvalid"})
		os.Exit(1)
	}
	dnsAllowlist, err := failover.ParseDNSRecordAllowlist(cfg.FailoverDNSAllowedRecords)
	if err != nil {
		log.Error("startup", "failover DNS guard config invalid", map[string]any{"event": "failoverDNSGuardConfigInvalid"})
		os.Exit(1)
	}
	failoverController := failover.NewController(failoverNodes, failover.DefaultPolicyConfig(), failover.NewMockDNSProvider())
	failoverController.ConfigureDNSGuard(failover.DNSGuardConfig{
		ProviderMode:          failover.DNSProviderMode(cfg.FailoverDNSProviderMode),
		AllowRealProvider:     cfg.FailoverDNSRealApply,
		RollbackMetadataReady: false,
		Allowlist:             dnsAllowlist,
		DryRunTTL:             5 * time.Minute,
	})
	failoverController.SetEventWriter(func(event failover.Event) error {
		return store.AppendRedactedFailoverEvent(context.Background(), event.CreatedAt.Unix(), event.EventType, event.FromNodeID, event.ToNodeID, string(event.Mode), event.ReasonCode, event.Success)
	})
	failoverController.SetDNSRunWriter(func(change failover.DNSChange, success bool) error {
		propagation := "verified"
		if change.DryRun {
			propagation = "dry_run"
		} else if !success {
			propagation = "failed"
		}
		now := time.Now().Unix()
		return store.RecordDNSUpdateRunRecord(context.Background(), storage.DNSUpdateRunRecord{
			StartedAt: now, CompletedAt: now, ProviderKind: string(change.ProviderMode),
			RecordName: change.Name, RecordType: change.Type,
			PreviousValue: change.PreviousValue, DesiredValue: change.Value,
			RollbackReady: change.PreviousValueKnown, DryRun: change.DryRun,
			ProviderResult: "mock", PropagationResult: propagation, Success: success,
		})
	})
	failoverController.SetDNSPendingWriter(func(change failover.DNSChange) error {
		now := time.Now().Unix()
		return store.RecordDNSUpdateRunRecord(context.Background(), storage.DNSUpdateRunRecord{
			StartedAt: now, CompletedAt: now, ProviderKind: string(change.ProviderMode),
			RecordName: change.Name, RecordType: change.Type,
			PreviousValue: change.PreviousValue, DesiredValue: change.Value,
			RollbackReady:  change.PreviousValueKnown,
			ProviderResult: "pending", PropagationResult: "pending",
		})
	})
	failoverController.SetStateWriter(func(state failover.State) error {
		return store.SaveFailoverState(context.Background(), state.ActiveNodeID, state.DesiredNodeID, state.ObservedDNSNodeID, string(state.Mode), state.CurrentCycleKey, unixOrZero(state.CooldownUntil), unixOrZero(state.LastTransitionAt), unixOrZero(state.LastEvaluationAt), state.ReconciliationRequired)
	})
	failoverController.SetTransitionWriter(func(state failover.State, event failover.Event) error {
		return store.CommitFailoverTransition(context.Background(), failoverStateRecord(state), failoverEventRecord(event))
	})
	failoverController.SetDNSCommitWriter(func(change failover.DNSChange, state failover.State, event failover.Event, success bool) error {
		propagation := "failed"
		if success {
			propagation = "verified"
		}
		return store.CommitDNSUpdate(context.Background(), storage.DNSUpdateRunRecord{
			StartedAt: time.Now().Unix(), CompletedAt: time.Now().Unix(), ProviderKind: string(change.ProviderMode),
			RecordName: change.Name, RecordType: change.Type, DryRun: change.DryRun,
			PreviousValue: change.PreviousValue, DesiredValue: change.Value, RollbackReady: change.PreviousValueKnown,
			ProviderResult: "mock", PropagationResult: propagation, Success: success,
		}, failoverStateRecord(state), failoverEventRecord(event))
	})
	failoverController.SetHealthWriter(func(result failover.HealthResult, node failover.Node) error {
		return store.RecordFailoverHealthCheck(context.Background(), result.NodeID, time.Now().Unix(), result.Kind, result.Success, result.StatusCode, result.Latency.Milliseconds(), node.ConsecutiveFailures, node.ConsecutiveSuccesses, result.ErrorCode)
	})
	failoverController.SetTrafficWriter(func(sample failover.TrafficSample) error {
		var inbound, outbound, total, quota *int64
		var usage *float64
		if sample.Quality == failover.TrafficKnown {
			inbound, outbound, total, quota = &sample.InboundBytes, &sample.OutboundBytes, &sample.TotalBytes, &sample.QuotaBytes
			if value, ok := sample.UsagePercent(); ok {
				usage = &value
			}
		}
		return store.RecordFailoverTrafficSample(context.Background(), storage.TrafficSampleRecord{
			NodeID: sample.NodeID, SampledAt: sample.SampledAt.Unix(), CycleKey: sample.CycleKey,
			SourceType: "controller", InboundBytes: inbound, OutboundBytes: outbound,
			TotalBytes: total, QuotaBytes: quota, UsagePercent: usage, Quality: string(sample.Quality),
		})
	})
	if saved, ok, loadErr := store.LoadFailoverState(context.Background()); loadErr != nil {
		log.Warn("failover", "state restore failed", map[string]any{"event": "failoverStateRestoreFailed"})
	} else if ok {
		failoverController.RestoreState(failover.State{
			ActiveNodeID: saved.ActiveNodeID, DesiredNodeID: saved.DesiredNodeID,
			ObservedDNSNodeID: saved.ObservedDNSNodeID, Mode: failover.Mode(saved.Mode),
			CooldownUntil: unixTimeOrZero(saved.CooldownUntil), LastTransitionAt: unixTimeOrZero(saved.LastTransitionAt),
			LastEvaluationAt: unixTimeOrZero(saved.LastEvaluationAt), CurrentCycleKey: saved.CurrentCycleKey,
			ReconciliationRequired: saved.ReconciliationRequired,
		})
	}
	if savedEvents, loadErr := store.LoadFailoverEvents(context.Background(), 200); loadErr == nil {
		restored := make([]failover.Event, 0, len(savedEvents))
		for index, event := range savedEvents {
			restored = append(restored, failover.Event{ID: int64(index + 1), CreatedAt: time.Unix(event.CreatedAt, 0), EventType: event.EventType, FromNodeID: event.FromNodeID, ToNodeID: event.ToNodeID, Mode: failover.Mode(event.Mode), ReasonCode: event.ReasonCode, Success: event.Success})
		}
		failoverController.RestoreEvents(restored)
	}
	if runtimes, loadErr := store.LoadFailoverNodeRuntime(context.Background()); loadErr == nil {
		for nodeID, runtime := range runtimes {
			traffic := trafficSampleFromRecord(runtime.Traffic)
			failoverController.RestoreNodeRuntime(nodeID, runtime.ConsecutiveFailures, runtime.ConsecutiveSuccesses, traffic)
		}
	}
	adminHandler.SetFailoverController(failoverController)
	adminHandler.SetDNSStatusReader(func() map[string]any {
		run, ok, err := store.LoadLatestDNSUpdateRun(context.Background())
		if err != nil || !ok {
			return map[string]any{"available": false}
		}
		return map[string]any{"available": true, "dry_run": run.DryRun, "success": run.Success, "provider": run.ProviderKind, "propagation": run.PropagationResult, "rollback_ready": run.RollbackReady, "completed_at": run.CompletedAt}
	})

	scheduler.New(log, tg, proxyHandler.CleanupTTLMaps).Start(ctx)

	proxyRoute := proxyRouteHandler(cfg, store, proxyHandler)
	type serverSpec struct {
		server      *http.Server
		logProfiles bool
	}
	var serverSpecs []serverSpec
	if cfg.AdminAddr() == "" {
		mux := http.NewServeMux()
		registerRoutes(mux, adminHandler, proxyRoute)
		serverSpecs = append(serverSpecs, serverSpec{
			server:      &http.Server{Addr: cfg.Addr(), Handler: wrapHTTPHandler(cfg, store, log, mux)},
			logProfiles: true,
		})
	} else {
		proxyMux := http.NewServeMux()
		registerProxyRoutes(proxyMux, proxyRoute)
		adminMux := http.NewServeMux()
		registerAdminRoutes(adminMux, adminHandler)
		serverSpecs = append(serverSpecs,
			serverSpec{server: &http.Server{Addr: cfg.Addr(), Handler: wrapHTTPHandler(cfg, store, log, proxyMux)}, logProfiles: true},
			serverSpec{server: &http.Server{Addr: cfg.AdminAddr(), Handler: wrapHTTPHandler(cfg, store, log, adminMux)}},
		)
	}
	for _, spec := range serverSpecs {
		spec := spec
		go func() {
			listener, err := net.Listen("tcp", spec.server.Addr)
			if err != nil {
				log.Error("startup", "server failed", map[string]any{"event": "serverFailed", "error": err.Error()})
				stop()
				return
			}
			log.Info("startup", "server listening", map[string]any{"event": "serverListening", "addr": spec.server.Addr, "db": cfg.DBPath})
			if spec.logProfiles {
				logIdentityProfiles(log, ids)
			}
			if err := spec.server.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
				log.Error("startup", "server failed", map[string]any{"event": "serverFailed", "error": err.Error()})
				stop()
			}
		}()
	}

	<-ctx.Done()
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	for _, spec := range serverSpecs {
		if err := spec.server.Shutdown(shutdownCtx); err != nil {
			log.Error("shutdown", "server shutdown failed", map[string]any{"event": "serverShutdownFailed", "error": err.Error()})
		}
	}
}

func isolatedFailoverFixtureNodes(cfg config.Config) ([]failover.Node, error) {
	if !cfg.FailoverMockFixture {
		return nil, nil
	}
	mode := failover.DNSProviderMode(cfg.FailoverDNSProviderMode)
	switch mode {
	case failover.DNSProviderModeMock, failover.DNSProviderModeNoop, failover.DNSProviderModeLocalOnly:
	default:
		return nil, errors.New("isolated failover fixture requires an explicit local provider mode")
	}
	if !loopbackListenAddress(cfg.Addr()) || !loopbackListenAddress(cfg.AdminAddr()) {
		return nil, errors.New("isolated failover fixture requires loopback proxy and admin listeners")
	}
	return []failover.Node{
		{ID: "mock-primary", Name: "Mock Primary", Role: failover.RolePrimary, Enabled: true, Priority: 1, HealthStatus: failover.HealthHealthy},
		{ID: "mock-fallback", Name: "Mock Fallback", Role: failover.RoleFallback, Enabled: true, Priority: 2, HealthStatus: failover.HealthHealthy},
	}, nil
}

func loopbackListenAddress(address string) bool {
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		return false
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func unixOrZero(value time.Time) int64 {
	if value.IsZero() {
		return 0
	}
	return value.Unix()
}

func unixTimeOrZero(value int64) time.Time {
	if value == 0 {
		return time.Time{}
	}
	return time.Unix(value, 0)
}

func failoverStateRecord(state failover.State) storage.FailoverStateRecord {
	return storage.FailoverStateRecord{
		ActiveNodeID: state.ActiveNodeID, DesiredNodeID: state.DesiredNodeID, ObservedDNSNodeID: state.ObservedDNSNodeID,
		Mode: string(state.Mode), CooldownUntil: unixOrZero(state.CooldownUntil), LastTransitionAt: unixOrZero(state.LastTransitionAt),
		LastEvaluationAt: unixOrZero(state.LastEvaluationAt), CurrentCycleKey: state.CurrentCycleKey, ReconciliationRequired: state.ReconciliationRequired,
	}
}

func failoverEventRecord(event failover.Event) storage.FailoverEventRecord {
	return storage.FailoverEventRecord{CreatedAt: event.CreatedAt.Unix(), EventType: event.EventType, FromNodeID: event.FromNodeID, ToNodeID: event.ToNodeID, Mode: string(event.Mode), ReasonCode: event.ReasonCode, Success: event.Success}
}

func trafficSampleFromRecord(record storage.TrafficSampleRecord) failover.TrafficSample {
	sample := failover.TrafficSample{NodeID: record.NodeID, CycleKey: record.CycleKey, Quality: failover.TrafficQuality(record.Quality)}
	if record.InboundBytes != nil {
		sample.InboundBytes = *record.InboundBytes
	}
	if record.OutboundBytes != nil {
		sample.OutboundBytes = *record.OutboundBytes
	}
	if record.TotalBytes != nil {
		sample.TotalBytes = *record.TotalBytes
	}
	if record.QuotaBytes != nil {
		sample.QuotaBytes = *record.QuotaBytes
	}
	if record.SampledAt != 0 {
		sample.SampledAt = time.Unix(record.SampledAt, 0)
	}
	return sample
}

func registerRoutes(mux *http.ServeMux, adminHandler http.Handler, proxyHandler http.Handler) {
	registerAdminEndpoints(mux, adminHandler)
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/" {
			capture.SetMeta(r, map[string]any{"mode": "admin", "stage": "admin-redirect"})
			http.Redirect(w, r, "/admin", http.StatusFound)
			return
		}
		proxyHandler.ServeHTTP(w, r)
	})
}

func registerAdminRoutes(mux *http.ServeMux, adminHandler http.Handler) {
	registerAdminEndpoints(mux, adminHandler)
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		capture.SetMeta(r, map[string]any{"mode": "admin", "stage": "admin-redirect"})
		http.Redirect(w, r, "/admin", http.StatusFound)
	})
}

func registerProxyRoutes(mux *http.ServeMux, proxyHandler http.Handler) {
	for _, path := range []string{"/admin", "/admin/", "/api/admin", "/api/admin/", "/favicon.ico"} {
		mux.Handle(path, http.NotFoundHandler())
	}
	mux.Handle("/", proxyHandler)
}

func registerAdminEndpoints(mux *http.ServeMux, adminHandler http.Handler) {
	mux.Handle("/admin", adminHandler)
	mux.Handle("/admin/", adminHandler)
	mux.Handle("/api/admin", adminHandler)
	mux.Handle("/api/admin/", adminHandler)
	mux.Handle("/api/edge/", adminHandler)
	mux.HandleFunc("/favicon.ico", func(w http.ResponseWriter, r *http.Request) {
		capture.SetMeta(r, map[string]any{"mode": "admin", "stage": "favicon"})
		w.WriteHeader(http.StatusNoContent)
	})
}

func wrapHTTPHandler(cfg config.Config, store *storage.Store, log *logging.Logger, next http.Handler) http.Handler {
	handler := capture.New(cfg, store, log).Middleware(next)
	return requestMiddleware(log, store, handler)
}

func proxyRouteHandler(cfg config.Config, store *storage.Store, fallback http.Handler) http.Handler {
	if !cfg.MediaProxyRoutes || store == nil {
		return fallback
	}
	mediaConfig := mediaproxy.Config{TrustProxyEnv: true}
	return proxyadapter.NewProductionRouter(
		proxyadapter.NewStorageResolver(store, "admin"),
		mediaproxy.NewExecutor(mediaConfig),
		mediaConfig,
		fallback,
	)
}

func logBuildInfo(log *logging.Logger) {
	info := buildinfo.Current()
	log.Info("startup", "EmbyProxy starting", map[string]any{"event": "serviceStarting", "version": info.Version, "commit": info.Commit, "builtAt": info.BuiltAt})
}

func shouldPrintVersion(args []string) bool {
	if len(args) != 1 {
		return false
	}
	switch args[0] {
	case "version", "--version", "-v":
		return true
	default:
		return false
	}
}

func logIdentityProfiles(log *logging.Logger, ids *identity.Manager) {
	for _, profile := range identity.ProfileKeys() {
		snap := ids.Snapshot(profile)
		log.Debug("startup", "upstream identity profile", map[string]any{
			"event":     "identityProfileLoaded",
			"profile":   snap.Profile,
			"label":     snap.Label,
			"client":    snap.ClientName,
			"version":   snap.ClientVersion,
			"device":    snap.DeviceName,
			"deviceId":  snap.DeviceID,
			"userAgent": snap.UserAgent,
		})
	}
}

func applyRuntimeConfig(ctx context.Context, store *storage.Store, log *logging.Logger) {
	systemCfg, err := store.GetSystemConfig(ctx, storage.DefaultSystemConfig())
	if err != nil {
		log.Warn("startup", "system config lookup failed", map[string]any{"event": "systemConfigLookupFailed", "error": err.Error()})
		return
	}
	log.Configure(systemCfg.LogLevel, systemCfg.LogAccess)
}

type statusWriter struct {
	http.ResponseWriter
	status int
	bytes  int64
}

func (w *statusWriter) WriteHeader(status int) {
	w.status = status
	w.ResponseWriter.WriteHeader(status)
}

func (w *statusWriter) Write(chunk []byte) (int, error) {
	if w.status == 0 {
		w.status = http.StatusOK
	}
	n, err := w.ResponseWriter.Write(chunk)
	w.bytes += int64(n)
	return n, err
}

func (w *statusWriter) Flush() {
	if f, ok := w.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

func (w *statusWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	h, ok := w.ResponseWriter.(http.Hijacker)
	if !ok {
		return nil, nil, http.ErrNotSupported
	}
	return h.Hijack()
}

func requestMiddleware(log *logging.Logger, store *storage.Store, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := log.NextRequestID("")
		ctx := context.WithValue(r.Context(), "requestID", id)
		ctx = proxy.WithAccessLogFields(ctx)
		ctx = requestlog.WithAccessLogState(ctx)
		trustProxy := trustsProxy(ctx, store)
		clientIP := auth.ClientIP(r, trustProxy)
		sw := &statusWriter{ResponseWriter: w, status: http.StatusOK}
		started := time.Now()
		req := r.WithContext(ctx)
		next.ServeHTTP(sw, req)
		if log.AccessEnabled() && !requestlog.AccessLogSuppressed(ctx) {
			proxy.LogRequestStarted(ctx, log, req, clientIP, "")
			totalMs := time.Since(started).Milliseconds()
			meta := map[string]any{"id": id, "status": sw.status, "bytes": sw.bytes, "totalMs": totalMs, "ip": clientIP}
			for key, value := range proxy.AccessLogFields(ctx) {
				meta[key] = value
			}
			if bodyStarted, ok := proxy.AccessLogResponseBodyStart(ctx); ok {
				bodyMs := time.Since(bodyStarted).Milliseconds()
				if bodyMs < 0 {
					bodyMs = 0
				}
				if bodyMs > totalMs {
					bodyMs = totalMs
				}
				meta["bodyMs"] = bodyMs
			}
			requestURI := logging.RedactURL(r.URL.RequestURI())
			if uri, ok := requestlog.RequestURI(ctx); ok {
				requestURI = uri
			}
			meta["event"] = "requestFinished"
			meta["method"] = r.Method
			meta["uri"] = requestURI
			logRequestFinished(log, sw.status, meta)
		}
	})
}

func logRequestFinished(log *logging.Logger, status int, meta map[string]any) {
	switch {
	case status >= http.StatusInternalServerError:
		log.Error("access", "request finished", meta)
	case status >= http.StatusBadRequest:
		log.Warn("access", "request finished", meta)
	default:
		log.Info("access", "request finished", meta)
	}
}

func trustsProxy(ctx context.Context, store *storage.Store) bool {
	cfg := storage.DefaultSystemConfig()
	if store == nil {
		return cfg.TrustProxy
	}
	saved, err := store.GetSystemConfig(ctx, cfg)
	if err != nil {
		return cfg.TrustProxy
	}
	return saved.TrustProxy
}
