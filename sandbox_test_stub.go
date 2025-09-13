package main

import (
	"archive/zip"
	"bytes"
	"io"
)

// StaticURLSandbox is a lightweight Sandbox implementation for tests that use an httptest.Server as upstream.
type StaticURLSandbox struct {
	baseURL string
	running bool
}

func NewStaticURLSandbox(url string) *StaticURLSandbox {
	return &StaticURLSandbox{baseURL: url, running: true}
}

func (s *StaticURLSandbox) Start(_ map[string]AuthConfig) error { s.running = true; return nil }
func (s *StaticURLSandbox) OpencodeURL() string                 { return s.baseURL }
func (s *StaticURLSandbox) Stop() error                         { s.running = false; return nil }
func (s *StaticURLSandbox) IsRunning() bool                     { return s.running }

// DownloadZip returns an empty zip archive for compatibility.
func (s *StaticURLSandbox) DownloadZip() (io.ReadCloser, error) {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	_ = zw.Close()
	return io.NopCloser(bytes.NewReader(buf.Bytes())), nil
}
