package main

import (
	"context"
	"crypto/tls"
	"flag"
	"fmt"
	stdlog "log"
	"net/http"
	"os"
	"time"

	"github.com/gorilla/mux"

	log "github.com/go-kit/kit/log"
	admissioncontrol "github.com/tonyo/admission-control"
)

type conf struct {
	TLSCertPath string
	TLSKeyPath  string
	HTTPOnly    bool
	Port        string
	Host        string
}

func main() {
	ctx := context.Background()

	// Get config
	conf := &conf{}
	flag.StringVar(&conf.TLSCertPath, "cert-path", "./cert.crt", "The path to the PEM-encoded TLS certificate")
	flag.StringVar(&conf.TLSKeyPath, "key-path", "./key.key", "The path to the unencrypted TLS key")
	flag.BoolVar(&conf.HTTPOnly, "http-only", false, "Only listen on unencrypted HTTP (e.g. for proxied environments)")
	flag.StringVar(&conf.Port, "port", "8443", "The port to listen on (HTTPS).")
	flag.StringVar(&conf.Host, "host", "admissiond.questionable.services", "The hostname for the service")
	flag.Parse()

	// Set up logging
	var logger log.Logger
	logger = log.NewLogfmtLogger(log.NewSyncWriter(os.Stderr))
	stdlog.SetOutput(log.NewStdlibAdapter(logger))
	logger = log.With(logger, "ts", log.DefaultTimestampUTC, "loc", log.DefaultCaller)

	// TLS configuration
	// Only load the TLS keypair if the -http-only flag is not set.
	var tlsConf *tls.Config
	if !conf.HTTPOnly {
		keyPair, err := tls.LoadX509KeyPair(conf.TLSCertPath, conf.TLSKeyPath)
		if err != nil {
			fatal(logger, err)
		}
		tlsConf = &tls.Config{
			Certificates: []tls.Certificate{keyPair},
			ServerName:   conf.Host,
		}
	}

	// Set up the routes & logging middleware.
	r := mux.NewRouter().StrictSlash(true)
	// Show all available routes
	msg := "Admission Control example server. See the docs at https://github.com/elithrar/admission-control 🎟"
	r.Handle("/", printAvailableRoutes(r, logger, msg)).Methods(http.MethodGet)
	// Default health-check endpoint
	r.HandleFunc("/healthz", healthCheckHandler).Methods(http.MethodGet)

	// Example admission handler endpoints
	admissions := r.PathPrefix("/admission-control").Subrouter()
	admissions.Handle("/deny-ingresses", &admissioncontrol.AdmissionHandler{
		AdmitFunc: admissioncontrol.DenyIngresses(nil),
		Logger:    logger,
	}).Methods(http.MethodPost)
	admissions.Handle("/deny-public-services/gcp", &admissioncontrol.AdmissionHandler{
		// nil = don't whitelist any namespace.
		AdmitFunc: admissioncontrol.DenyPublicLoadBalancers(nil, admissioncontrol.GCP),
		Logger:    logger,
	}).Methods(http.MethodPost)
	admissions.Handle("/deny-public-services/azure", &admissioncontrol.AdmissionHandler{
		AdmitFunc: admissioncontrol.DenyPublicLoadBalancers(nil, admissioncontrol.Azure),
		Logger:    logger,
	}).Methods(http.MethodPost)
	admissions.Handle("/deny-public-services/aws", &admissioncontrol.AdmissionHandler{
		AdmitFunc: admissioncontrol.DenyPublicLoadBalancers(nil, admissioncontrol.AWS),
		Logger:    logger,
	}).Methods(http.MethodPost)
	admissions.Handle("/enforce-pod-annotations", &admissioncontrol.AdmissionHandler{
		AdmitFunc: admissioncontrol.EnforcePodAnnotations(
			[]string{"kube-system"},
			map[string]func(string) bool{
				"k8s.questionable.services/hostname": func(string) bool { return true },
			}),
		Logger: logger,
	}).Methods(http.MethodPost)

	// HTTP server
	timeout := time.Second * 15
	srv := &http.Server{
		Handler:           admissioncontrol.LoggingMiddleware(logger)(r),
		TLSConfig:         tlsConf,
		Addr:              ":" + conf.Port,
		IdleTimeout:       timeout,
		ReadTimeout:       timeout,
		ReadHeaderTimeout: timeout,
		WriteTimeout:      timeout,
	}

	admissionServer, err := admissioncontrol.NewServer(
		srv,
		log.With(logger, "component", "server"),
	)
	if err != nil {
		fatal(logger, err)
		return
	}

	if err := admissionServer.Run(ctx); err != nil {
		fatal(logger, err)
		return
	}
}

func fatal(logger log.Logger, err error) {
	logger.Log(
		"status", "fatal",
		"err", err,
	)

	os.Exit(1)
	return
}

// healthCheckHandler returns a HTTP 200, everytime.
func healthCheckHandler(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
}

// printAvailableRoutes prints all routes attached to the provided Router, and
// prepends a message to the response.
func printAvailableRoutes(router *mux.Router, logger log.Logger, msg string) http.Handler {
	fn := func(w http.ResponseWriter, req *http.Request) {
		var routes []string
		err := router.Walk(func(route *mux.Route, router *mux.Router, ancestors []*mux.Route) error {
			path, err := route.GetPathTemplate()
			if err != nil {
				w.WriteHeader(http.StatusInternalServerError)
				logger.Log("msg", "walkFunc failed", err, err.Error())
				return err
			}

			routes = append(routes, path)
			return nil
		})
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			logger.Log("msg", "walkFunc failed", err, err.Error())
			return
		}

		fmt.Fprintln(w, msg)
		fmt.Fprintln(w, "Available routes:")
		for _, path := range routes {
			fmt.Fprintln(w, path)
		}
	}

	return http.HandlerFunc(fn)
}
