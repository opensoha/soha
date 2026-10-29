package contentdlp

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/binary"
	"io"
	"io/fs"
	"path"
	"sort"
	"strings"

	domain "github.com/opensoha/soha/internal/domain/contentdlp"
)

const (
	maxArchiveEntries = 64
	maxArchiveDepth   = 3
	maxArchiveRatio   = 100
)

type archiveInspection struct {
	ctx     context.Context
	bytes   int
	entries int
	texts   []string
}

// All budgets are shared across the recursive walk. No archive path is opened.
func archiveTexts(ctx context.Context, data []byte) ([]string, string) {
	a := archiveInspection{ctx: ctx}
	status := a.inspect(data, 0)
	if status != "clean" {
		return nil, status
	}
	return a.texts, status
}

func (a *archiveInspection) inspect(data []byte, depth int) string {
	if a.ctx.Err() != nil {
		return "timeout"
	}
	kind := archiveKind(data)
	if kind == "" {
		return a.text(string(data))
	}
	if depth >= maxArchiveDepth {
		return "too_large"
	}
	switch kind {
	case "zip":
		return a.zip(data, depth)
	case "tar":
		return a.tar(data, depth)
	default:
		return a.gzip(data, depth)
	}
}

func archiveKind(data []byte) string {
	switch {
	case bytes.HasPrefix(data, []byte("PK\x03\x04")), bytes.HasPrefix(data, []byte("PK\x05\x06")):
		return "zip"
	case bytes.HasPrefix(data, []byte{0x1f, 0x8b}):
		return "gzip"
	case len(data) >= 512 && string(data[257:262]) == "ustar":
		return "tar"
	}
	return ""
}

func (a *archiveInspection) text(text string) string {
	if status := contentStatus(text, int64(len(text)), false); status != "clean" {
		return status
	}
	if a.bytes+len(text) > domain.MaxTextBytes {
		return "too_large"
	}
	a.bytes += len(text)
	a.texts = append(a.texts, text)
	return "clean"
}

func (a *archiveInspection) entry(name string, seen map[string]bool) string {
	a.entries++
	if a.entries > maxArchiveEntries {
		return "too_large"
	}
	name = strings.TrimSuffix(name, "/")
	if len(name) == 0 || len(name) > 1024 || path.IsAbs(name) || path.Clean(name) != name || name == ".." || strings.HasPrefix(name, "../") || strings.ContainsAny(name, "\\:") {
		return "unsupported"
	}
	key := strings.ToLower(name)
	if seen[key] {
		return "unsupported"
	}
	seen[key] = true
	return a.text(name)
}

type archiveContextReader struct {
	ctx context.Context
	r   io.Reader
}

func (r archiveContextReader) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.r.Read(p)
}

func (a *archiveInspection) read(r io.Reader, remaining int) ([]byte, string) {
	remaining = min(remaining, domain.MaxTextBytes-a.bytes)
	if remaining < 0 {
		return nil, "too_large"
	}
	data, err := io.ReadAll(io.LimitReader(archiveContextReader{a.ctx, r}, int64(remaining)+1))
	if a.ctx.Err() != nil {
		return nil, "timeout"
	}
	if err != nil {
		return nil, "error"
	}
	if len(data) > remaining {
		return nil, "too_large"
	}
	return data, "clean"
}

func (a *archiveInspection) zip(data []byte, depth int) string {
	_, _, count, status := zipDirectory(data)
	if status != "clean" {
		return status
	}
	if count > maxArchiveEntries-a.entries {
		return "too_large"
	}
	r, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return "error"
	}
	if status := zipFraming(data, r.File); status != "clean" {
		return status
	}
	if status := a.text(r.Comment); status != "clean" {
		return status
	}
	seen, expanded := map[string]bool{}, 0
	for _, f := range r.File {
		if status := zipMemberStatus(f); status != "clean" {
			return status
		}
		if status := a.entry(f.Name, seen); status != "clean" {
			return status
		}
		if status := a.text(f.Comment); status != "clean" {
			return status
		}
		if f.FileInfo().IsDir() {
			continue
		}
		body, status := a.zipFile(f, len(data)*maxArchiveRatio-expanded)
		if status != "clean" {
			return status
		}
		expanded += len(body)
		if status := a.nested(body, depth); status != "clean" {
			return status
		}
	}
	return "clean"
}

func zipMemberStatus(f *zip.File) string {
	if f.Flags&1 != 0 {
		return "encrypted"
	}
	if f.ReaderVersion > 20 || f.Method != zip.Store && f.Method != zip.Deflate || f.Flags & ^uint16(0x80e) != 0 {
		return "unsupported"
	}
	if f.Name == "[Content_Types].xml" || f.Name == "mimetype" {
		return "unsupported" // Office/ODF need a document-aware extractor.
	}
	if f.FileInfo().IsDir() {
		if f.CRC32 != 0 {
			return "error"
		}
		if f.Mode().Type() != fs.ModeDir || f.UncompressedSize64 != 0 || f.CompressedSize64 != 0 {
			return "unsupported"
		}
	} else if !f.Mode().IsRegular() {
		return "unsupported"
	}
	return "clean"
}

func (a *archiveInspection) zipFile(f *zip.File, remaining int) ([]byte, string) {
	size := f.CompressedSize64
	if size > domain.MaxTextBytes {
		return nil, "error"
	}
	remaining = min(remaining, int(size)*maxArchiveRatio)
	if f.UncompressedSize64 > uint64(max(0, min(remaining, domain.MaxTextBytes-a.bytes))) {
		return nil, "too_large"
	}
	r, err := f.Open()
	if err != nil {
		return nil, "error"
	}
	body, status := a.read(r, remaining)
	if err := r.Close(); err != nil && status == "clean" {
		status = "error"
	}
	return body, status
}

func (a *archiveInspection) nested(data []byte, depth int) string {
	if archiveKind(data) != "" {
		// Account for intermediate containers as well as their expanded leaves.
		a.bytes += len(data)
		if a.bytes > domain.MaxTextBytes {
			return "too_large"
		}
	}
	return a.inspect(data, depth+1)
}

func (a *archiveInspection) gzip(data []byte, depth int) string {
	source := bytes.NewReader(data)
	r, err := gzip.NewReader(source)
	if err != nil {
		return "error"
	}
	r.Multistream(false)
	if len(r.Extra) != 0 {
		_ = r.Close()
		return "unsupported"
	}
	if r.Name != "" {
		if status := a.entry(r.Name, map[string]bool{}); status != "clean" {
			_ = r.Close()
			return status
		}
	} else {
		a.entries++
	}
	if status := a.text(r.Comment); status != "clean" {
		_ = r.Close()
		return status
	}
	body, status := a.read(r, len(data)*maxArchiveRatio)
	if err := r.Close(); err != nil && status == "clean" {
		status = "error"
	}
	if source.Len() != 0 && status == "clean" {
		return "error" // Reject concatenation instead of inspecting a safe prefix.
	}
	if a.entries > maxArchiveEntries {
		return "too_large"
	}
	if status != "clean" {
		return status
	}
	return a.nested(body, depth)
}

func (a *archiveInspection) tar(data []byte, depth int) string {
	if len(data)%512 != 0 || len(data) < 1024 || !allZero(data[len(data)-1024:]) {
		return "error"
	}
	source := bytes.NewReader(data)
	r := tar.NewReader(source)
	seen := map[string]bool{}
	for {
		h, err := r.Next()
		if err == io.EOF {
			if !allZero(data[len(data)-source.Len():]) {
				return "error"
			}
			return "clean"
		}
		if err != nil {
			return "error"
		}
		if h.Format != tar.FormatUSTAR || len(h.PAXRecords) != 0 || h.Typeflag != tar.TypeReg && h.Typeflag != tar.TypeDir {
			return "unsupported"
		}
		if status := a.entry(h.Name, seen); status != "clean" {
			return status
		}
		if h.Typeflag == tar.TypeDir {
			if h.Size != 0 {
				return "unsupported"
			}
			continue
		}
		body, status := a.read(r, len(data)*maxArchiveRatio)
		if status != "clean" {
			return status
		}
		if status := a.nested(body, depth); status != "clean" {
			return status
		}
	}
}

func allZero(data []byte) bool {
	for _, b := range data {
		if b != 0 {
			return false
		}
	}
	return true
}

// Reject ZIP64, prefixes, gaps, duplicate local records and trailing payloads.
// Central-directory entries must describe every local record exactly once.
func zipFraming(data []byte, files []*zip.File) string {
	central, end, count, status := zipDirectory(data)
	if status != "clean" || count != len(files) {
		return "error"
	}
	position := central
	ranges := [][2]int{}
	for _, f := range files {
		if position < 0 || position+46 > end || string(data[position:position+4]) != "PK\x01\x02" || binary.LittleEndian.Uint16(data[position+34:]) != 0 {
			return "error"
		}
		start := int(binary.LittleEndian.Uint32(data[position+42:]))
		finish, status := zipLocalRecord(data, central, start, f)
		if status != "clean" {
			return status
		}
		ranges = append(ranges, [2]int{start, finish})
		size := 46
		for _, offset := range []int{28, 30, 32} {
			size += int(binary.LittleEndian.Uint16(data[position+offset:]))
		}
		position += size
	}
	if position != end {
		return "error"
	}
	sort.Slice(ranges, func(i, j int) bool { return ranges[i][0] < ranges[j][0] })
	position = 0
	for _, span := range ranges {
		if span[0] != position {
			return "error"
		}
		position = span[1]
	}
	if position != central {
		return "error"
	}
	return "clean"
}

func zipDirectory(data []byte) (int, int, int, string) {
	end := bytes.LastIndex(data, []byte("PK\x05\x06"))
	if end < 0 || end+22 > len(data) || end+22+int(binary.LittleEndian.Uint16(data[end+20:])) != len(data) {
		return 0, 0, 0, "error"
	}
	u16 := func(offset int) uint16 { return binary.LittleEndian.Uint16(data[end+offset:]) }
	u32 := func(offset int) uint32 { return binary.LittleEndian.Uint32(data[end+offset:]) }
	central := int(u32(16))
	if u16(4) != 0 || u16(6) != 0 || u16(8) != u16(10) || uint64(u32(12))+uint64(u32(16)) != uint64(end) {
		return 0, 0, 0, "error"
	}
	return central, end, int(u16(10)), "clean"
}

func zipLocalRecord(data []byte, limit, start int, f *zip.File) (int, string) {
	offset, valid := zipLocalOffset(data, limit, start, f)
	if !valid {
		return 0, "error"
	}
	size := f.CompressedSize64
	if size > domain.MaxTextBytes {
		return 0, "error"
	}
	compressed := int(size)
	if compressed > limit-offset {
		return 0, "error"
	}
	end := offset + compressed
	if f.Flags&8 != 0 {
		if end+4 <= limit && string(data[end:end+4]) == "PK\x07\x08" {
			end += 4
		}
		if end+12 > limit || binary.LittleEndian.Uint32(data[end:]) != f.CRC32 || uint64(binary.LittleEndian.Uint32(data[end+4:])) != f.CompressedSize64 || uint64(binary.LittleEndian.Uint32(data[end+8:])) != f.UncompressedSize64 {
			return 0, "error"
		}
		end += 12
	} else if binary.LittleEndian.Uint32(data[start+14:]) != f.CRC32 || uint64(binary.LittleEndian.Uint32(data[start+18:])) != f.CompressedSize64 || uint64(binary.LittleEndian.Uint32(data[start+22:])) != f.UncompressedSize64 {
		return 0, "error"
	}
	return end, "clean"
}

func zipLocalOffset(data []byte, limit, start int, f *zip.File) (int, bool) {
	if limit < 30 || limit > len(data) || start < 0 || start > limit-30 || string(data[start:start+4]) != "PK\x03\x04" {
		return 0, false
	}
	name := int(binary.LittleEndian.Uint16(data[start+26:]))
	extra := int(binary.LittleEndian.Uint16(data[start+28:]))
	offset := start + 30 + name + extra
	if offset > limit || string(data[start+30:start+30+name]) != f.Name || binary.LittleEndian.Uint16(data[start+6:]) != f.Flags || binary.LittleEndian.Uint16(data[start+8:]) != f.Method {
		return 0, false
	}
	return offset, true
}
