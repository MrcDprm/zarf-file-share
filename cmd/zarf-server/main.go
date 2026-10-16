// Command zarf-server runs a Zarf server: it stores envelopes it cannot read and hands them out.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/MrcDprm/zarf-file-share/internal/server"
	"github.com/MrcDprm/zarf-file-share/web"
)

var version = "dev"

const usage = `zarf-server: the Zarf server

Usage:
  zarf-server [serve] [-addr ADDR] [-data DIR] [-trust-proxy]
  zarf-server reports [-data DIR]       list abuse reports
  zarf-server remove [-data DIR] ID     remove a file now
  zarf-server version

Every option can also come from the environment: ZARF_ADDR, ZARF_DATA, ZARF_TRUST_PROXY=1.
`

const cleanupEvery = 10 * time.Minute

func main() {
	args := os.Args[1:]
	cmd := "serve"
	if len(args) > 0 && args[0] != "" && args[0][0] != '-' {
		cmd, args = args[0], args[1:]
	}
	fs := flag.NewFlagSet(cmd, flag.ExitOnError)
	fs.Usage = func() { fmt.Fprint(os.Stderr, usage) }
	addr := fs.String("addr", env("ZARF_ADDR", "127.0.0.1:8080"), "")
	data := fs.String("data", env("ZARF_DATA", "data"), "")
	trustProxy := fs.Bool("trust-proxy", os.Getenv("ZARF_TRUST_PROXY") == "1", "")
	_ = fs.Parse(args)

	if cmd == "version" {
		fmt.Println("zarf-server", version)
		return
	}
	cfg := server.DefaultConfig(*data)
	cfg.TrustProxy = *trustProxy
	cfg.Web = web.Files()
	s, err := server.New(cfg)
	if err != nil {
		log.Fatalf("zarf-server: %v", err)
	}
	defer s.Close()

	switch cmd {
	case "serve":
		err = serve(s, *addr)
	case "reports":
		err = listReports(s)
	case "remove":
		if fs.NArg() != 1 {
			fmt.Fprint(os.Stderr, usage)
			os.Exit(2)
		}
		err = s.Remove(context.Background(), fs.Arg(0))
	default:
		fmt.Fprint(os.Stderr, usage)
		os.Exit(2)
	}
	if err != nil {
		log.Fatalf("zarf-server: %v", err)
	}
}

func env(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func serve(s *server.Server, addr string) error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	go func() {
		t := time.NewTicker(cleanupEvery)
		defer t.Stop()
		for {
			if n, err := s.Cleanup(ctx); err != nil {
				log.Printf("zarf-server: cleanup: %v", err)
			} else if n > 0 {
				log.Printf("zarf-server: removed %d finished files", n)
			}
			select {
			case <-ctx.Done():
				return
			case <-t.C:
			}
		}
	}()

	hs := &http.Server{
		Addr:              addr,
		Handler:           s,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       5 * time.Minute, // one 8 MB part even on a slow line
		IdleTimeout:       2 * time.Minute,
		MaxHeaderBytes:    16 << 10,
		// No WriteTimeout: a 1024 MB download on a slow connection takes a long time.
	}
	errc := make(chan error, 1)
	go func() { errc <- hs.ListenAndServe() }()
	log.Printf("zarf-server %s listening on %s", version, addr)
	if !s.HasWeb() {
		log.Print("zarf-server: the web interface is not built (cd web && npm run build); serving the API only")
	}

	select {
	case err := <-errc:
		return err
	case <-ctx.Done():
	}
	log.Print("zarf-server: shutting down")
	shutdown, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := hs.Shutdown(shutdown); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

func listReports(s *server.Server) error {
	reports, err := s.Reports(context.Background())
	if err != nil {
		return err
	}
	if len(reports) == 0 {
		fmt.Println("No reports.")
	}
	for _, r := range reports {
		fmt.Printf("%s  %s  %s\n", r.CreatedAt.UTC().Format(time.RFC3339), r.FileID, r.Reason)
	}
	return nil
}
