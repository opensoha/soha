package environment

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"hash"
	"io"
	"time"
)

const ExportResultVersion = "opensoha.dev/personal-environment-export/v1"

// ponytail: bounded synchronous downloads; use the artifact store for archives
// above 256 MiB rather than silently truncating or buffering them in memory.
const ExportMaxBytes int64 = 256 << 20
const exportChunkBytes = 32 << 10

type ExportResult struct {
	SchemaVersion string    `json:"schemaVersion"`
	LeaseID       string    `json:"leaseId"`
	RunID         string    `json:"runId"`
	OwnerID       string    `json:"ownerId"`
	ScopeHash     string    `json:"scopeHash"`
	SizeBytes     int64     `json:"sizeBytes"`
	SHA256        string    `json:"sha256"`
	ObservedAt    time.Time `json:"observedAt"`
}

func (r ExportResult) ValidateFor(request Request) error {
	if request.Validate() != nil || request.Action != "export_data" || r.SchemaVersion != ExportResultVersion || r.LeaseID != request.Lease.ID || r.RunID != request.Lease.RunID || r.OwnerID != request.Lease.OwnerID || r.ScopeHash != request.Lease.ScopeHash || r.SizeBytes < 1 || r.SizeBytes > ExportMaxBytes || !scopePattern.MatchString(r.SHA256) || r.ObservedAt.IsZero() {
		return errors.New("invalid personal data export receipt")
	}
	return nil
}

// Each archive chunk has a four-byte big-endian length. Zero ends the archive;
// a length-prefixed JSON receipt follows only after Docker and cleanup succeed.
// EOF, a partial archive or a failed producer cannot become a confirmed export.
type ExportWriter struct {
	Writer io.Writer
	size   int64
	digest hash.Hash
}

func (w *ExportWriter) Write(data []byte) (int, error) {
	if w.size+int64(len(data)) > ExportMaxBytes {
		return 0, errors.New("personal data archive exceeds 256 MiB")
	}
	if w.digest == nil {
		w.digest = sha256.New()
	}
	written := 0
	for len(data) > 0 {
		n := min(len(data), exportChunkBytes)
		if err := binary.Write(w.Writer, binary.BigEndian, uint32(n)); err != nil {
			return written, err
		}
		if count, err := w.Writer.Write(data[:n]); err != nil || count != n {
			return written, errors.Join(err, io.ErrShortWrite)
		}
		_, _ = w.digest.Write(data[:n])
		w.size += int64(n)
		written += n
		data = data[n:]
	}
	return written, nil
}

func (w *ExportWriter) Finish(request Request) (ExportResult, error) {
	if w.digest == nil {
		return ExportResult{}, errors.New("empty personal data archive")
	}
	l := request.Lease
	r := ExportResult{SchemaVersion: ExportResultVersion, LeaseID: l.ID, RunID: l.RunID, OwnerID: l.OwnerID, ScopeHash: l.ScopeHash, SizeBytes: w.size, SHA256: "sha256:" + hex.EncodeToString(w.digest.Sum(nil)), ObservedAt: time.Now().UTC()}
	if err := r.ValidateFor(request); err != nil {
		return ExportResult{}, err
	}
	data, err := json.Marshal(r)
	if err != nil {
		return ExportResult{}, err
	}
	if err := binary.Write(w.Writer, binary.BigEndian, uint32(0)); err != nil {
		return ExportResult{}, err
	}
	// #nosec G115 -- ValidateFor bounds all three lease IDs to 128 runes and the remaining receipt fields are fixed or bounded.
	if err := binary.Write(w.Writer, binary.BigEndian, uint32(len(data))); err != nil {
		return ExportResult{}, err
	}
	if n, err := w.Writer.Write(data); err != nil || n != len(data) {
		return ExportResult{}, errors.Join(err, io.ErrShortWrite)
	}
	return r, nil
}

func ReadExport(reader io.Reader, target io.Writer, request Request) (ExportResult, error) {
	var size int64
	digest := sha256.New()
	buffer := make([]byte, exportChunkBytes)
	for {
		var n uint32
		if err := binary.Read(reader, binary.BigEndian, &n); err != nil {
			return ExportResult{}, err
		}
		if n == 0 {
			break
		}
		if n > exportChunkBytes || size+int64(n) > ExportMaxBytes {
			return ExportResult{}, errors.New("personal data export exceeds its bound")
		}
		if _, err := io.ReadFull(reader, buffer[:n]); err != nil {
			return ExportResult{}, err
		}
		if _, err := io.Copy(io.MultiWriter(target, digest), bytes.NewReader(buffer[:n])); err != nil {
			return ExportResult{}, err
		}
		size += int64(n)
	}
	var receiptBytes uint32
	if err := binary.Read(reader, binary.BigEndian, &receiptBytes); err != nil || receiptBytes == 0 || receiptBytes > 4096 {
		return ExportResult{}, errors.New("personal data export has no bounded receipt")
	}
	data := make([]byte, receiptBytes)
	if _, err := io.ReadFull(reader, data); err != nil {
		return ExportResult{}, err
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var result ExportResult
	if decoder.Decode(&result) != nil || decoder.Decode(new(any)) != io.EOF || result.ValidateFor(request) != nil || result.SizeBytes != size || result.SHA256 != "sha256:"+hex.EncodeToString(digest.Sum(nil)) {
		return ExportResult{}, errors.New("personal data export integrity or identity mismatch")
	}
	if n, err := reader.Read(buffer[:1]); n != 0 || err != io.EOF {
		return ExportResult{}, errors.New("personal data export contains trailing bytes")
	}
	return result, nil
}
