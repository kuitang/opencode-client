package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"opencode-chat/internal/sandbox"
	"opencode-chat/internal/server"
)

func main() {
	port := flag.Int("port", 8080, "Port to serve HTTP")
	flag.Parse()

	log.Printf("Starting OpenCode Chat on port %d", *port)

	srv, err := server.NewServer()
	if err != nil {
		log.Fatalf("Failed to create server: %v", err)
	}
	log.Printf("Templates loaded successfully")

	log.Printf("Initializing sandbox...")

	authConfig, err := sandbox.LoadAuthConfig()
	if err != nil {
		log.Fatalf("Failed to load auth config: %v", err)
	}

	srv.Sandbox = sandbox.NewLocalDockerSandbox()

	if err := srv.Sandbox.Start(authConfig); err != nil {
		log.Fatalf("Failed to start sandbox: %v", err)
	}

	defer func() {
		log.Println("Defer: Cleaning up sandbox")
		if err := srv.Sandbox.Stop(); err != nil {
			log.Printf("Error stopping sandbox: %v", err)
		}
	}()

	log.Printf("Sandbox ready at %s", srv.Sandbox.OpencodeURL())

	log.Printf("Initializing workspace session...")
	if err := srv.InitWorkspaceSession(); err != nil {
		log.Fatalf("Failed to initialize workspace session: %v", err)
	}

	log.Printf("Loading providers from sandbox...")
	if err := srv.LoadProviders(); err != nil {
		log.Fatalf("Failed to load providers: %v", err)
	}

	mux := srv.RegisterRoutes()
	handler := srv.WrapWithMiddleware(mux)

	httpServer := &http.Server{
		Addr:    fmt.Sprintf(":%d", *port),
		Handler: handler,
	}

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

	go func() {
		log.Printf("Starting server on port %d (opencode at %s)\n", *port, srv.Sandbox.OpencodeURL())
		if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("Server failed: %v", err)
		}
	}()

	sig := <-sigChan
	log.Printf("\nReceived signal %v, shutting down gracefully...", sig)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := httpServer.Shutdown(ctx); err != nil {
		log.Printf("HTTP server shutdown error: %v", err)
	}

	log.Printf("Cleaning up sandbox...")
	if err := srv.Sandbox.Stop(); err != nil {
		log.Printf("Error during explicit sandbox cleanup: %v", err)
	}

	log.Printf("Shutdown complete")
}
