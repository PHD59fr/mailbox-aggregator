package main

import (
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"time"
)

// healthcheck supports shell-free container images without opening the data store.
func healthcheck(address string) error {
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return fmt.Errorf("invalid listen address: %w", err)
	}
	switch host {
	case "", "0.0.0.0":
		host = "127.0.0.1"
	case "::":
		host = "::1"
	}
	client := &http.Client{
		Timeout:       2 * time.Second,
		Transport:     &http.Transport{}, // Do not send the local probe through an HTTP proxy.
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
	defer client.CloseIdleConnections()
	target := url.URL{Scheme: "http", Host: net.JoinHostPort(host, port), Path: "/healthz"}
	response, err := client.Get(target.String())
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return errors.New("health endpoint did not return HTTP 200")
	}
	return nil
}
