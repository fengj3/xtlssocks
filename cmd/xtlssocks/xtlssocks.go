package main

import (
	"github.com/allenbyus/xtls"
	"flag"
	"fmt"
	"encoding/json"

	"github.com/fengj3/go-socks5"
	"github.com/foomo/htpasswd"
	"go.uber.org/zap"

	"golang.org/x/crypto/bcrypt"
)

var (
	logger *zap.Logger
    cfg zap.Config
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

type Credentials map[string]string

func (s Credentials) Valid(user, password string) bool {
	hashedPassword, ok := s[user]
	if !ok {
		return false
	}
	errHash := bcrypt.CompareHashAndPassword([]byte(hashedPassword), []byte(password))
	return errHash == nil
}

func must(err error, comment ...interface{}) {
	if err != nil {
		logger.Fatal(fmt.Sprint(comment...), zap.Error(err))
	}
}

type Destination struct {
	Users []string
	Ports []int
}

func main() {
	defer logger.Sync()
	
	flagQuiet := flag.Bool("quiet", false, "only print error level of log")
	flagAddr := flag.String("addr", "", "where to listen like 127.0.0.1:8000")
	flagHtpasswdFile := flag.String("auth", "", "basic auth file")
	flagCert := flag.String("cert", "", "path to server cert.pem")
	flagKey := flag.String("key", "", "path to server key.pem")
	flag.Parse()
	
	if(*flagQuiet) {
		cfg.Level.SetLevel(zap.ErrorLevel)
	}

	passwordHashes, errParsePasswords := htpasswd.ParseHtpasswdFile(*flagHtpasswdFile)
	must(errParsePasswords, "basic auth file sucks")
	credentials := Credentials(passwordHashes)

	autenticator := socks5.UserPassAuthenticator{Credentials: credentials}

	conf := &socks5.Config {
		AuthMethods: []socks5.Authenticator{autenticator},
	}
	server, err := socks5.New(conf)
	must(err)

	logger.Info(
		"starting xtls server",
		zap.String("addr", *flagAddr),
		zap.String("cert", *flagCert),
		zap.String("key", *flagKey),
	)

	cert, errLoadKeyPair := xtls.LoadX509KeyPair(*flagCert, *flagKey)
	if errLoadKeyPair != nil {
		logger.Fatal("could not load server key pair", zap.Error(errLoadKeyPair))
	}

	listener, errListen := xtls.Listen("tcp", *flagAddr, &xtls.Config{Certificates: []xtls.Certificate{cert}})
	if errListen != nil {
		logger.Fatal(
			"could not listen for tcp / xtls",
			zap.String("addr", *flagAddr),
			zap.Error(errListen),
		)
	}
	logger.Fatal(
		"server fucked up",
		zap.Error(server.Serve(listener)),
	)
}