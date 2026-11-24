package pluginrelease

import (
	"archive/tar"
	"bufio"
	"compress/gzip"
	"context"
	"errors"
	"io"
	"io/fs"
	"os"
	"path"
	"strings"
	"unicode"
	"unicode/utf8"
)

const (
	webDirectory     = ".web"
	maxWebBytes      = 512 << 20
	maxWebPaths      = 10_000
	maxWebPathLength = 1024
	maxWebPathDepth  = 32
)

type webPath struct {
	name     string
	dir      bool
	explicit bool
	size     int64
}

type webArchive struct {
	paths map[string]webPath
	apply func(webPath, io.Reader) error
}

func readWebArchive(ctx context.Context, source io.Reader, apply func(webPath, io.Reader) error) (int, error) {
	buffered := bufio.NewReader(source)
	compressed, err := gzip.NewReader(buffered)
	if err != nil {
		return 0, errArtifact
	}
	defer func() { _ = compressed.Close() }()
	compressed.Multistream(false)
	// Include metadata and padding in the decompression bound.
	limited := &io.LimitedReader{R: cancelReader{ctx, compressed}, N: maxWebBytes + 1}
	archive := webArchive{paths: make(map[string]webPath), apply: apply}
	if err := archive.readEntries(tar.NewReader(limited)); err != nil {
		return 0, err
	}
	if err := checkTarPadding(limited); err != nil {
		return 0, err
	}
	_, trailing := buffered.Peek(1)
	index := archive.paths["index.html"]
	if limited.N == 0 || trailing != io.EOF || index.dir || index.name != "index.html" {
		return 0, errArtifact
	}
	return len(archive.paths), nil
}

func (archive *webArchive) readEntries(reader *tar.Reader) error {
	for count := 0; ; count++ {
		header, err := reader.Next()
		if err == io.EOF {
			return nil
		}
		if err != nil || count >= maxWebPaths {
			return errArchiveRead(err)
		}
		entry, err := canonicalWebPath(header)
		if err != nil {
			return err
		}
		if entry.name != "" {
			if err := archive.record(entry, reader); err != nil {
				return err
			}
		}
	}
}

func canonicalWebPath(header *tar.Header) (webPath, error) {
	if header.Typeflag != tar.TypeReg && header.Typeflag != tar.TypeDir ||
		header.Size < 0 || header.Size > maxWebBytes {
		return webPath{}, errArtifact
	}
	for key := range header.PAXRecords {
		if strings.HasPrefix(key, "GNU.sparse.") {
			return webPath{}, errArtifact
		}
	}
	if header.Typeflag == tar.TypeDir {
		if header.Size != 0 {
			return webPath{}, errArtifact
		}
		if header.Name == "." || header.Name == "./" {
			return webPath{}, nil
		}
	}
	name := strings.TrimPrefix(header.Name, "./")
	if header.Typeflag == tar.TypeDir {
		name = strings.TrimSuffix(name, "/")
	}
	if !validWebPath(name) {
		return webPath{}, errArtifact
	}
	return webPath{name: name, dir: header.Typeflag == tar.TypeDir, explicit: true, size: header.Size}, nil
}

func validWebPath(name string) bool {
	if len(name) > maxWebPathLength || strings.Count(name, "/") >= maxWebPathDepth ||
		!utf8.ValidString(name) || strings.Contains(name, "\\") || strings.ContainsFunc(name, unicode.IsControl) {
		return false
	}
	for segment := range strings.SplitSeq(name, "/") {
		if segment == "" || segment == "." || segment == ".." || len(segment) > 255 {
			return false
		}
	}
	return true
}

func (archive *webArchive) record(entry webPath, content io.Reader) error {
	key := strings.ToLower(entry.name)
	if old, found := archive.paths[key]; found {
		// An explicit directory may follow its children exactly once.
		if old.name != entry.name || old.explicit || !old.dir || !entry.dir || !entry.explicit {
			return errArtifact
		}
		archive.paths[key] = entry
		return nil
	}
	parent := path.Dir(entry.name)
	if parent != "." {
		existing, found := archive.paths[strings.ToLower(parent)]
		if found && (!existing.dir || existing.name != parent) {
			return errArtifact
		}
		if !found {
			if err := archive.record(webPath{name: parent, dir: true}, nil); err != nil {
				return err
			}
		}
	}
	if len(archive.paths) >= maxWebPaths {
		return errArtifact
	}
	archive.paths[key] = entry
	return archive.apply(entry, content)
}

func checkTarPadding(input io.Reader) error {
	buffer := make([]byte, 32<<10)
	for {
		count, err := input.Read(buffer)
		for _, value := range buffer[:count] {
			if value != 0 {
				return errArtifact
			}
		}
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return errArchiveRead(err)
		}
	}
}

func errArchiveRead(err error) error {
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	return errArtifact
}

type cancelReader struct {
	context context.Context
	reader  io.Reader
}

func (reader cancelReader) Read(buffer []byte) (int, error) {
	if err := reader.context.Err(); err != nil {
		return 0, err
	}
	return reader.reader.Read(buffer)
}

func extractWeb(ctx context.Context, directory *os.Root, archiveName string) error {
	archive, err := openPrivateFile(directory, archiveName)
	if err != nil {
		return err
	}
	defer func() { _ = archive.Close() }()
	web, err := privateChild(directory, webDirectory, true)
	if err != nil {
		return err
	}
	defer func() { _ = web.Close() }()
	_, err = readWebArchive(ctx, archive, func(entry webPath, input io.Reader) error {
		if entry.dir {
			return web.Mkdir(entry.name, 0o700)
		}
		return writePrivateFile(web, entry.name, func(output io.Writer) error {
			_, err := io.Copy(output, input)
			return err
		})
	})
	if err != nil {
		return err
	}
	return syncWebDirectories(web)
}

func syncWebDirectories(web *os.Root) error {
	return fs.WalkDir(web.FS(), ".", func(name string, entry fs.DirEntry, err error) error {
		if err != nil || !entry.IsDir() {
			return err
		}
		return syncDirectory(web, name)
	})
}
