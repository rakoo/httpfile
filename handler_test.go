package main

import (
	"bytes"
	"encoding/hex"
	"errors"
	"io"
	"io/ioutil"
	"math/rand"
	"mime"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path"
	"strconv"
	"testing"
	"time"
)

func postDefaultContent(t *testing.T, targetUrl string) (res *http.Response, err error) {
	content := []byte("This is some content")
	u, _ := url.Parse(targetUrl)
	v := url.Values{}
	v.Set("name", "content.txt")
	u.RawQuery = v.Encode()
	req, err := http.NewRequest("POST", u.String(), bytes.NewReader(content))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "text/plain")
	req.Header.Set("Content-Length", strconv.Itoa(len(content)))
	return http.DefaultClient.Do(req)
}

func TestHandlePost(t *testing.T) {
	h := handler{newDummyStore()}
	ts := httptest.NewServer(h)

	res, err := postDefaultContent(t, ts.URL)
	if err != nil {
		t.Fatal(err)
	}

	if res.StatusCode != http.StatusCreated {
		t.Fatalf("got statuscode=%d, expected %d", res.StatusCode, http.StatusCreated)
	}
	if _, err := time.Parse(time.RFC1123, res.Header.Get("Date")); err != nil {
		t.Fatal("Couldn't decode Date header in response (%s): %v", res.Header.Get("Date"), err)
	}

	mt, _, err := mime.ParseMediaType(res.Header.Get("Content-Type"))
	if err != nil {
		t.Fatalf("Invalid Content-Type header(%s): %v", res.Header.Get("Content-Type"), err)
	}
	if mt != "text/plain" {
		t.Fatalf("got mimetype %s, expected text/plain", mt)
	}

	location := res.Header.Get("Location")
	expectedLocation := "/?name=75ed184249e9bc19675e4d1f766213da71b64278fed2cad5f18a247619205e30/content.txt"
	if location != expectedLocation {
		t.Fatalf("Invalid location header, got %s, expected %s", location, expectedLocation)
	}
}

// A thread-unsafe dummy store implementation, purely in-memory, good for tests
type file struct {
	name    string
	content []byte
	modTime time.Time
}

var _ store = &dummyStore{}

type dummyStore struct {
	files map[string]file
	r     *rand.Rand
}

func newDummyStore() *dummyStore {
	return &dummyStore{
		files: make(map[string]file),
		r:     rand.New(rand.NewSource(99)),
	}
}

func (ds *dummyStore) Post(name string, rd io.Reader, modTime time.Time) (newpath string, err error) {
	var randBytes [32]byte
	ds.r.Read(randBytes[:])
	fullpath := path.Join(hex.EncodeToString(randBytes[:]), name)
	content, err := ioutil.ReadAll(rd)
	if err != nil {
		return "", err
	}
	ds.files[fullpath] = file{
		name:    fullpath,
		content: content,
		modTime: modTime,
	}
	return fullpath, nil
}

func (ds *dummyStore) Get(name string) (rd readSeekCloser, modTime time.Time, err error) {
	f, ok := ds.files[name]
	if !ok {
		return nil, time.Now(), errors.New("Not found")
	}
	return nopCloser{bytes.NewReader(f.content)}, f.modTime, nil
}

type nopCloser struct {
	io.ReadSeeker
}

func (nc nopCloser) Close() error { return nil }

func (ds *dummyStore) Delete(name string) error {
	_, ok := ds.files[name]
	if !ok {
		return errors.New("Not found")
	}
	delete(ds.files, name)
	return nil
}
