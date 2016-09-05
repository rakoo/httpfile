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

func postDefaultContent(t *testing.T, targetUrl string) (res *http.Response, content []byte, err error) {
	content = []byte("This is some content")
	u, _ := url.Parse(targetUrl)
	v := url.Values{}
	v.Set("name", "content.txt")
	u.RawQuery = v.Encode()
	req, err := http.NewRequest("POST", u.String(), bytes.NewReader(content))
	if err != nil {
		return nil, nil, err
	}
	req.Header.Set("Content-Type", "text/plain")
	req.Header.Set("Content-Length", strconv.Itoa(len(content)))
	res, err = http.DefaultClient.Do(req)
	return res, content, err
}

func TestHandlePost(t *testing.T) {
	h := handler{newDummyStore()}
	ts := httptest.NewServer(h)

	res, _, err := postDefaultContent(t, ts.URL)
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

func TestHandleGetHead(t *testing.T) {
	h := handler{newDummyStore()}
	ts := httptest.NewServer(h)

	postRes, content, err := postDefaultContent(t, ts.URL)
	if err != nil {
		t.Fatal(err)
	}

	for _, method := range []string{"GET", "HEAD"} {
		// first test unexistant file
		targetUrl, _ := url.Parse(ts.URL)
		v := url.Values{}
		v.Set("name", "random/unexistant-file.txt")
		targetUrl.RawQuery = v.Encode()
		unexistantReq, _ := http.NewRequest(method, targetUrl.String(), nil)
		unexistantRes, err := http.DefaultClient.Do(unexistantReq)
		if err != nil {
			t.Fatal(err)
		}
		if unexistantRes.StatusCode != http.StatusNotFound {
			t.Fatalf("[%s] got status code %d for unexistant file, expected %d", method, unexistantRes.StatusCode, http.StatusNotFound)
		}

		// then test existing file
		targetUrl, _ = url.Parse(ts.URL)
		location, _ := url.Parse(postRes.Header.Get("Location"))
		targetUrl.RawQuery = location.RawQuery
		getReq, _ := http.NewRequest(method, targetUrl.String(), nil)
		getRes, err := http.DefaultClient.Do(getReq)
		if err != nil {
			t.Fatal(err)
		}

		if getRes.StatusCode != http.StatusOK {
			t.Fatalf("[%s] got status %d, expected %d", method, getRes.StatusCode, http.StatusOK)
		}
		actualSize, err := strconv.Atoi(getRes.Header.Get("Content-Length"))
		if err != nil {
			t.Fatalf("[%s] Couldn't parse Content-Length header (%s) as error: %v", method, getRes.Header.Get("Content-Length"), err)
		}
		if actualSize != len(content) {
			t.Fatalf("[%s] got size %d, expected %d", method, actualSize, len(content))
		}

		mt, _, err := mime.ParseMediaType(getRes.Header.Get("Content-Type"))
		if err != nil {
			t.Fatalf("[%s] Invalid Content-Type header(%s): %v", method, getRes.Header.Get("Content-Type"), err)
		}
		if mt != "text/plain" {
			t.Fatalf("[%s] got mimetype %s, expected text/plain", method, mt)
		}

		expectedEtag := "75ed184249e9bc19675e4d1f766213da71b64278fed2cad5f18a247619205e30"
		if getRes.Header.Get("Etag") != expectedEtag {
			t.Fatalf("[%s] got Etag %s, expected %s", method, getRes.Header.Get("Etag"), expectedEtag)
		}

		if method == "GET" {
			body, err := ioutil.ReadAll(getRes.Body)
			getRes.Body.Close()
			if err != nil {
				t.Fatal("[GET] Couldn't read body:", err)
			}
			if !bytes.Equal(body, content) {
				t.Fatalf("[GET] Invalid body, got %s, expected %s", body, content)
			}
		}
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
