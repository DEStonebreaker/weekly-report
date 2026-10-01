package google

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"sync"
	"time"
)

// receiveAuthorizationCode starts the listener before publishing its redirect URL.
func receiveAuthorizationCode(ctx context.Context, state string, authorize func(string) error, returnInstruction string) (string, error) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return "", fmt.Errorf("start OAuth callback listener: %w", err)
	}
	type result struct {
		code string
		err  error
	}
	results := make(chan result, 1)
	var once sync.Once
	mux := http.NewServeMux()
	mux.HandleFunc("/callback", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.Header().Set("Allow", http.MethodGet)
			http.Error(w, "Use GET for the authorization callback.", http.StatusMethodNotAllowed)
			return
		}
		query := r.URL.Query()
		if query.Get("state") != state {
			http.Error(w, "Invalid authorization state.", http.StatusBadRequest)
			return
		}
		callback := result{code: query.Get("code")}
		if oauthError := query.Get("error"); oauthError != "" {
			callback.err = fmt.Errorf("Google authorization failed: %s", oauthError)
		} else if callback.code == "" {
			http.Error(w, "Missing authorization code.", http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		fmt.Fprintf(w, "Authorization callback received. You can %s.\n", returnInstruction)
		once.Do(func() { results <- callback })
	})
	server := &http.Server{Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	defer func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		if err := server.Shutdown(shutdownCtx); err != nil {
			server.Close()
		}
	}()
	serverErrors := make(chan error, 1)
	go func() { serverErrors <- server.Serve(listener) }()
	if err := authorize("http://" + listener.Addr().String() + "/callback"); err != nil {
		return "", fmt.Errorf("open Google authorization in browser: %w", err)
	}
	select {
	case callback := <-results:
		return callback.code, callback.err
	case err := <-serverErrors:
		return "", fmt.Errorf("serve OAuth callback: %w", err)
	case <-ctx.Done():
		return "", fmt.Errorf("wait for Google authorization: %w", ctx.Err())
	}
}
