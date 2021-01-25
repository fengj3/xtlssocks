#!/bin/bash
compiler() {
  if [ "$1" == "windows" -o "$1" == "darwin" ];then
    suffix=".exe";
  else
    suffix="";
  fi
  CGO_ENABLED=0 GOOS=$1 GOARCH=$2 go build -o bin/xtlssocks-${1}-${2}${suffix} cmd/xtlssocks/xtlssocks.go
  CGO_ENABLED=0 GOOS=$1 GOARCH=$2 go build -o bin/tcpproxy-${1}-${2}${suffix} cmd/tcpproxy/tcpproxy.go
  CGO_ENABLED=0 GOOS=$1 GOARCH=$2 go build -trimpath -o bin/xtlssocksproxy-${1}-${2}${suffix} cmd/xtlssocksproxy/xtlssocksproxy.go
  CGO_ENABLED=0 GOOS=$1 GOARCH=$2 go build -o bin/socksclient-${1}-${2}${suffix} cmd/socksclient/socksclient.go
}
compiler linux amd64
#compiler linux 386 #has bug
compiler linux arm64
#compiler linux arm #has bug
compiler windows amd64
compiler darwin amd64