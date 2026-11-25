package ingest

import (
	"bufio"
	"bytes"
	"errors"
	"io"
	"os"
	"regexp"
)

var mboxEscape = regexp.MustCompile(`^>+From `)

// MboxReader streams messages out of an mbox file. It accepts mboxo and mboxrd
// escaping and uses the conventional blank-line + "From " separator heuristic.
type MboxReader struct {
	r           *bufio.Reader
	started     bool
	done        bool
	pendingLine []byte
}

func NewMboxReader(r io.Reader) *MboxReader {
	return &MboxReader{r: bufio.NewReaderSize(r, 64*1024)}
}

func (m *MboxReader) Next() ([]byte, error) {
	if m.done {
		return nil, io.EOF
	}

	if !m.started {
		for {
			line, err := m.readLine()
			if len(line) > 0 && bytes.HasPrefix(line, []byte("From ")) {
				m.started = true
				break
			}
			if err != nil {
				m.done = true
				if errors.Is(err, io.EOF) {
					return nil, io.EOF
				}
				return nil, err
			}
			// Tolerate leading blank lines before the first separator.
			if len(bytes.TrimSpace(line)) > 0 {
				m.pendingLine = line
				m.started = true
				break
			}
		}
	}

	var buf bytes.Buffer
	prevBlank := true
	for {
		var line []byte
		var err error
		if m.pendingLine != nil {
			line = m.pendingLine
			m.pendingLine = nil
		} else {
			line, err = m.readLine()
		}

		if len(line) > 0 {
			if isMboxSeparator(line, prevBlank) && buf.Len() > 0 {
				m.pendingLine = line
				return buf.Bytes(), nil
			}
			if isMboxSeparator(line, prevBlank) && buf.Len() == 0 {
				// Empty message or the initial separator; skip it.
				prevBlank = true
				if err != nil {
					m.done = true
				}
				continue
			}
			if unescaped := mboxEscape.ReplaceAll(line, []byte("From ")); len(unescaped) != len(line) {
				line = unescaped
			}
			buf.Write(line)
			prevBlank = len(bytes.TrimSpace(line)) == 0
		}

		if err != nil {
			m.done = true
			if buf.Len() > 0 {
				return buf.Bytes(), nil
			}
			if errors.Is(err, io.EOF) {
				return nil, io.EOF
			}
			return nil, err
		}
	}
}

func (m *MboxReader) readLine() ([]byte, error) {
	line, err := m.r.ReadBytes('\n')
	if len(line) > 0 {
		return line, err
	}
	return nil, err
}

func isMboxSeparator(line []byte, prevBlank bool) bool {
	return prevBlank && bytes.HasPrefix(line, []byte("From "))
}

// CountMboxMessages counts separator lines in a file without holding messages in
// memory. Used only for job progress.
func CountMboxMessages(path string) (int, error) {
	f, err := os.Open(path)
	if err != nil {
		return 0, err
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 64*1024), 16*1024*1024)
	count := 0
	prevBlank := true
	firstNonEmpty := false
	for scanner.Scan() {
		line := scanner.Bytes()
		trimmed := bytes.TrimSpace(line)
		if !firstNonEmpty {
			if len(trimmed) == 0 {
				continue
			}
			firstNonEmpty = true
			if bytes.HasPrefix(line, []byte("From ")) {
				count++
				prevBlank = true
				continue
			}
		}
		if isMboxSeparator(line, prevBlank) {
			count++
		}
		prevBlank = len(trimmed) == 0
	}
	if err := scanner.Err(); err != nil {
		return count, err
	}
	if count == 0 && firstNonEmpty {
		count = 1
	}
	return count, nil
}
