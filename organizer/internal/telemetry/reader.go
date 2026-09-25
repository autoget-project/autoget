package telemetry

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
)

// ErrTraceNotFound is returned when no spans matching the given traceID are found.
var ErrTraceNotFound = errors.New("trace not found")

// ReadTraceSpans reads a JSON Lines trace file, filters spans by traceID,
// and returns them sorted by StartTime ascending.
// It tolerates a torn/truncated final line (e.g., if the writer process was killed
// or crashed mid-write) by skipping the unparseable final line and logging or ignoring it.
func ReadTraceSpans(filePath string, traceID string) ([]SpanRecord, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to open trace file %s: %w", filePath, err)
	}
	defer func() {
		_ = file.Close()
	}()

	var matchedSpans []SpanRecord
	reader := bufio.NewReader(file)

	for {
		line, isPrefix, readErr := reader.ReadLine()
		if readErr != nil {
			if errors.Is(readErr, io.EOF) {
				break
			}
			return nil, fmt.Errorf("error reading trace file %s: %w", filePath, readErr)
		}

		// If line was too long for buffer, read the rest of the line
		var fullLine []byte
		if isPrefix {
			fullLine = append(fullLine, line...)
			for isPrefix {
				var chunk []byte
				chunk, isPrefix, readErr = reader.ReadLine()
				if readErr != nil && !errors.Is(readErr, io.EOF) {
					return nil, fmt.Errorf("error reading long line in trace file %s: %w", filePath, readErr)
				}
				fullLine = append(fullLine, chunk...)
				if readErr != nil {
					break
				}
			}
		} else {
			fullLine = line
		}

		trimmed := bytes.TrimSpace(fullLine)
		if len(trimmed) == 0 {
			continue
		}

		var record SpanRecord
		if err := json.Unmarshal(trimmed, &record); err != nil {
			// Check if this might be a torn line at the end of the file
			// Peek if we are at EOF
			if _, peekErr := reader.Peek(1); errors.Is(peekErr, io.EOF) || errors.Is(readErr, io.EOF) {
				// Torn line at EOF - tolerate and skip
				break
			}
			// Middle-of-file corruption or malformed JSON: skip with warning or continue
			continue
		}

		if traceID == "" || record.TraceID == traceID {
			matchedSpans = append(matchedSpans, record)
		}
	}

	if len(matchedSpans) == 0 && traceID != "" {
		return nil, ErrTraceNotFound
	}

	// Sort spans by StartTime ascending
	sort.Slice(matchedSpans, func(i, j int) bool {
		return matchedSpans[i].StartTime.Before(matchedSpans[j].StartTime)
	})

	return matchedSpans, nil
}

// GetLatestTraceID reads the trace file from the end backwards to quickly find
// the most recent valid SpanRecord's TraceID.
// It tolerates a torn/truncated final line by skipping it.
func GetLatestTraceID(filePath string) (string, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return "", fmt.Errorf("failed to open trace file %s: %w", filePath, err)
	}
	defer func() {
		_ = file.Close()
	}()

	stat, err := file.Stat()
	if err != nil {
		return "", fmt.Errorf("failed to stat trace file %s: %w", filePath, err)
	}

	fileSize := stat.Size()
	if fileSize == 0 {
		return "", errors.New("trace file is empty")
	}

	// Read backwards in chunks
	const chunkSize = 4096
	offset := fileSize
	var leftover []byte

	for offset > 0 {
		readSize := int64(chunkSize)
		if offset < readSize {
			readSize = offset
		}
		offset -= readSize

		buf := make([]byte, readSize)
		if _, err := file.ReadAt(buf, offset); err != nil && !errors.Is(err, io.EOF) {
			return "", fmt.Errorf("failed to read trace file backwards: %w", err)
		}

		current := append(buf, leftover...)
		lines := bytes.Split(current, []byte("\n"))

		// If offset > 0, the first element may be an incomplete line
		if offset > 0 {
			leftover = lines[0]
			lines = lines[1:]
		} else {
			leftover = nil
		}

		// Traverse lines backwards
		for i := len(lines) - 1; i >= 0; i-- {
			line := bytes.TrimSpace(lines[i])
			if len(line) == 0 {
				continue
			}

			var rec SpanRecord
			if err := json.Unmarshal(line, &rec); err == nil && rec.TraceID != "" {
				return rec.TraceID, nil
			}
			// If JSON unmarshal failed, could be a torn line at EOF or bad line, keep checking earlier lines
		}
	}

	return "", errors.New("no valid span record with trace ID found in trace file")
}

// StringAttr returns string attribute value from SpanRecord attributes map.
func (s SpanRecord) StringAttr(key string) string {
	if s.Attributes == nil {
		return ""
	}
	if v, ok := s.Attributes[key]; ok {
		if sVal, ok := v.(string); ok {
			return sVal
		}
		return fmt.Sprintf("%v", v)
	}
	return ""
}

// BoolAttr returns bool attribute value from SpanRecord attributes map.
func (s SpanRecord) BoolAttr(key string) bool {
	if s.Attributes == nil {
		return false
	}
	if v, ok := s.Attributes[key]; ok {
		if bVal, ok := v.(bool); ok {
			return bVal
		}
		if sVal, ok := v.(string); ok {
			return strings.EqualFold(sVal, "true")
		}
	}
	return false
}

// IntAttr returns int attribute value from SpanRecord attributes map.
func (s SpanRecord) IntAttr(key string) int {
	if s.Attributes == nil {
		return 0
	}
	if v, ok := s.Attributes[key]; ok {
		switch num := v.(type) {
		case float64:
			return int(num)
		case int:
			return num
		case int64:
			return int(num)
		}
	}
	return 0
}
