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
	"sort"
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
	if len(name) < 2 {
		return nil, time.Now(), errors.New("Invalid name")
	}
	filepath := path.Join(ds.root, name[:2], name[2:])
	chunkList, err := ioutil.ReadFile(filepath)
	if err != nil {
		return nil, time.Now(), err
	}
	st, err := os.Stat(filepath)
	if err != nil {
		return nil, time.Now(), err
	}

	chunks := strings.Split(string(chunkList), "\n")
	cr, err := newChunkedReader(ds.root, chunks)
	return cr, st.ModTime(), err
}

type chunkedReader struct {
	root   string
	off    int64
	chunks []string

	// All offsets, starting from 0 then incremented by the size of each
	// chunk
	// There are len(chunks)+1 offsets, the last one indicates the total
	// size of the file
	chunkOffsets []int64
}

func newChunkedReader(root string, chunks []string) (*chunkedReader, error) {
	cr := &chunkedReader{
		root:         root,
		chunks:       chunks,
		chunkOffsets: make([]int64, len(chunks)+1),
	}
	totalOffset := int64(0)
	for i, hash := range cr.chunks {
		st, err := os.Stat(path.Join(cr.root, hash[:2], hash[2:]))
		if err != nil {
			return nil, err
		}
		cr.chunkOffsets[i+1] = totalOffset + st.Size()
		totalOffset += st.Size()
	}
	return cr, nil
}

func (cr chunkedReader) Read(p []byte) (n int, err error) {
	for len(p) > 0 {
		chunkIndex := sort.Search(len(cr.chunkOffsets), func(i int) bool {
			return cr.chunkOffsets[i] > cr.off
		}) - 1
		if chunkIndex == len(cr.chunkOffsets) {
			return 0, errors.New("Invalid offset")
		}
		chunkHash := cr.chunks[chunkIndex]
		chunk, err := ioutil.ReadFile(path.Join(cr.root, chunkHash[:2], chunkHash[2:]))
		if err != nil {
			return n, err
		}
		offsetInChunk := cr.off - cr.chunkOffsets[chunkIndex]
		nn := copy(p, chunk[offsetInChunk:])
		n += nn
		cr.off += int64(nn)
		p = p[nn:]
	}

	return n, nil
}

func (cr chunkedReader) Seek(offset int64, whence int) (int64, error) {
	switch whence {
	case io.SeekStart:
		cr.off = offset
	case io.SeekCurrent:
		cr.off += offset
	case io.SeekEnd:
		totalSize := cr.chunkOffsets[len(cr.chunkOffsets)-1]
		cr.off = totalSize - offset
	}
	return cr.off, nil
}

func (cr chunkedReader) Close() error {
	return nil
}

// Delete deletes the metadata file but doesn't delete chunks, since
// they may be used somewhere else
// TODO: gc chunks ?
func (ds dedupStore) Delete(name string) error {
	filepath := path.Join(ds.root, name[:2], name[2:])
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
