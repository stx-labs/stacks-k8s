package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"sort"
	"strings"
	"testing"
)

type entry struct {
	name, link string
	kind       byte
	body       string
}

func tarOf(t *testing.T, entries ...entry) *bytes.Buffer {
	t.Helper()
	b := tarStream(t, true, entries...)
	// tar pads its output; rewrite must stop at the end-of-archive marker.
	b.Write(make([]byte, 10240))
	return b
}

// tarStream writes entries and, when marker is set, the end-of-archive marker. Without it the
// stream ends cleanly at a member boundary, as a stream cut short there would.
func tarStream(t *testing.T, marker bool, entries ...entry) *bytes.Buffer {
	t.Helper()
	var b bytes.Buffer
	w := tar.NewWriter(&b)
	for _, e := range entries {
		hdr := &tar.Header{Name: e.name, Linkname: e.link, Typeflag: e.kind, Mode: 0o644, Size: int64(len(e.body))}
		if e.kind != tar.TypeReg {
			hdr.Size = 0
		}
		if err := w.WriteHeader(hdr); err != nil {
			t.Fatal(err)
		}
		if e.kind == tar.TypeReg {
			if _, err := w.Write([]byte(e.body)); err != nil {
				t.Fatal(err)
			}
		}
	}
	finish := w.Flush
	if marker {
		finish = w.Close
	}
	if err := finish(); err != nil {
		t.Fatal(err)
	}
	return &b
}

func members(t *testing.T, archive []byte) map[string]string {
	t.Helper()
	zr, err := gzip.NewReader(bytes.NewReader(archive))
	if err != nil {
		t.Fatal(err)
	}
	r := tar.NewReader(zr)
	out := map[string]string{}
	for {
		hdr, err := r.Next()
		if errors.Is(err, io.EOF) {
			return out
		}
		if err != nil {
			t.Fatal(err)
		}
		body, err := io.ReadAll(r)
		if err != nil {
			t.Fatal(err)
		}
		out[hdr.Name] = string(body)
	}
}

func TestRewriteKeepsChainDataAndDropsPeersMempoolsAndRoot(t *testing.T) {
	src := tarOf(t,
		entry{name: "./", kind: tar.TypeDir},
		entry{name: "./chainstate/", kind: tar.TypeDir},
		entry{name: "./chainstate/vm/index.sqlite", kind: tar.TypeReg, body: "marf"},
		entry{name: "./peer.sqlite", kind: tar.TypeReg, body: "peers"},
		entry{name: "./peer.sqlite-wal", kind: tar.TypeReg, body: "wal"},
		entry{name: "./chainstate/mempool.sqlite", kind: tar.TypeReg, body: "pending"},
		entry{name: "./chainstate/mempool.sqlite-shm", kind: tar.TypeReg, body: "shm"},
		entry{name: "./regtest/blocks/blk00000.dat", kind: tar.TypeReg, body: "blocks"},
		entry{name: "./regtest/peers.dat", kind: tar.TypeReg, body: "bitcoin peers"},
		entry{name: "./regtest/anchors.dat", kind: tar.TypeReg, body: "bitcoin anchors"},
		entry{name: "./regtest/mempool.dat", kind: tar.TypeReg, body: "bitcoin pending"},
		// A network that was itself restored carries the restore marker at its data root.
		entry{name: "./.chainstate-restore", kind: tar.TypeReg, body: "source archive checksum"},
		entry{name: "./chainstate/.chainstate-restore", kind: tar.TypeReg, body: "kept: not at the root"},
	)
	var dst bytes.Buffer
	sum, size, err := rewrite(src, &dst)
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(dst.Bytes())
	if sum != hex.EncodeToString(digest[:]) || size != int64(dst.Len()) {
		t.Fatalf("checksum %s/%d does not describe the written archive", sum, size)
	}
	got := members(t, dst.Bytes())
	names := make([]string, 0, len(got))
	for name := range got {
		names = append(names, name)
	}
	sort.Strings(names)
	want := "chainstate/,chainstate/.chainstate-restore,chainstate/vm/index.sqlite,regtest/blocks/blk00000.dat"
	if strings.Join(names, ",") != want {
		t.Fatalf("members %v, want %s", names, want)
	}
	if got["chainstate/vm/index.sqlite"] != "marf" || got["regtest/blocks/blk00000.dat"] != "blocks" {
		t.Fatalf("contents changed: %v", got)
	}
}

func TestRewriteRejectsMembersOutsideTheDataRootAndLinks(t *testing.T) {
	const outside, unsupported = "leaves the data root", "has unsupported type"
	for name, tc := range map[string]struct {
		entry
		want string
	}{
		"parent":        {entry{name: "../etc/passwd", kind: tar.TypeReg, body: "x"}, outside},
		"nested parent": {entry{name: "./a/../../b", kind: tar.TypeReg, body: "x"}, outside},
		"absolute":      {entry{name: "/etc/passwd", kind: tar.TypeReg, body: "x"}, outside},
		// Links are refused outright, inside the root or not: actor data has none.
		"symlink":           {entry{name: "./current", link: "regtest/blocks", kind: tar.TypeSymlink}, unsupported},
		"escaping link":     {entry{name: "./a/l", link: "../../etc", kind: tar.TypeSymlink}, unsupported},
		"hard link":         {entry{name: "./h", link: "chainstate", kind: tar.TypeLink}, unsupported},
		"device":            {entry{name: "./dev", kind: tar.TypeChar}, unsupported},
		"named pipe (fifo)": {entry{name: "./fifo", kind: tar.TypeFifo}, unsupported},
	} {
		t.Run(name, func(t *testing.T) {
			_, _, err := rewrite(tarOf(t, tc.entry), io.Discard)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("got %v, want %q", err, tc.want)
			}
		})
	}
}

// Go's tar reader reports io.EOF both for the end-of-archive marker and for a stream that just
// stops at a member boundary; only the marker proves the whole volume arrived.
func TestRewriteReportsATruncatedStream(t *testing.T) {
	const noMarker = "data stream ended without the end-of-archive marker"
	body := entry{name: "./a", kind: tar.TypeReg, body: strings.Repeat("x", 4096)}
	full := tarOf(t, body).Bytes()
	// Sparse SQLite pages end in zeros: a dropped body must not pass for the marker.
	zeros := entry{name: "./peer.sqlite", kind: tar.TypeReg, body: string(make([]byte, 4096))}
	for name, tc := range map[string]struct {
		stream []byte
		want   string
	}{
		"inside a body":             {full[:1024], "unexpected EOF"},
		"at a member boundary":      {tarStream(t, false, body).Bytes(), noMarker},
		"after one zero block":      {append(tarStream(t, false, body).Bytes(), make([]byte, 512)...), noMarker},
		"after a dropped zero body": {tarStream(t, false, body, zeros).Bytes(), noMarker},
	} {
		t.Run(name, func(t *testing.T) {
			_, _, err := rewrite(bytes.NewReader(tc.stream), io.Discard)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("got %v, want %q", err, tc.want)
			}
		})
	}
	// Control: exactly the marker, with no record padding after it, is a complete stream.
	exact := tarStream(t, true, body, zeros).Bytes()
	if _, _, err := rewrite(bytes.NewReader(exact), io.Discard); err != nil {
		t.Fatalf("complete stream refused: %v", err)
	}
}
