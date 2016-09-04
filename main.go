package main

import (
	"io"
	"log"
	"mime"
	"net/http"
	"path"
	"strconv"
	"time"
)

// readSeekCloser provides io.Close interface to io.ReadSeeker
type readSeekCloser interface {
	io.ReadSeeker
	io.Closer
}

// store is the interface to be implemented by backends for basic
// operations
type store interface {
	Post(name string, rd io.Reader, modTime time.Time) (newpath string, err error)
	Get(name string) (rd readSeekCloser, modTime time.Time, err error)
	Delete(name string) error
}

var st store

func init() {
	st = fsStore{"data"}
}

func main() {
	http.HandleFunc("/", handler)
	log.Println("Serving on :8080")
	err := http.ListenAndServe(":8080", nil)
	if err != nil {
		log.Println(err)
	}
}

// handler dispatches the request to the proper handler depending on the
// method.
// As a security measure, any internal error is printed on stderr but
// never sent to the client; they have no business knowing how the
// server works.
func handler(w http.ResponseWriter, r *http.Request) {
	if !check(r) {
		http.Error(w, "Invalid request", http.StatusBadRequest)
		return
	}
	switch r.Method {
	case "POST":
		handlePost(w, r)
	case "GET", "HEAD":
		handleGet(w, r, r.Method)
	case "DELETE":
		handleDelete(w, r)
	}
}

// check makes sure the request has the correct method, params and
// header values we expect
func check(r *http.Request) bool {
	if r.Method != "GET" && r.Method != "POST" && r.Method != "HEAD" && r.Method != "DELETE" {
		return false
	}
	if err := r.ParseForm(); err != nil {
		return false
	}
	if r.Form.Get("name") == "" {
		return false
	}
	if dir, file := path.Split(r.Form.Get("name")); dir == "" || file == "" {
		return false
	}
	if r.Method == "POST" {
		_, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
		if err != nil {
			return false
		}
		if _, err := strconv.Atoi(r.Header.Get("Content-Length")); err != nil {
			return false
		}
	}
	return true
}

func handlePost(w http.ResponseWriter, r *http.Request) {
	newpath, err := st.Post(r.Form.Get("name"), r.Body, time.Now())
	r.Body.Close()
	if err != nil {
		log.Println("Error putting:", err)
		http.Error(w, "Error putting file", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Location", "/?name="+newpath)
	w.WriteHeader(http.StatusCreated)
}

func handleGet(w http.ResponseWriter, r *http.Request, method string) {
	rd, modTime, err := st.Get(r.Form.Get("name"))
	if err != nil {
		http.Error(w, "Not found", http.StatusNotFound)
		return
	}
	responseWriter := w
	if method == "HEAD" {
		responseWriter = nullWriter{w}
	}
	random := path.Dir(r.Form.Get("name"))
	w.Header().Set("Etag", random)
	http.ServeContent(responseWriter, r, "", modTime, rd)
	rd.Close()
}

// nullWriter is a wrapper around a http.ResponseWriter that doesn't
// actually write anything. It is useful for requests that are
// interested in writing all information except the actual content, such
// as HEAD requests.
type nullWriter struct {
	http.ResponseWriter
}

func (nw nullWriter) Write(p []byte) (n int, err error) {
	return len(p), nil
}

func handleDelete(w http.ResponseWriter, r *http.Request) {
	err := st.Delete(r.Form.Get("name"))
	if err != nil {
		log.Println("Couldn't delete:", err)
		http.Error(w, "Couldn't delete", http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusOK)
}
