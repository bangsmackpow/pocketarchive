package ingest

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const mboxFixture = `From alice@example.com Mon Jan  1 00:00:00 2024
From: alice@example.com
To: archive@example.com
Subject: First
Date: Mon, 01 Jan 2024 00:00:00 +0000
Message-ID: <one@example.com>

First body.
>From the mboxrd escaped line.
From not-a-separator because no blank line before it? Actually this follows a newline.
`

func TestMboxReaderMboxoAndMboxrd(t *testing.T) {
	fixture := "From alice@example.com Mon Jan  1 00:00:00 2024\r\n" +
		"From: alice@example.com\r\nSubject: One\r\n\r\nBody one\r\n\r\n" +
		">From escaped line\r\n\r\n" +
		"From bob@example.com Mon Jan  1 00:00:01 2024\r\n" +
		"From: bob@example.com\r\nSubject: Two\r\n\r\nBody two\r\n"

	r := NewMboxReader(strings.NewReader(fixture))
	first, err := r.Next()
	if err != nil {
		t.Fatalf("first message: %v", err)
	}
	if !strings.Contains(string(first), "Subject: One") {
		t.Fatalf("first message header missing: %q", string(first))
	}
	if !strings.Contains(string(first), "From escaped line") {
		t.Fatalf("mboxrd escape not unescaped: %q", string(first))
	}
	if strings.Contains(string(first), ">From escaped") {
		t.Fatal("mboxrd escape marker remains")
	}
	second, err := r.Next()
	if err != nil {
		t.Fatalf("second message: %v", err)
	}
	if !strings.Contains(string(second), "Subject: Two") {
		t.Fatalf("second message header missing: %q", string(second))
	}
	if _, err := r.Next(); err != io.EOF {
		t.Fatalf("expected EOF, got %v", err)
	}
}

func TestCountMboxMessages(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "sample.mbox")
	if err := os.WriteFile(path, []byte(mboxFixture), 0o600); err != nil {
		t.Fatal(err)
	}
	count, err := CountMboxMessages(path)
	if err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("expected 1 message, got %d", count)
	}
}

func TestSanitizeHTML(t *testing.T) {
	in := `<p onclick="evil()">Hello <script>alert(1)</script><a href="javascript:alert(2)">link</a><img src="x" onerror="evil()"></p>`
	out := SanitizeHTML(in)
	if strings.Contains(out, "script") || strings.Contains(out, "onclick") || strings.Contains(out, "onerror") || strings.Contains(out, "javascript:") {
		t.Fatalf("dangerous HTML survived: %s", out)
	}
	if !strings.Contains(out, "Hello") {
		t.Fatalf("benign content removed: %s", out)
	}
}
