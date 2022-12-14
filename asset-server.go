package vitgo

import (
	"embed"
	"io/fs"
	"log"
	"net/http"
	"path/filepath"
	"strings"
)

//go:embed react
var embedFiles embed.FS

// FileServer is a customized version of http.FileServer
// that can handle either an embed.FS or a os.DirFS fs.FS.
// Since development directories used for hot updates
// can contain dot files (potentially with sensitive
// information) the code checks to make sure that dot files
// are not served.
func (vg *VitGo) FileServer() (http.Handler, error) {
	// First, make sure if our fs.FS is from an embed.FS,
	// that we adjust where the FS is "pointing".
	target, err := correctEmbedFS(vg.DistFS, vg.JSProjectPath)
	if err != nil {
		return nil, err
	}

	// Prevent directory listings
	wrapped := wrapperFS{
		FS: target,
	}

	handler := vg.guardedFileServer(wrapped)

	return handler, nil
}

// guardedFileServer wraps http.FileServer with filtering
// code that checks for dot files and other potentially
// sensitive material a static file server should not
// render over.
//
// We assume that the fs.Dir's top level is pointed at the contents
// of where the assets are, and not its parent directory as would
// typically be the case for an embed.FS instance.
//
// In both prod and dev, serveDir should point to the js dir.
// We will adjust prod to add the relative path to dist.
func (vg *VitGo) guardedFileServer(serveDir fs.FS) http.Handler {
	stripPrefix := "/"

	handler := func(w http.ResponseWriter, r *http.Request) {
		prefixLen := len(stripPrefix)
		rest := r.URL.Path[prefixLen:]
		parts := strings.Split(rest, "/")

		// Now walk the parts and make sure none of them are
		// either "hidden" files or directories.
		for _, stem := range parts {
			if len(stem) > 0 && stem[:1] == "." {
				http.NotFound(w, r)
				return
			}
		}

		// handle any special-cased files
		if len(parts) > 0 {
			baseFile := parts[len(parts)-1]

			if baseFile == "preamble.js" {
				// react preamble file
				bytes, err := embedFiles.ReadFile("react/preamble.js")
				if err != nil {
					log.Println("could not load preamble:", err)
					http.NotFound(w, r)

					return
				}

				serveOneFile(w, r, bytes, "application/javascript")
				return
			}
		}

		if vg.Debug {
			escapedURLPath := strings.Replace(r.URL.Path, "\n", "", -1)
			escapedURLPath = strings.Replace(escapedURLPath, "\r", "", -1)

			log.Println("entered FS", escapedURLPath)
			dir, err := fs.ReadDir(serveDir, ".")

			if err != nil {
				log.Println("could not read the asset dir", err)
				http.NotFound(w, r)
				return
			}

			for _, item := range dir {
				log.Println(item.Name())
			}
		}

		var loggingFS http.Handler
		var fileServer http.Handler

		if vg.Environment == "production" {
			// We actually want to read from the dist subdir of
			// the JSDir.
			newDir, err := fs.Sub(serveDir, vg.AssetPath)
			if err != nil {
				w.WriteHeader(http.StatusNotFound)
				return
			}

			loggingFS = logRequest(http.FileServer(http.FS(newDir)))
			fileServer = loggingFS
		} else {
			loggingFS = logRequest(http.FileServer(http.FS(serveDir)))
			fileServer = http.StripPrefix(stripPrefix, loggingFS)
		}

		fileServer.ServeHTTP(w, r)
	}

	return http.HandlerFunc(handler)
}

// Wrapper file system to prevent listing of directories
// forked from: https://www.alexedwards.net/blog/disable-http-fileserver-directory-listings
type wrapperFS struct {
	FS fs.FS
}

// Open implements the fs.FS interface for wrapperFS
func (wrpr wrapperFS) Open(path string) (fs.File, error) {
	f, err := wrpr.FS.Open(path)
	if err != nil {
		return nil, err
	}

	s, err := f.Stat()
	if err != nil {
		return nil, err
	}

	if s.IsDir() {
		// Have an index file or go home!
		index := filepath.Join(path, "index.html")

		if _, err := wrpr.FS.Open(index); err != nil {
			closeErr := f.Close()
			if closeErr != nil {
				return nil, closeErr
			}

			return nil, err
		}
	}

	return f, nil
}

// serveOneFile is used for serving special-cased files.
func serveOneFile(w http.ResponseWriter, r *http.Request, data []byte, ctype string) {
	w.Header().Add("Content-Type", ctype)

	_, err := w.Write(data)

	if err != nil {
		log.Println("could not write file:", err)
	}
}

// Logger writes out status codes:
type WriterWrapper struct {
	Writer  http.ResponseWriter
	RetCode int
}

func NewRespWriter(w http.ResponseWriter) *WriterWrapper {
	return &WriterWrapper{
		Writer:  w,
		RetCode: 200,
	}
}

func (w *WriterWrapper) WriteHeader(status int) {
	w.RetCode = status
	w.Writer.WriteHeader(status)
}

func (w *WriterWrapper) Header() http.Header {
	return w.Writer.Header()
}

func (w *WriterWrapper) Write(buf []byte) (int, error) {
	return w.Writer.Write(buf)
}

func logRequest(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ww := NewRespWriter(w)
		next.ServeHTTP(ww, r)

		defer func() {
			escapedReqURI := strings.Replace(r.URL.RequestURI(), "\n", "", -1)
			escapedReqURI = strings.Replace(escapedReqURI, "\r", "", -1)

			log.Printf(
				"%s - %s %s %s (%d)",
				r.RemoteAddr, r.Proto, r.Method,
				escapedReqURI, ww.RetCode,
			)
		}()
	})
}
