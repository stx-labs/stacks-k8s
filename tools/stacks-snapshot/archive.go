package main

import (
	"archive/tar"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"path"
	"strings"
)

// counter counts bytes written through it.
type counter struct {
	w io.Writer
	n int64
}

func (c *counter) Write(b []byte) (int, error) {
	n, err := c.w.Write(b)
	c.n += int64(n)
	return n, err
}

// endMarker is the tar end-of-archive marker: two zero blocks.
const endMarker = 2 * 512

// trailer counts the bytes read through it and keeps the last endMarker of them, so the end of a
// tar stream can be told apart from a stream cut short at a member boundary: Go's tar reader
// reports io.EOF for both.
type trailer struct {
	r    io.Reader
	n    int64
	tail [endMarker]byte
}

func (t *trailer) Read(p []byte) (int, error) {
	n, err := t.r.Read(p)
	b := p[:n]
	if len(b) >= endMarker {
		copy(t.tail[:], b[len(b)-endMarker:])
	} else {
		copy(t.tail[:], t.tail[len(b):])
		copy(t.tail[endMarker-len(b):], b)
	}
	t.n += int64(n)
	return n, err
}

// zeroTail reports whether the last endMarker bytes read were all zero.
func (t *trailer) zeroTail() bool {
	for _, b := range t.tail {
		if b != 0 {
			return false
		}
	}
	return true
}

// rewrite copies a tar stream of an actor's data root into a gzip tar archive and returns the
// archive's sha256 and size. It drops the root entry, the peer and mempool databases, which
// describe the source network's peers and pending transactions, not its chain, and the restore
// marker of a network that was itself restored. Members must stay inside the data root, and the
// stream must end with the end-of-archive marker, so a stream cut short is never archived.
func rewrite(src io.Reader, dst io.Writer) (string, int64, error) {
	sum := sha256.New()
	out := &counter{w: io.MultiWriter(dst, sum)}
	zw := gzip.NewWriter(out)
	tw := tar.NewWriter(zw)
	in := &trailer{r: src}
	tr := tar.NewReader(in)
	for {
		start := in.n
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			// The final header read consumes the previous member's zero padding, then the marker.
			// Every member body is read before the next header, so nothing else counts here.
			if in.n-start < endMarker || !in.zeroTail() {
				return "", 0, errors.New("data stream ended without the end-of-archive marker")
			}
			break
		}
		if err != nil {
			return "", 0, fmt.Errorf("read data stream: %w", err)
		}
		name, keep, err := memberName(hdr)
		if err != nil {
			return "", 0, err
		}
		if !keep {
			// Read the dropped body here so the next header read consumes only padding and headers.
			// #nosec G110 -- the input is an uncompressed tar; each copy is bounded by its header size.
			if _, err = io.Copy(io.Discard, tr); err != nil {
				return "", 0, fmt.Errorf("skip %s: %w", hdr.Name, err)
			}
			continue
		}
		hdr.Name = name
		if err = tw.WriteHeader(hdr); err != nil {
			return "", 0, err
		}
		if hdr.Typeflag == tar.TypeReg {
			// #nosec G110 -- the input is an uncompressed tar; each copy is bounded by its header size.
			if _, err = io.Copy(tw, tr); err != nil {
				return "", 0, fmt.Errorf("copy %s: %w", name, err)
			}
		}
	}
	if err := tw.Close(); err != nil {
		return "", 0, err
	}
	if err := zw.Close(); err != nil {
		return "", 0, err
	}
	return hex.EncodeToString(sum.Sum(nil)), out.n, nil
}

// restoreMarker is the file the operator's restore writes at the data root; a snapshot of a
// restored network must not carry it.
const restoreMarker = ".chainstate-restore"

// memberName returns a member's name relative to the data root and whether to keep it. Only
// directories and regular files are accepted: actor data holds no links, and refusing them keeps
// every member inside the data root without resolving link chains.
func memberName(hdr *tar.Header) (string, bool, error) {
	name := path.Clean(strings.TrimPrefix(hdr.Name, "./"))
	if name == "." {
		return "", false, nil
	}
	if escapes(name) {
		return "", false, fmt.Errorf("member %q leaves the data root", hdr.Name)
	}
	switch hdr.Typeflag {
	case tar.TypeDir:
		return name + "/", true, nil
	case tar.TypeReg:
		if name == restoreMarker || sourceLocal(path.Base(name)) {
			return "", false, nil
		}
		return name, true, nil
	}
	return "", false, fmt.Errorf("member %q has unsupported type %q", hdr.Name, hdr.Typeflag)
}

// sourceLocal reports a file that holds the source network's peers or pending transactions: the
// Stacks peer database and mempool, and Bitcoin Core's peer addresses, anchor peers and mempool.
// The operator's restore drops the same names.
func sourceLocal(base string) bool {
	switch base {
	case "peer.sqlite", "peers.dat", "anchors.dat", "mempool.dat":
		return true
	}
	return strings.HasPrefix(base, "peer.sqlite-") || strings.HasPrefix(base, "mempool.sqlite")
}

func escapes(name string) bool {
	return path.IsAbs(name) || name == ".." || strings.HasPrefix(name, "../")
}
