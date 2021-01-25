package main

import (
	"context"
	"github.com/allenbyus/xtls"
	"flag"
	"io"
	"net"
	"net/http"
	"time"
	"encoding/json"

	"github.com/fengj3/xtlssocks"
	"github.com/pkg/errors"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"go.uber.org/zap"
	"golang.org/x/sync/errgroup"
)

var (
	logger *zap.Logger
	cfg zap.Config
)

const (
	defaultTimeout           = 180 * time.Second
	defaultPrometheusAddress = ":9200"
)

func init() {
	rawJSON := []byte(`{
		"level": "info",
		"outputPaths": ["stdout"],
		"errorOutputPaths": ["stderr"],
		"encoding": "json",
		"encoderConfig": {
			"messageKey": "message",
			"levelKey": "level",
			"levelEncoder": "lowercase"
		}
	}`)
	if err := json.Unmarshal(rawJSON, &cfg); err != nil {
		panic(err)
	}
	l, err := cfg.Build()
	if err != nil {
		panic(err)
	}
	logger = l
}

func copyData(streamName string, dst io.Writer, src io.Reader) error {
	_, err := io.Copy(dst, src)
	if err != nil {
		return errors.Wrapf(err, "failed to copy stream %s", streamName)
	}
	return nil
}

func main() {
	defer logger.Sync()

	flagQuiet := flag.Bool("quiet", false, "only print error level of log")
	flagInsecureSkipVerify := flag.Bool("insecure-skip-verify", false, "allow insecure skipping of peer verification, when talking to the server")
	flagAddr := flag.String("addr", "0.0.0.0:8080", "address to listen to like 0.0.0.0:8001")
	flagAddrServer := flag.String("server", "", "address of the xtls socks server like 0.0.0.0:8000")
	flag.Parse()

	if(*flagQuiet) {
		cfg.Level.SetLevel(zap.ErrorLevel)
	}
	
	logger.Info(
		"Starting socks proxy to listen on addr and forward requests to server",
		zap.String("addr", *flagAddr),
		zap.String("server", *flagAddrServer),
	)

	socks5Listener, errListenSocks5 := net.Listen("tcp", *flagAddr)
	if errListenSocks5 != nil {
		logger.Fatal(
			"Error listening for incoming socks connections",
			zap.Error(errListenSocks5),
		)
	}

	defer silentClose(socks5Listener)

	var xtlsConfig *xtls.Config

	if *flagInsecureSkipVerify {
		xtlsConfig = &xtls.Config{
			InsecureSkipVerify: true,
		}
		logger.Warn("Running without verification of the xtls server - this is dangerous")
	}
	ctx := context.Background()

	go runPrometheusHandler(ctx, defaultPrometheusAddress)

	for {
		socksConn, err := socks5Listener.Accept()
		if err != nil {
			logger.Fatal(
				"error accepting incoming connections",
				zap.Error(err),
			)
		}
		logger.Info(
			"socks client connected",
			zap.String("from", socksConn.RemoteAddr().String()),
		)
		go serve(ctx, socksConn, *flagAddrServer, xtlsConfig)
	}
}

func serve(ctx context.Context, srcConn io.ReadWriteCloser, destinationAddress string, xtlsConfig *xtls.Config) {
	// Recover if a panic occurs
	defer recoverAndLogPanic()
	defer silentClose(srcConn)

	// Cancel context
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	start := time.Now()

	dstConn, errDial := xtls.DialWithDialer(&net.Dialer{
		KeepAlive: -1,
		Timeout:   defaultTimeout,
	}, "tcp", destinationAddress, xtlsConfig)

	if errDial != nil {
		logger.Warn(
			"could not reach xtls server",
			zap.Error(errDial),
		)
		return
	}
	defer silentClose(dstConn)

	group, gctx := errgroup.WithContext(ctx)

	group.Go(func() error {
		srcConn := xtlssocks.NewBufferedReader(gctx, srcConn)
		return copyData("conn->socksConn", dstConn, srcConn)
	})
	group.Go(func() error {
		dstConn := xtlssocks.NewBufferedReader(gctx, dstConn)
		return copyData("socksConn->conn", srcConn, dstConn)
	})

	if err := group.Wait(); err != nil {
		switch {
		case err == io.ErrUnexpectedEOF,
			err == io.ErrClosedPipe,
			err == io.EOF,
			err.Error() == "broken pipe":
			logger.Warn("Error occurred, while copying data", zap.Error(err))
		default:
			logger.Error("Unexpected error occurred while copying the data", zap.Error(err))
		}
	}

	logger.Info(
		"request served",
		zap.Duration("duration", time.Now().Sub(start)),
	)
}

func recoverAndLogPanic() {
	if r := recover(); r != nil {
		var err error
		switch x := r.(type) {
		case string:
			err = errors.New(x)
		case error:
			err = x
		default:
			// Fallback err (per specs, error strings should be lowercase w/o punctuation
			err = errors.New("unknown panic")
		}
		logger.Error("Panic occurred in serve thread", zap.Error(err))
	}
}

func runPrometheusHandler(_ context.Context, address string) {
	h := http.NewServeMux()
	h.Handle("/metrics", promhttp.Handler())
	logger.Fatal("Failed to start prometheus handler", zap.Error(http.ListenAndServe(address, h)))
}

func silentClose(closer io.Closer) {
	_ = closer.Close()
}