package doger

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type lifecycleTestService struct {
	Name string
}

func (s *lifecycleTestService) HealthCheck() error {
	return nil
}

func (s *lifecycleTestService) Shutdown() error {
	return nil
}

func lifecycleTestServiceProvider(i Injector) (*lifecycleTestService, error) {
	return &lifecycleTestService{Name: "lifecycle-test-service"}, nil
}

func ExampleHealthCheck() {
	injector := New()

	Provide(injector, lifecycleTestServiceProvider)
	_, _ = Invoke[*lifecycleTestService](injector)

	err := HealthCheck[*lifecycleTestService](injector)
	fmt.Println(err)
	// Output: <nil>
}

func ExampleHealthCheckWithContext() {
	injector := New()

	Provide(injector, lifecycleTestServiceProvider)
	_, _ = Invoke[*lifecycleTestService](injector)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err := HealthCheckWithContext[*lifecycleTestService](ctx, injector)
	fmt.Println(err)
	// Output: <nil>
}

func ExampleHealthCheckNamed() {
	injector := New()

	ProvideNamed(injector, "main-database", lifecycleTestServiceProvider)
	_, _ = InvokeNamed[*lifecycleTestService](injector, "main-database")

	err := HealthCheckNamed(injector, "main-database")
	fmt.Println(err)
	// Output: <nil>
}

func ExampleHealthCheckNamedWithContext() {
	injector := New()

	ProvideNamed(injector, "main-database", lifecycleTestServiceProvider)
	_, _ = InvokeNamed[*lifecycleTestService](injector, "main-database")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err := HealthCheckNamedWithContext(ctx, injector, "main-database")
	fmt.Println(err)
	// Output: <nil>
}

func ExampleShutdown() {
	injector := New()

	Provide(injector, lifecycleTestServiceProvider)
	_, _ = Invoke[*lifecycleTestService](injector)

	err := Shutdown[*lifecycleTestService](injector)
	fmt.Println(err)
	// Output: <nil>
}

func ExampleShutdownWithContext() {
	injector := New()

	Provide(injector, lifecycleTestServiceProvider)
	_, _ = Invoke[*lifecycleTestService](injector)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err := ShutdownWithContext[*lifecycleTestService](ctx, injector)
	fmt.Println(err)
	// Output: <nil>
}

func ExampleShutdownNamed() {
	injector := New()

	ProvideNamed(injector, "main-database", lifecycleTestServiceProvider)
	_, _ = InvokeNamed[*lifecycleTestService](injector, "main-database")

	err := ShutdownNamed(injector, "main-database")
	fmt.Println(err)
	// Output: <nil>
}

func ExampleShutdownNamedWithContext() {
	injector := New()

	ProvideNamed(injector, "main-database", lifecycleTestServiceProvider)
	_, _ = InvokeNamed[*lifecycleTestService](injector, "main-database")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err := ShutdownNamedWithContext(ctx, injector, "main-database")
	fmt.Println(err)
	// Output: <nil>
}

func ExampleMustShutdown() {
	injector := New()

	Provide(injector, lifecycleTestServiceProvider)
	_, _ = Invoke[*lifecycleTestService](injector)

	// This will panic if shutdown fails
	MustShutdown[*lifecycleTestService](injector)
	fmt.Println("shutdown completed")
	// Output: shutdown completed
}

func ExampleMustShutdownWithContext() {
	injector := New()

	Provide(injector, lifecycleTestServiceProvider)
	_, _ = Invoke[*lifecycleTestService](injector)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// This will panic if shutdown fails
	MustShutdownWithContext[*lifecycleTestService](ctx, injector)
	fmt.Println("shutdown completed")
	// Output: shutdown completed
}

func ExampleMustShutdownNamed() {
	injector := New()

	ProvideNamed(injector, "main-database", lifecycleTestServiceProvider)
	_, _ = InvokeNamed[*lifecycleTestService](injector, "main-database")

	// This will panic if shutdown fails
	MustShutdownNamed(injector, "main-database")
	fmt.Println("shutdown completed")
	// Output: shutdown completed
}

func ExampleMustShutdownNamedWithContext() {
	injector := New()

	ProvideNamed(injector, "main-database", lifecycleTestServiceProvider)
	_, _ = InvokeNamed[*lifecycleTestService](injector, "main-database")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// This will panic if shutdown fails
	MustShutdownNamedWithContext(ctx, injector, "main-database")
	fmt.Println("shutdown completed")
	// Output: shutdown completed
}

// exampleServer is a Starter with the right shape for a server: Start binds
// the listener, so that a bind error fails Start, serves in a goroutine and
// returns at once; Shutdown stops serving. A Start that called
// ListenAndServe instead would block until Shutdown, and so would Start.
type exampleServer struct {
	addr    string
	srv     *http.Server
	ln      net.Listener
	serving atomic.Bool   // a Serve loop was launched
	served  chan struct{} // closed when the Serve loop ended
}

// newExampleServer returns a server for handler that listens on addr.
func newExampleServer(addr string, handler http.Handler) *exampleServer {
	return &exampleServer{
		addr:   addr,
		srv:    &http.Server{Handler: handler, ReadHeaderTimeout: time.Second},
		served: make(chan struct{}), // made here, so Shutdown never waits on a nil channel
	}
}

func (s *exampleServer) Start(ctx context.Context) error {
	ln, err := new(net.ListenConfig).Listen(ctx, "tcp", s.addr)
	if err != nil {
		return err // the container's Start fails, naming this service
	}
	s.ln = ln
	s.serving.Store(true)
	go func() {
		defer close(s.served)
		_ = s.srv.Serve(ln) // http.ErrServerClosed once Shutdown is called
	}()

	return nil // listening: Start returns
}

func (s *exampleServer) Shutdown(ctx context.Context) error {
	err := s.srv.Shutdown(ctx)
	if !s.serving.Load() {
		// Start failed or never ran: there is no Serve loop to wait for. A
		// Serve launched after this point returns at once, since srv is
		// shut down.
		return err
	}
	select {
	case <-s.served: // the Serve loop ended
	case <-ctx.Done(): // it outlived ctx
	}

	return err
}

// A server's Start must return once it is listening: serve in a goroutine
// and stop in Shutdown.
func Example_starterHTTPServer() {
	root := New()
	ProvideNamed(root, "http", func(Injector) (*exampleServer, error) {
		mux := http.NewServeMux()
		mux.HandleFunc("/", func(w http.ResponseWriter, _ *http.Request) { fmt.Fprint(w, "hello") })
		return newExampleServer("127.0.0.1:0", mux), nil
	}, AsStarter())

	if err := root.Start(); err != nil { // returns: the server is listening
		fmt.Println(err)
		return
	}

	server := MustInvokeNamed[*exampleServer](root, "http")
	client := &http.Client{Transport: &http.Transport{DisableKeepAlives: true}}
	req, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, "http://"+server.ln.Addr().String(), nil)
	resp, err := client.Do(req)
	if err != nil {
		fmt.Println(err)
		return
	}
	body, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	fmt.Println(string(body))

	root.Shutdown()

	// Output:
	// hello
}

// TestExampleServer_ShutdownWithoutStart pins the example server's Shutdown
// when no Serve loop runs, after no Start or a failed bind: it returns
// whatever its context, including one that never ends, instead of hanging.
func TestExampleServer_ShutdownWithoutStart(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		start      bool   // Start is called first
		addr       string // the address Start binds
		background bool   // Shutdown gets context.Background(), which never ends
	}{
		{name: "no_start", addr: "127.0.0.1:0"},
		{name: "failed_bind", start: true, addr: "127.0.0.1:-1"},
		{name: "no_start_background", addr: "127.0.0.1:0", background: true},
		{name: "failed_bind_background", start: true, addr: "127.0.0.1:-1", background: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			testWithTimeout(t, time.Second)

			server := newExampleServer(tt.addr, http.NewServeMux())
			if tt.start {
				require.Error(t, server.Start(context.Background()), "the bind fails")
			}

			ctx := context.Background()
			if !tt.background {
				var cancel context.CancelFunc
				ctx, cancel = context.WithTimeout(ctx, 100*time.Millisecond)
				defer cancel()
			}
			require.NoError(t, server.Shutdown(ctx)) // returns: the test does not time out
		})
	}
}

// TestExampleServer_ContainerShutdownAfterFailedBind pins the example
// server inside a container: root.Shutdown(), whose context never ends,
// returns after root.Start() failed on the bind.
func TestExampleServer_ContainerShutdownAfterFailedBind(t *testing.T) {
	t.Parallel()
	testWithTimeout(t, time.Second)

	root := New()
	ProvideNamed(root, "http", func(Injector) (*exampleServer, error) {
		return newExampleServer("127.0.0.1:-1", http.NewServeMux()), nil
	}, AsStarter())
	require.Error(t, root.Start(), "the bind fails")

	report := root.Shutdown() // returns: the test does not time out
	require.True(t, report.Succeed, "report: %v", report)
}
