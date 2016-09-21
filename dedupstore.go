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

// dedupStore stores files in the filesystem but dedupes content through
// content-defined chunking: if a given string of bytes appears in the
// middle of the file and is already stored, we don't store it twice.
//
// Each file is identified by a metadata file, ie a file that has the
// correct timestamp, and contains a concatenated list of all chunks
// that comprises it. Chunks are stored separately, one chunk per file.
// If any of the chunk could not be stored, the metadata file is not
// written (and thus the system behaves as though the "file" doesn't
// exist). A second attempt at uploading the "file" will then only store
// chunks that weren't already there; in a way it makes retries slightly
// more efficient.
//
// This construction is far from novel, it has been inspired by bup and
// camlistore mainly. However contrary to those each chunk and each
// metadata file is in its own file, meaning that performance may not be
// stellar.
type dedupStore struct {
	root string
}

var _ store = dedupStore{}

// randomPath generates a random path from dedupStore's root to the
// name, inserting a random string in the middle to avoid overwriting
// other files with the same name and prevent url-guessing.
//
// The storage is similar to the one in git: the random string has 64
// characters (2 for each byte), the first 2 characters (first byte) is
// served as a top-level fanout directory, then the rest of the string
// is used as a directory that contains a single file: the metadata file
// with the name chosen by the client
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

	// We chunk the content, storing chunks one by one if they don't
	// already exist in the filesystem, and gather chunk hashes in a list
	// for later storing in the metadata file
	chunkList := make([]string, 0)
loop:
	for {
		select {
		case chunk := <-chunks:
			chunkList = append(chunkList, chunk.hash)
			chunkpath := path.Join(ds.root, chunk.hash[:2], chunk.hash[2:])
			if _, err := os.Stat(chunkpath); err == nil {
				// chunk already exists, no need to store it again
				continue
			}
			if err := os.MkdirAll(path.Dir(chunkpath), 0755); err != nil {
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

	// Now that all chunks are on the disk, we build the metadata file as
	// the concatenation of all chunk hashes separated by a newline
	// character ("\n"), still with the correct name and modification
	// time. The full path is generated randomly to allow multiple files
	// to have the same name but different identities (and since we dedupe
	// the content, it's not *that* expensive)
	filepath := ds.randomPath(name)
	if _, err := os.Stat(filepath); err == nil {
		// File already exists
		return "", errors.New("File already exists")
	}
	os.MkdirAll(path.Dir(filepath), 0755)
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

	// Build path to be returned to the client.
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

// doRoll chunks content according to good ol' adler32-style rolling
// checksum, building chunks whose boundaries depend only on the bytes
// inside the content. doRoll is expected to be run in its own
// goroutine; when finished, the `done` channel is closed
func doRoll(rd io.Reader, chunks chan chunk, errors chan error, done chan struct{}) {
	defer close(done)

	bufr := bufio.NewReader(rd)
	rs := NewRollSum()
	contentBuf := make([]byte, 0)

	chunkit := func(contentBuf []byte) chunk {
		content := make([]byte, len(contentBuf))
		// we copy the content because contentBuf is reused outside of the
		// function
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

// chunkedReader allows reading and seeking inside a "file" as seen by
// the client, reconstructing content on the fly based on the metadata
// file.
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
	// We need to find the correct chunk to read from (knowing that
	// because there is an offset, we may start from somewhere in the
	// middle). Once we have the chunk, we read the content; if we want to
	// read beyond the current chunk, we advance the internal offset and
	// restart from the beginning
	for len(p) > 0 {
		chunkIndex := sort.Search(len(cr.chunkOffsets), func(i int) bool {
			return cr.chunkOffsets[i] > cr.off
		}) - 1
		if chunkIndex == len(cr.chunkOffsets)-1 {
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
//
// TODO: gc chunks. This will need some more thinking to not be too
// inefficient, probably some kind of inverted index: map of chunk hash
// to number of metadata files using it, each POST increases the
// relevant chunk hashes, each DELETE decreases the relevant chunk
// hashes, when count is 0 it is safe to delete the chunk
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
