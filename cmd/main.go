package main

import (
	"context"
	"crypto/tls"
	"flag"
	"fmt"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zapio"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"log"
	"net/http"
	"os"
	"os/signal"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/config"
	"syscall"
)

var (
	globalClient                 client.Client
	port                         int
	certFile, keyFile, caCrtFile string
)

type teardownFn func()

func setupLogger() teardownFn {
	// Setup logger
	var logger *zap.Logger
	if _, inDebug := os.LookupEnv("LOG_DEVEL"); inDebug {
		logger, _ = zap.NewDevelopment()
	} else {
		loggerConfig := zap.NewProductionConfig()
		loggerConfig.EncoderConfig.TimeKey = "time"
		loggerConfig.EncoderConfig.MessageKey = "message"
		loggerConfig.EncoderConfig.EncodeTime = zapcore.ISO8601TimeEncoder
		loggerConfig.Level = zap.NewAtomicLevel()
		levelFromEnv := os.Getenv("LOG_LEVEL")
		if err := loggerConfig.Level.UnmarshalText([]byte(levelFromEnv)); err != nil {
			panic(fmt.Errorf("could not parse log level %s: %w", levelFromEnv, err))
		}
		logger, _ = loggerConfig.Build()
	}
	zap.ReplaceGlobals(logger)

	// Set Zap as default logger for some internal Go services
	zapWriter := &zapio.Writer{Log: logger.WithOptions(zap.AddStacktrace(zap.InfoLevel)).Named("go"), Level: zap.InfoLevel}
	log.SetOutput(zapWriter)

	teardownFn := func() {
		err := zapWriter.Close()
		if err != nil {
			zap.L().Error("Problem while closing zapio.Writer", zap.Error(err))
		}
	}

	return teardownFn
}

func main() {
	teardownLogger := setupLogger()
	defer teardownLogger()

	scheme := runtime.NewScheme()
	err := corev1.AddToScheme(scheme)
	if err != nil {
		zap.L().Fatal("Could not add to scheme", zap.Error(err))
	}

	ourCache, err := cache.New(config.GetConfigOrDie(), cache.Options{ByObject: map[client.Object]cache.ByObject{&corev1.Node{}: {}}})
	if err != nil {
		zap.L().Fatal("Could not create our cache", zap.Error(err))
	}

	cacheCtx := context.Background()

	go func() {
		err = ourCache.Start(cacheCtx)
		if err != nil {
			zap.L().Fatal("Could not start our cache", zap.Error(err))
		}
	}()

	success := ourCache.WaitForCacheSync(context.Background())
	if !success {
		zap.L().Warn("Could not warm cached client during initialization")
	} else {
		zap.L().Info("Done warming client cache")
	}

	globalClient, err = client.New(config.GetConfigOrDie(), client.Options{
		Scheme: scheme,
		Cache:  &client.CacheOptions{Reader: ourCache},
	})
	if err != nil {
		zap.L().Fatal("Failed to create a new client: %v", zap.Error(err))
	}

	// init command flags
	flag.IntVar(&port, "port", 8443, "Webhook server port.")
	flag.StringVar(&certFile, "tlsCertFile", "/tmp/k8s-webhook-server/serving-certs/tls.crt", "x509 Certificate file.")
	flag.StringVar(&keyFile, "tlsKeyFile", "/tmp/k8s-webhook-server/serving-certs/tls.key", "x509 private key file.")
	flag.StringVar(&caCrtFile, "tlsCaFile", "/tmp/k8s-webhook-server/serving-certs/ca.crt", "x509 Certificate file.")
	flag.Parse()

	certBytes, err := os.ReadFile(certFile)
	if err != nil {
		zap.L().Fatal("Failed to read the certificate file: %v", zap.Error(err))
	}

	certKeyBytes, err := os.ReadFile(keyFile)
	if err != nil {
		zap.L().Fatal("Failed to read the private key file: %v", zap.Error(err))
	}

	pair, err := tls.X509KeyPair(certBytes, certKeyBytes)
	if err != nil {
		zap.L().Fatal("Failed to load certificate key pair: %v", zap.Error(err))
	}

	// XXX find a way for apiserver to present client certificate for mTLS
	//caCertPool := x509.NewCertPool()
	//caCertPool.AppendCertsFromPEM(caCrtBytes)

	tlsConfig := &tls.Config{
		Certificates: []tls.Certificate{pair},
		//ClientCAs:    caCertPool, // XXX find a way for apiserver to present client certificate for mTLS
		//ClientAuth:   tls.RequireAndVerifyClientCert, // XXX find a way for apiserver to present client certificate for mTLS
	}

	webhookServer := &WebhookServer{
		server: &http.Server{
			Addr:      fmt.Sprintf(":%v", port),
			TLSConfig: tlsConfig,
		},
	}

	// define http server and server handler
	mux := http.NewServeMux()
	mux.HandleFunc("/mutate", webhookServer.serve)
	webhookServer.server.Handler = mux

	zap.L().Info("Starting webhook server", zap.String("address", webhookServer.server.Addr))

	// start webhook server in new routine
	go func() {
		if err := webhookServer.server.ListenAndServeTLS("", ""); err != nil {
			zap.L().Fatal("Failed to listen and serve webhook server: %v", zap.Error(err))
		}
	}()

	// listening OS shutdown singal
	signalChan := make(chan os.Signal, 1)
	signal.Notify(signalChan, syscall.SIGINT, syscall.SIGTERM)
	<-signalChan

	zap.L().Info("Got OS shutdown signal, shutting down webhook server gracefully.")
	cacheCtx.Done()
	err = webhookServer.server.Shutdown(context.Background())
	if err != nil {
		zap.L().Error("Problem while shutting down webhook server", zap.Error(err))
	}
}
