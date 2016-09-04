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

// fsStore is a simple implementation of the store interface. It is not
// designed to be run at high scales, as it will create one file for
// each object and as such will probably be quickly limited by the OS.
//
// You probably want a dedupStore instead, which automatically dedupes
// content on-disk to only store what is truly necessary. This store is
// included for documentation of what is asked of a store.
type fsStore struct {
	root string
}

var _ store = fsStore{}

func (fs fsStore) randomPath(name string) string {
	var random [32]byte
	rand.Read(random[:])
	randomString := hex.EncodeToString(random[:])
	filename := path.Base(name)
	return path.Join(fs.root, randomString[:2], randomString[2:], filename)
}

func (fs fsStore) Post(name string, rd io.Reader, modTime time.Time) (newpath string, err error) {
	filepath := fs.randomPath(name)
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

	// There are 4 args at this point:
	// * "data"
	// * 2 first chars of random
	// * rest of random
	// * filename
	// (sample filepath:
	// <root>/68/901af226d03f4a9d050ec049316848a5f44ad8e91800067d1073485521f050/<filename>
	//
	// we get the full random and filename
	filename := path.Base(filepath)
	dir := path.Dir(filepath)
	randomrest := path.Base(dir)
	random2 := path.Base(path.Dir(dir))
	newpath = path.Join(random2+randomrest, filename)

	return newpath, os.Chtimes(filepath, modTime, modTime)
}

// Note: it's easy to exhaust the server's resources here because each
// file is kept open as long as it's not completely served. Don't use
// this with high volume !
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

func (fs fsStore) Delete(name string) error {
	filepath := path.Join(fs.root, name[:2], name[2:])
	err := os.Remove(filepath)
	if err != nil {
		return err
	}

	untilRandomRest := path.Dir(filepath)
	err = os.Remove(untilRandomRest)
	if err != nil {
		return err
	}

	untilRandom2 := path.Dir(untilRandomRest)
	d, err := os.Open(untilRandom2)
	if err != nil {
		return err
	}
	names, err := d.Readdirnames(-1)
	if err != nil {
		return err
	}
	if len(names) == 0 {
		os.Remove(untilRandom2)
	}
	return nil
}
