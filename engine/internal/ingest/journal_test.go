package ingest

import (
	"strings"
	"testing"
)

const journalFixture = `From: journaling@contoso.com
To: journal@archive.example.com
Subject: Journal Report
Date: Mon, 01 Jan 2024 00:00:00 +0000
MIME-Version: 1.0
Content-Type: multipart/report; report-type=dispatched; boundary="outer"

--outer
Content-Type: text/plain

This is an Exchange journal report.
--outer
Content-Type: message/rfc822
Content-Disposition: attachment; filename="original.eml"

From: alice@example.com
To: bob@example.com
Subject: Original subject
Date: Mon, 01 Jan 2024 00:00:00 +0000
Message-ID: <original@example.com>
MIME-Version: 1.0
Content-Type: text/plain

Original body.
--outer--
`

func TestParseJournal(t *testing.T) {
	report, err := ParseJournal([]byte(journalFixture))
	if err != nil {
		t.Fatal(err)
	}
	if !report.IsJournal {
		t.Fatal("expected journal detection")
	}
	if !strings.Contains(string(report.InnerRaw), "Subject: Original subject") {
		t.Fatalf("inner message missing: %q", string(report.InnerRaw))
	}
	if strings.Contains(string(report.InnerRaw), "Journal Report") {
		t.Fatal("outer journal report leaked into inner message")
	}
	if !strings.Contains(string(report.EnvelopeRaw), "Journal Report") {
		t.Fatal("raw envelope not preserved")
	}
	if report.EnvelopeFrom == "" {
		t.Fatal("envelope sender not extracted")
	}
}

func TestParseJournalRegularMessage(t *testing.T) {
	raw := "From: a@example.com\r\nTo: b@example.com\r\nSubject: plain\r\n\r\nbody\r\n"
	report, err := ParseJournal([]byte(raw))
	if err != nil {
		t.Fatal(err)
	}
	if report.IsJournal {
		t.Fatal("regular message treated as journal")
	}
	if string(report.InnerRaw) != raw {
		t.Fatal("regular message bytes changed")
	}
}
