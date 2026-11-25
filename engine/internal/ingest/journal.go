package ingest

import (
	"bytes"
	"fmt"

	"github.com/jhillyerd/enmime/v2"
)

// JournalReport is an Office 365 (or generic) journal envelope with the original
// message extracted from its message/rfc822 part.
type JournalReport struct {
	EnvelopeRaw  []byte
	InnerRaw     []byte
	EnvelopeFrom string
	EnvelopeTo   []string
	IsJournal    bool
}

// ParseJournal detects and unpacks journal reports. If raw is a normal message,
// InnerRaw is the original input and IsJournal is false.
func ParseJournal(raw []byte) (JournalReport, error) {
	report := JournalReport{EnvelopeRaw: raw, InnerRaw: raw}
	env, err := enmime.ReadEnvelope(bytes.NewReader(raw))
	if err != nil {
		return JournalReport{}, fmt.Errorf("parse journal envelope: %w", err)
	}
	if env.Root == nil {
		return JournalReport{}, errNotMessage
	}

	if addrs, err := env.AddressList("From"); err == nil && len(addrs) > 0 {
		report.EnvelopeFrom = addrs[0].Address
	}
	if addrs, err := env.AddressList("To"); err == nil {
		for _, a := range addrs {
			if a.Address != "" {
				report.EnvelopeTo = append(report.EnvelopeTo, a.Address)
			}
		}
	}

	var walk func(part *enmime.Part) *enmime.Part
	walk = func(part *enmime.Part) *enmime.Part {
		if part == nil {
			return nil
		}
		if part.ContentType == "message/rfc822" && len(part.Content) > 0 {
			return part
		}
		for child := part.FirstChild; child != nil; child = child.NextSibling {
			if found := walk(child); found != nil {
				return found
			}
		}
		return nil
	}
	if inner := walk(env.Root); inner != nil {
		report.InnerRaw = inner.Content
		report.IsJournal = true
	}
	return report, nil
}
