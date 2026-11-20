package doger

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log"
	"log/slog"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// logRecord is a record a logRecorder received.
type logRecord struct {
	level   slog.Level
	message string
	attrs   map[string]string // each value as slog renders it
	keys    []string          // attribute keys, in order
}

// String renders the record as "LEVEL message key=value...".
func (r logRecord) String() string {
	var b strings.Builder
	b.WriteString(r.level.String())
	b.WriteString(" ")
	b.WriteString(r.message)
	for _, key := range r.keys {
		fmt.Fprintf(&b, " %s=%s", key, r.attrs[key])
	}

	return b.String()
}

// logRecorder is a slog.Handler that records every record at or above its
// level. It is safe for concurrent use. onRecord, when set, is called with
// each record before it is kept.
type logRecorder struct {
	level    slog.Level
	onRecord func(logRecord)

	mu      sync.Mutex
	records []logRecord
}

// newLogRecorder returns a recorder of the records at or above level.
func newLogRecorder(level slog.Level) *logRecorder {
	return &logRecorder{level: level}
}

// logger returns a logger writing to r.
func (r *logRecorder) logger() *slog.Logger { return slog.New(r) }

// newRecordingLogger returns a logger whose handler records every record at
// or above level, and the recorder holding them.
func newRecordingLogger(t *testing.T, level slog.Level) (*slog.Logger, *logRecorder) {
	t.Helper()

	recorder := newLogRecorder(level)

	return recorder.logger(), recorder
}

func (r *logRecorder) Enabled(_ context.Context, level slog.Level) bool { return level >= r.level }

func (r *logRecorder) Handle(_ context.Context, record slog.Record) error {
	rec := logRecord{level: record.Level, message: record.Message, attrs: map[string]string{}}
	record.Attrs(func(attr slog.Attr) bool {
		rec.keys = append(rec.keys, attr.Key)
		rec.attrs[attr.Key] = attr.Value.Resolve().String()
		return true
	})
	if r.onRecord != nil {
		r.onRecord(rec)
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	r.records = append(r.records, rec)

	return nil
}

func (r *logRecorder) WithAttrs([]slog.Attr) slog.Handler { return r }
func (r *logRecorder) WithGroup(string) slog.Handler      { return r }

// lineLogger returns a logger at Debug level passing each record, rendered
// by logRecord.String, to record.
func lineLogger(record func(line string)) *slog.Logger {
	recorder := newLogRecorder(slog.LevelDebug)
	recorder.onRecord = func(rec logRecord) { record(rec.String()) }

	return recorder.logger()
}

// hookLogger returns a logger at Debug level calling hook with each record,
// on the goroutine logging it.
func hookLogger(hook func(logRecord)) *slog.Logger {
	recorder := newLogRecorder(slog.LevelDebug)
	recorder.onRecord = hook

	return recorder.logger()
}

// panickingLogger returns a logger at Debug level whose handler panics with
// value on the first record matching match.
func panickingLogger(match func(logRecord) bool, value any) *slog.Logger {
	var panicked atomic.Bool
	recorder := newLogRecorder(slog.LevelDebug)
	recorder.onRecord = func(rec logRecord) {
		if match(rec) && panicked.CompareAndSwap(false, true) {
			panic(value)
		}
	}

	return recorder.logger()
}

// all returns the recorded records.
func (r *logRecorder) all() []logRecord {
	r.mu.Lock()
	defer r.mu.Unlock()

	return append([]logRecord(nil), r.records...)
}

// withMessage returns the recorded records with message.
func (r *logRecorder) withMessage(message string) []logRecord {
	var found []logRecord
	for _, rec := range r.all() {
		if rec.message == message {
			found = append(found, rec)
		}
	}

	return found
}

// containing returns the rendered records containing every one of parts.
func (r *logRecorder) containing(parts ...string) []string {
	var found []string
	for _, rec := range r.all() {
		line := rec.String()
		ok := true
		for _, part := range parts {
			ok = ok && strings.Contains(line, part)
		}
		if ok {
			found = append(found, line)
		}
	}

	return found
}

// loggingSvc is a lazy service with Start, HealthCheck and Shutdown.
type loggingSvc struct {
	healthErr   error
	shutdownErr error
}

func (*loggingSvc) Start(context.Context) error { return nil }
func (s *loggingSvc) HealthCheck() error        { return s.healthErr }
func (s *loggingSvc) Shutdown() error           { return s.shutdownErr }

// provideLoggingSvc registers the lazy "svc" in scope, opted in to Start.
func provideLoggingSvc(scope Injector, svc *loggingSvc) {
	ProvideNamed(scope, "svc", func(Injector) (*loggingSvc, error) { return svc, nil }, AsStarter())
}

// loggingScheduler is a HealthScheduler failing as configured.
type loggingScheduler struct {
	err     error // returned by ScheduleHealthRounds
	stopErr error // returned by the stop function
}

func (s *loggingScheduler) ScheduleHealthRounds(_ Clock, _ func(context.Context)) (func(context.Context) error, error) {
	if s.err != nil {
		return nil, s.err
	}

	return func(context.Context) error { return s.stopErr }, nil
}

// captureDefaultLogs replaces the default slog logger and the output of the
// log package until the end of t, and returns what they receive.
func captureDefaultLogs(t *testing.T) (*logRecorder, *bytes.Buffer) {
	t.Helper()

	// slog.SetDefault redirects the log package and clears its flags, and
	// restoring the default slog logger does not undo that: restore both
	writer, flags, prefix, previous := log.Writer(), log.Flags(), log.Prefix(), slog.Default()
	t.Cleanup(func() {
		slog.SetDefault(previous)
		log.SetOutput(writer)
		log.SetFlags(flags)
		log.SetPrefix(prefix)
	})

	defaults := newLogRecorder(slog.LevelDebug)
	slog.SetDefault(defaults.logger())
	var output bytes.Buffer
	log.SetOutput(&output)

	return defaults, &output
}

// TestLogging_CaptureDefaultLogsRestores checks that captureDefaultLogs
// leaves the log and slog defaults as it found them, so later tests in the
// binary can log.
func TestLogging_CaptureDefaultLogsRestores(t *testing.T) { // not parallel: replaces the default slog logger and log output
	is := assert.New(t)
	writer, flags, prefix, logger := log.Writer(), log.Flags(), log.Prefix(), slog.Default()

	t.Run("capture", func(t *testing.T) { captureDefaultLogs(t) })

	is.Equal(writer, log.Writer(), "log output")
	is.Equal(flags, log.Flags(), "log flags")
	is.Equal(prefix, log.Prefix(), "log prefix")
	is.Same(logger, slog.Default(), "default slog logger")
}

func TestLogging_OffByDefault(t *testing.T) { // not parallel: replaces the default slog logger and log output
	tests := []struct {
		name string
		root func() *RootScope
	}{
		{name: "new", root: func() *RootScope { return New() }},
		{name: "empty_opts", root: func() *RootScope { return NewWithOpts(&InjectorOpts{}) }},
		{name: "nil_opts", root: func() *RootScope { return NewWithOpts(nil) }},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			defaults, output := captureDefaultLogs(t)

			root := tt.root()
			provideLoggingSvc(root, &loggingSvc{healthErr: errors.New("down"), shutdownErr: errors.New("stuck")})
			_, err := InvokeNamed[*loggingSvc](root, "svc")
			require.NoError(t, err)
			require.NoError(t, root.Start())
			_ = root.HealthCheck()
			_ = root.Shutdown()

			assert.Empty(t, defaults.all())
			assert.Empty(t, output.String())
		})
	}
}

func TestLogging_Events(t *testing.T) {
	t.Parallel()

	sourceFailure := &fs.PathError{Op: "open", Path: "/vault", Err: fs.ErrNotExist}
	readProperty := func(t *testing.T, root *RootScope) {
		t.Helper()
		ProvideNamed(root, NameOf[PropertySource](), func(Injector) (PropertySource, error) { return nil, sourceFailure })
		require.NoError(t, Extend(root, &testExtension{name: "redis", extend: func(x *ExtensionContext) error {
			_, _ = x.Properties().Lookup("addr")
			return nil
		}}))
	}
	healthSwitch := func(t *testing.T, root *RootScope, results ...error) {
		t.Helper()
		db := &healthReportTestSwitch{}
		ProvideNamedValue(root, "db", db, AsOptionalHealth())
		for _, result := range results {
			db.set(result)
			_ = root.HealthCheck()
		}
	}

	tests := []struct {
		name    string
		opts    *InjectorOpts
		trigger func(t *testing.T, root *RootScope)
		message string
		level   slog.Level
		attrs   map[string]string
	}{
		{
			name:    "registered",
			trigger: func(_ *testing.T, root *RootScope) { provideLoggingSvc(root, &loggingSvc{}) },
			message: "service registered", level: slog.LevelDebug,
			attrs: map[string]string{"scope": "[root]", "service": "svc"},
		},
		{
			name: "overridden",
			trigger: func(_ *testing.T, root *RootScope) {
				provideLoggingSvc(root, &loggingSvc{})
				OverrideNamed(root, "svc", func(Injector) (*loggingSvc, error) { return &loggingSvc{}, nil })
			},
			message: "service overridden", level: slog.LevelDebug,
			attrs: map[string]string{"scope": "[root]", "service": "svc"},
		},
		{
			name: "invoked",
			trigger: func(t *testing.T, root *RootScope) {
				t.Helper()
				provideLoggingSvc(root, &loggingSvc{})
				_, err := InvokeNamed[*loggingSvc](root, "svc")
				require.NoError(t, err)
			},
			message: "service invoked", level: slog.LevelDebug,
			attrs: map[string]string{"scope": "[root]", "service": "svc"},
		},
		{
			name: "exported",
			trigger: func(t *testing.T, root *RootScope) {
				t.Helper()
				ext := root.Scope("ext")
				provideLoggingSvc(ext, &loggingSvc{})
				require.NoError(t, ExportNamed[*loggingSvc](ext, "svc"))
			},
			message: "service exported", level: slog.LevelDebug,
			attrs: map[string]string{"scope": "ext", "service": "svc"},
		},
		{
			name: "started",
			trigger: func(t *testing.T, root *RootScope) {
				t.Helper()
				provideLoggingSvc(root, &loggingSvc{})
				require.NoError(t, root.Start())
			},
			message: "container started", level: slog.LevelInfo,
			attrs: map[string]string{"scope": "[root]"},
		},
		{
			name: "ready",
			trigger: func(t *testing.T, root *RootScope) {
				t.Helper()
				provideLoggingSvc(root, &loggingSvc{})
				require.NoError(t, root.Start())
			},
			message: "container ready", level: slog.LevelInfo,
			attrs: map[string]string{"scope": "[root]"},
		},
		{
			name:    "requested_shutdown",
			trigger: func(_ *testing.T, root *RootScope) { _ = root.Shutdown() },
			message: "requested shutdown", level: slog.LevelDebug,
			attrs: map[string]string{"scope": "[root]"},
		},
		{
			name:    "closing",
			trigger: func(_ *testing.T, root *RootScope) { _ = root.Shutdown() },
			message: "container closing", level: slog.LevelInfo,
			attrs: map[string]string{"scope": "[root]"},
		},
		{
			name:    "closed",
			trigger: func(_ *testing.T, root *RootScope) { _ = root.Shutdown() },
			message: "container closed", level: slog.LevelInfo,
			attrs: map[string]string{"scope": "[root]"},
		},
		{
			name: "service_shutdown_failed",
			trigger: func(t *testing.T, root *RootScope) {
				t.Helper()
				provideLoggingSvc(root, &loggingSvc{shutdownErr: errors.New("x")})
				_, err := InvokeNamed[*loggingSvc](root, "svc")
				require.NoError(t, err)
				_ = root.Shutdown()
			},
			message: "service shutdown failed", level: slog.LevelError,
			attrs: map[string]string{"scope": "[root]", "service": "svc", "error": "x"},
		},
		{
			name: "health_scheduler_failed",
			opts: &InjectorOpts{HealthScheduler: &loggingScheduler{err: errors.New("no cron")}},
			trigger: func(t *testing.T, root *RootScope) {
				t.Helper()
				require.NoError(t, root.Start())
			},
			message: "health scheduler failed", level: slog.LevelError,
			attrs: map[string]string{"error": "no cron"},
		},
		{
			name: "health_scheduler_stop_failed",
			opts: &InjectorOpts{HealthScheduler: &loggingScheduler{stopErr: errors.New("busy")}},
			trigger: func(t *testing.T, root *RootScope) {
				t.Helper()
				require.NoError(t, root.Start())
				_ = root.Shutdown()
			},
			message: "health scheduler stop failed", level: slog.LevelError,
			attrs: map[string]string{"error": "busy"},
		},
		{
			name:    "property_source_unavailable",
			trigger: readProperty,
			message: "property source unavailable", level: slog.LevelWarn,
			attrs: map[string]string{"source_type": "*fs.PathError"},
		},
		{
			name: "extension_disabled",
			trigger: func(t *testing.T, root *RootScope) {
				t.Helper()
				require.NoError(t, Extend(root, &testExtension{name: "redis", extend: func(*ExtensionContext) error {
					return ErrExtensionDisabled
				}}))
			},
			message: "extension disabled", level: slog.LevelInfo,
			attrs: map[string]string{"extension": "redis"},
		},
		{
			name: "health_check_failed",
			trigger: func(t *testing.T, root *RootScope) {
				t.Helper()
				provideLoggingSvc(root, &loggingSvc{healthErr: errors.New("x")})
				_, err := InvokeNamed[*loggingSvc](root, "svc")
				require.NoError(t, err)
				_ = root.HealthCheck()
			},
			message: "health check failed", level: slog.LevelDebug,
			attrs: map[string]string{"scope": "[root]", "service": "svc", "error": "x"},
		},
		{
			name: "health_degraded",
			trigger: func(t *testing.T, root *RootScope) {
				t.Helper()
				healthSwitch(t, root, nil, errors.New("slow"))
			},
			message: "container health changed", level: slog.LevelWarn,
			attrs: map[string]string{"previous": "healthy", "current": "degraded"},
		},
		{
			name: "health_recovered",
			trigger: func(t *testing.T, root *RootScope) {
				t.Helper()
				healthSwitch(t, root, nil, errors.New("slow"), nil)
			},
			message: "container health changed", level: slog.LevelInfo,
			attrs: map[string]string{"previous": "degraded", "current": "healthy"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			testWithTimeout(t, 5*time.Second)

			logger, recorder := newRecordingLogger(t, slog.LevelDebug)
			opts := &InjectorOpts{}
			if tt.opts != nil {
				opts = tt.opts
			}
			opts.Logger = logger
			root := NewWithOpts(opts)

			tt.trigger(t, root)

			// health changes are logged on the goroutine delivering them
			var matching []logRecord
			assert.Eventually(t, func() bool {
				matching = nil
				for _, rec := range recorder.withMessage(tt.message) {
					if rec.attrs["previous"] == tt.attrs["previous"] && rec.attrs["current"] == tt.attrs["current"] {
						matching = append(matching, rec)
					}
				}
				return len(matching) > 0
			}, time.Second, time.Millisecond)
			require.Len(t, matching, 1, "records: %v", recorder.all())
			assert.Equal(t, tt.level, matching[0].level)
			for key, value := range tt.attrs {
				assert.Equal(t, value, matching[0].attrs[key], "attribute %s", key)
			}
		})
	}
}

func TestLogging_LevelFilter(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		level   slog.Level
		debug   int
		started int
	}{
		{name: "debug", level: slog.LevelDebug, debug: 1, started: 1},
		{name: "info", level: slog.LevelInfo, debug: 0, started: 1},
		{name: "error", level: slog.LevelError, debug: 0, started: 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			logger, recorder := newRecordingLogger(t, tt.level)
			root := NewWithOpts(&InjectorOpts{Logger: logger})
			// not a Starter: Start invokes nothing more
			ProvideNamed(root, "svc", func(Injector) (int, error) { return 1, nil })

			_, err := InvokeNamed[int](root, "svc")
			require.NoError(t, err)
			require.NoError(t, root.Start())
			_ = root.Shutdown()

			assert.Len(t, recorder.withMessage("service invoked"), tt.debug)
			assert.Len(t, recorder.withMessage("container started"), tt.started)
		})
	}
}

func TestLogging_ChildScopeAttr(t *testing.T) {
	t.Parallel()

	logger, recorder := newRecordingLogger(t, slog.LevelDebug)
	root := NewWithOpts(&InjectorOpts{Logger: logger})
	tenant := root.Scope("tenant-a")

	provideLoggingSvc(tenant, &loggingSvc{})
	_, err := InvokeNamed[*loggingSvc](tenant, "svc")
	require.NoError(t, err)

	for _, message := range []string{"service registered", "service invoked"} {
		records := recorder.withMessage(message)
		require.Len(t, records, 1, message)
		assert.Equal(t, "tenant-a", records[0].attrs["scope"], message)
		assert.Equal(t, "svc", records[0].attrs["service"], message)
	}
}

func TestLogging_Concurrent(t *testing.T) {
	t.Parallel()
	testWithTimeout(t, 10*time.Second)

	logger, recorder := newRecordingLogger(t, slog.LevelDebug)
	root := NewWithOpts(&InjectorOpts{Logger: logger})
	for n := range 10 {
		ProvideNamed(root, fmt.Sprintf("svc-%d", n), func(Injector) (*loggingSvc, error) { return &loggingSvc{}, nil })
	}

	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() {
			for n := range 10 {
				for range 100 {
					_, _ = InvokeNamed[*loggingSvc](root, fmt.Sprintf("svc-%d", n))
				}
			}
		})
	}
	wg.Wait()

	assert.Len(t, recorder.withMessage("service invoked"), 8000)
}

func TestLogging_PanicValueDebugOnly(t *testing.T) {
	t.Parallel()

	// closing: the second Closing hook panic of one Shutdown is logged; the
	// first goes to HookContainerPanic. service-shutdown: with no
	// HookContainerPanic, a Shutdown panic is logged.
	tests := []struct {
		name    string
		level   slog.Level
		point   string
		service string // the service attribute of the Error record
		errors  int
		secret  int
	}{
		{name: "closing_debug", level: slog.LevelDebug, point: "closing", errors: 1, secret: 1},
		{name: "closing_info", level: slog.LevelInfo, point: "closing", errors: 1, secret: 0},
		{name: "service_shutdown_debug", level: slog.LevelDebug, point: "service-shutdown", service: "svc", errors: 1, secret: 1},
		{name: "service_shutdown_info", level: slog.LevelInfo, point: "service-shutdown", service: "svc", errors: 1, secret: 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			logger, recorder := newRecordingLogger(t, tt.level)
			opts := &InjectorOpts{Logger: logger}
			if tt.point == "closing" {
				opts.HookContainerPanic = func(*RootScope, string, any) {}
				opts.HookContainerClosing = []func(context.Context, *RootScope){
					func(context.Context, *RootScope) { panic("first") },
					func(context.Context, *RootScope) { panic("token=hunter2") },
				}
			}
			root := NewWithOpts(opts)
			if tt.point == "service-shutdown" {
				provideBuilt(t, root, "svc", &shutdownTestPanicPlain{value: "token=hunter2"})
			}

			_ = root.Shutdown()

			var errorRecords []logRecord
			for _, rec := range recorder.withMessage("panic recovered") {
				if rec.level == slog.LevelError && rec.attrs["point"] == tt.point && rec.attrs["panic_type"] == "string" &&
					(tt.service == "" || rec.attrs["service"] == tt.service) {
					errorRecords = append(errorRecords, rec)
				}
			}
			assert.Len(t, errorRecords, tt.errors, "records: %v", recorder.all())
			assert.Len(t, recorder.containing("hunter2"), tt.secret)
		})
	}
}

func TestLogging_HealthErrorDebugOnly(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		level  slog.Level
		secret int
	}{
		{name: "debug", level: slog.LevelDebug, secret: 1},
		{name: "info", level: slog.LevelInfo, secret: 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			logger, recorder := newRecordingLogger(t, tt.level)
			root := NewWithOpts(&InjectorOpts{Logger: logger})
			ProvideNamed(root, "db", func(Injector) (*loggingSvc, error) {
				return &loggingSvc{healthErr: errors.New("dsn=postgres://u:pw@h")}, nil
			})
			_, err := InvokeNamed[*loggingSvc](root, "db")
			require.NoError(t, err)

			_ = root.HealthCheck()

			assert.Len(t, recorder.containing("pw@h"), tt.secret, "records: %v", recorder.all())
		})
	}
}

// loggingBlockingCheck is a health check that blocks until release is
// closed.
type loggingBlockingCheck struct {
	release chan struct{}
	ran     atomic.Int32
}

func (c *loggingBlockingCheck) HealthCheck() error {
	c.ran.Add(1)
	<-c.release
	return nil
}

func TestLogging_HealthCheckStarted(t *testing.T) {
	t.Parallel()

	// blocking returns a check that blocks until release is called, and
	// that release function.
	blocking := func() (*loggingBlockingCheck, func()) {
		check := &loggingBlockingCheck{release: make(chan struct{})}
		return check, sync.OnceFunc(func() { close(check.release) })
	}

	tests := []struct {
		name string
		// run registers and checks services on a root with logger, and
		// returns the scope and service the records must name, and a
		// service that must have none ("" for none).
		run   func(t *testing.T, logger *slog.Logger) (scope, service, absent string)
		count int
	}{
		{
			name: "HealthCheckNamed",
			run: func(t *testing.T, logger *slog.Logger) (string, string, string) {
				t.Helper()
				root := NewWithOpts(&InjectorOpts{Logger: logger})
				check, release := blocking()
				ProvideNamedValue(root, "db", check)
				done := make(chan error, 1)
				go func() { done <- HealthCheckNamed(root, "db") }()
				synctest.Wait()
				release()
				assert.NoError(t, <-done)
				return "[root]", "db", ""
			},
			count: 1,
		},
		{
			name: "a second caller joins the check in flight",
			run: func(t *testing.T, logger *slog.Logger) (string, string, string) {
				t.Helper()
				root := NewWithOpts(&InjectorOpts{Logger: logger})
				check, release := blocking()
				ProvideNamedValue(root, "db", check)
				done := make(chan error, 2)
				go func() { done <- HealthCheckNamed(root, "db") }()
				synctest.Wait()
				go func() { done <- HealthCheckNamed(root, "db") }()
				synctest.Wait()
				release()
				assert.NoError(t, <-done)
				assert.NoError(t, <-done)
				return "[root]", "db", ""
			},
			count: 1,
		},
		{
			name: "an already-cancelled context",
			run: func(t *testing.T, logger *slog.Logger) (string, string, string) {
				t.Helper()
				root := NewWithOpts(&InjectorOpts{Logger: logger})
				check, release := blocking()
				defer release()
				ProvideNamedValue(root, "db", check)
				ctx, cancel := context.WithCancel(t.Context())
				cancel()
				assert.ErrorIs(t, HealthCheckNamedWithContext(ctx, root, "db"), ErrHealthCheckTimeout)
				return "[root]", "db", ""
			},
			count: 0,
		},
		{
			name: "root.HealthCheck",
			run: func(t *testing.T, logger *slog.Logger) (string, string, string) {
				t.Helper()
				root := NewWithOpts(&InjectorOpts{Logger: logger})
				check, release := blocking()
				ProvideNamedValue(root, "db", check)
				done := make(chan map[string]error, 1)
				go func() { done <- root.HealthCheck() }()
				synctest.Wait()
				release()
				assert.Equal(t, map[string]error{"db": nil}, <-done)
				return "[root]", "db", ""
			},
			count: 1,
		},
		{
			// with one slot, "b" waits behind "a"; the caller's context ends
			// while it waits, so its check never runs
			name: "queued_then_abandoned",
			run: func(t *testing.T, logger *slog.Logger) (string, string, string) {
				t.Helper()
				root := NewWithOpts(&InjectorOpts{Logger: logger, HealthCheckParallelism: 1})
				a, releaseA := blocking()
				b, releaseB := blocking()
				defer releaseB()
				ProvideNamedValue(root, "a", a)
				ProvideNamedValue(root, "b", b)
				ctx, cancel := context.WithCancel(t.Context())
				done := make(chan map[string]error, 1)
				go func() { done <- root.HealthCheckWithContext(ctx) }()
				synctest.Wait()
				cancel()
				synctest.Wait()
				releaseA()
				releaseB()
				<-done
				synctest.Wait()
				// the slot goes to either service first; the other may run
				// later, detached from the caller, or never: a record must
				// exist for exactly the checks that ran
				if a.ran.Load()+b.ran.Load() == 2 {
					return "[root]", "", ""
				}
				require.Equal(t, int32(1), a.ran.Load()+b.ran.Load(), "one check ran")
				if a.ran.Load() == 1 {
					return "[root]", "a", "b"
				}
				return "[root]", "b", "a"
			},
			count: 1,
		},
		{
			name: "scheduled round",
			run: func(t *testing.T, logger *slog.Logger) (string, string, string) {
				t.Helper()
				scheduler := &healthReportExampleScheduler{}
				root := NewWithOpts(&InjectorOpts{Logger: logger, HealthScheduler: scheduler})
				ProvideNamedValue(root, "db", healthReportExampleCheck{})
				require.NoError(t, root.Start())
				scheduler.round(context.Background())
				root.Shutdown()
				return "[root]", "db", ""
			},
			count: 1,
		},
		{
			name: "child scope",
			run: func(t *testing.T, logger *slog.Logger) (string, string, string) {
				t.Helper()
				root := NewWithOpts(&InjectorOpts{Logger: logger})
				tenant := root.Scope("tenant")
				ProvideNamedValue(tenant, "db", healthReportExampleCheck{})
				assert.NoError(t, HealthCheckNamed(tenant, "db"))
				return "tenant", "db", ""
			},
			count: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			synctest.Test(t, func(t *testing.T) {
				logger, recorder := newRecordingLogger(t, slog.LevelDebug)

				scope, service, absent := tt.run(t, logger)
				synctest.Wait()

				records := recorder.withMessage("health check started")
				named := 0
				for _, rec := range records {
					assert.Equal(t, slog.LevelDebug, rec.level)
					if absent != "" {
						assert.NotEqual(t, absent, rec.attrs["service"], "a check that never ran")
					}
					if rec.attrs["scope"] == scope && rec.attrs["service"] == service {
						named++
					}
				}
				if service == "" {
					// every check ran: one record each
					assert.Len(t, records, 2)
					return
				}
				assert.Len(t, records, tt.count)
				assert.Equal(t, tt.count, named, "records naming scope %q and service %q", scope, service)
			})
		})
	}
}

func TestLogging_HealthCheckStartedInfoLevel(t *testing.T) {
	t.Parallel()

	logger, recorder := newRecordingLogger(t, slog.LevelInfo)
	root := NewWithOpts(&InjectorOpts{Logger: logger})
	ProvideNamedValue(root, "db", healthReportExampleCheck{})

	require.NoError(t, HealthCheckNamed(root, "db"))

	assert.Empty(t, recorder.withMessage("health check started"))
}
