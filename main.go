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

type readSeekCloser interface {
	io.ReadSeeker
	io.Closer
}

type store interface {
	Post(name string, rd io.Reader, modTime time.Time) (newpath string, err error)
	Get(name string) (rd readSeekCloser, modTime time.Time, err error)
}

var st store

func init() {
	st = fsStore{"data"}
}

func main() {
	http.HandleFunc("/", handler)
	log.Println("Serving on :8080")
	http.ListenAndServe(":8080", nil)
}

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
	}
}

func check(r *http.Request) bool {
	if r.Method != "GET" && r.Method != "POST" && r.Method != "HEAD" {
		return false
	}
	if err := r.ParseForm(); err != nil {
		return false
	}
	if r.Form.Get("name") == "" {
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
		// Provide a wrapper that doesn't write anything to client
		responseWriter = nullWriter{w}
	}
	random := path.Dir(r.Form.Get("name"))
	w.Header().Set("Etag", random)
	http.ServeContent(responseWriter, r, "", modTime, rd)
	rd.Close()
}

type nullWriter struct {
	http.ResponseWriter
}

func (nw nullWriter) Write(p []byte) (n int, err error) {
	return len(p), nil
}
