package main

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"errors"
	"testing"
)

func frame(t *testing.T, v any) []byte {
	t.Helper()
	b, _ := json.Marshal(v)
	var buf bytes.Buffer
	_ = binary.Write(&buf, binary.LittleEndian, uint32(len(b)))
	buf.Write(b)
	return buf.Bytes()
}

func replies(t *testing.T, out *bytes.Buffer) []nativeReply {
	t.Helper()
	var rs []nativeReply
	for out.Len() > 0 {
		var n uint32
		if err := binary.Read(out, binary.LittleEndian, &n); err != nil {
			t.Fatal(err)
		}
		var r nativeReply
		if err := json.Unmarshal(out.Next(int(n)), &r); err != nil {
			t.Fatal(err)
		}
		rs = append(rs, r)
	}
	return rs
}

func TestNativeHostProtocol(t *testing.T) {
	var in bytes.Buffer
	in.Write(frame(t, map[string]string{"cmd": "ping"}))
	in.Write(frame(t, map[string]string{"cmd": "start"}))
	in.Write(frame(t, map[string]string{"cmd": "nope"}))
	var out bytes.Buffer
	started := 0
	err := serveNativeHost(&in, &out, func() error { started++; return nil })
	if err != nil {
		t.Fatal(err)
	}
	rs := replies(t, &out)
	if len(rs) != 3 || !rs[0].OK || !rs[1].OK || rs[2].OK {
		t.Fatalf("unexpected replies: %+v", rs)
	}
	if started != 1 {
		t.Fatalf("start ran %d times, want 1", started)
	}
}

func TestNativeHostReportsStartFailure(t *testing.T) {
	var in, out bytes.Buffer
	in.Write(frame(t, map[string]string{"cmd": "start"}))
	if err := serveNativeHost(&in, &out, func() error { return errors.New("boom") }); err != nil {
		t.Fatal(err)
	}
	if rs := replies(t, &out); len(rs) != 1 || rs[0].OK || rs[0].Error != "boom" {
		t.Fatalf("unexpected: %+v", rs)
	}
}

func TestNativeHostRejectsHugeFrame(t *testing.T) {
	var in, out bytes.Buffer
	_ = binary.Write(&in, binary.LittleEndian, uint32(10<<20))
	if err := serveNativeHost(&in, &out, nil); err == nil {
		t.Fatal("expected error for oversized frame")
	}
}

func TestIsNativeHostLaunch(t *testing.T) {
	yes := [][]string{
		{"odm.exe", "chrome-extension://abcdefghijklmnop/", "--parent-window=123"},
		{"odm.exe", `C:\Users\me\AppData\Roaming\odm\native\odm-native-firefox.json`, "odm@open-download-manager.local"},
	}
	no := [][]string{{"odm.exe"}, {"odm.exe", "add", "http://x"}, {"odm.exe", "serve", "--local"}}
	for _, a := range yes {
		if !isNativeHostLaunch(a) {
			t.Errorf("%v should be a native host launch", a)
		}
	}
	for _, a := range no {
		if isNativeHostLaunch(a) {
			t.Errorf("%v should not be a native host launch", a)
		}
	}
}
