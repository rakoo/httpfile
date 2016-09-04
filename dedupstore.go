package main

import (
	"bufio"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"io/ioutil"
	"os"
	"path"
	"strings"
	"time"
)

type dedupStore struct {
	root string
}

var _ store = dedupStore{}

func (ds dedupStore) randomPath(name string) string {
	var random [32]byte
	rand.Read(random[:])
	randomString := hex.EncodeToString(random[:])
	filename := path.Base(name)
	return path.Join(ds.root, randomString[:2], randomString[2:], filename)
}

func (ds dedupStore) Post(name string, rd io.Reader, modTime time.Time) (newpath string, err error) {
	chunks := make(chan chunk)
	errorChan := make(chan error)
	done := make(chan struct{})
	go doRoll(rd, chunks, errorChan, done)

	chunkList := make([]string, 0)
loop:
	for {
		select {
		case chunk := <-chunks:
			chunkList = append(chunkList, chunk.hash)
			chunkpath := path.Join(ds.root, chunk.hash[:2], chunk.hash[2:])
			if _, err := os.Stat(chunkpath); err == nil {
				continue
			}
			if err := os.MkdirAll(path.Dir(chunkpath), 0700); err != nil {
				return "", err
			}
			if err := ioutil.WriteFile(chunkpath, chunk.content, 0600); err != nil {
				return "", err
			}
		case err := <-errorChan:
			return "", err
		case <-done:
			break loop
		}
	}

	filepath := ds.randomPath(name)
	if _, err := os.Stat(filepath); err == nil {
		// File already exists
		return "", errors.New("File already exists")
	}
	os.MkdirAll(path.Dir(filepath), 0700)
	f, err := os.Create(filepath)
	if err != nil {
		return "", err
	}
	content := strings.Join(chunkList, "\n")
	_, err = io.WriteString(f, content)
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

type chunk struct {
	hash    string
	content []byte
}

func doRoll(rd io.Reader, chunks chan chunk, errors chan error, done chan struct{}) {
	defer close(done)

	bufr := bufio.NewReader(rd)
	rs := NewRollSum()
	contentBuf := make([]byte, 0)

	chunkit := func(contentBuf []byte) chunk {
		content := make([]byte, len(contentBuf))
		copy(content, contentBuf)
		chunkHash := sha256.Sum256(content)
		ch := chunk{
			hash:    hex.EncodeToString(chunkHash[:]),
			content: content,
		}
		return ch
	}

	for {
		b, err := bufr.ReadByte()
		if err != nil {
			if err == io.EOF {
				chunks <- chunkit(contentBuf)
				return
			} else {
				errors <- err
				return
			}
		}
		contentBuf = append(contentBuf, b)
		rs.Roll(b)
		if rs.OnSplit() {
			chunks <- chunkit(contentBuf)
			contentBuf = contentBuf[:0]
		}
	}
}

func (ds dedupStore) Get(name string) (rd readSeekCloser, modTime time.Time, err error) {
	return
}

func (ds dedupStore) Delete(name string) error {
	return nil
}
