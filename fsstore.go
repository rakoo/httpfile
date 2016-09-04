package main

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path"
	"time"
)

type fsStore struct {
	root string
}

func (fs fsStore) path(name string) string {
	var random [32]byte
	rand.Read(random[:])
	randomString := hex.EncodeToString(random[:])
	filename := path.Base(name)
	return path.Join(fs.root, randomString[:2], randomString[2:], filename)
}

func (fs fsStore) Post(name string, rd io.Reader, modTime time.Time) (newpath string, err error) {
	filepath := fs.path(name)
	if _, err := os.Stat(filepath); err == nil {
		// File already exists
		return "", errors.New("File already exists")
	}
	os.MkdirAll(path.Dir(filepath), 0700)
	f, err := os.Create(filepath)
	if err != nil {
		return "", err
	}
	_, err = io.Copy(f, rd)
	if err != nil {
		return "", err
	}
	err = f.Sync()
	if err != nil {
		return "", err
	}
	err = f.Close()
	if err != nil {
		return "", err
	}

	// There are 4 args:
	// * "data"
	// * 2 first chars of random
	// * rest of random
	// * filename
	// we get the full random and filename
	filename := path.Base(filepath)
	dir := path.Dir(filepath)
	randomrest := path.Base(dir)
	random2 := path.Base(path.Dir(dir))
	newpath = path.Join(random2+randomrest, filename)

	return newpath, os.Chtimes(filepath, modTime, modTime)
}

// Note: it's easy to exhaust the server's resources here because each
// file is kept open as long as it's not completely served
func (fs fsStore) Get(name string) (rd readSeekCloser, modTime time.Time, err error) {
	if len(name) < 2 {
		return nil, time.Now(), errors.New("Invalid name")
	}
	path := path.Join(fs.root, name[:2], name[2:])
	f, err := os.Open(path)
	if err != nil {
		return nil, time.Now(), err
	}
	st, err := f.Stat()
	if err != nil {
		return nil, time.Now(), err
	}
	return f, st.ModTime(), nil
}
