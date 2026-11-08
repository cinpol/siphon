package goceph

import (
	"errors"
	"testing"
)

func TestMonError(t *testing.T) {
	errno := errors.New("rados: ret=-22, Invalid argument")

	got := monError("orch daemon", "Not enough remaining RGW daemons. Use --force\n", errno)
	want := "mon command orch daemon: Not enough remaining RGW daemons. Use --force (rados: ret=-22, Invalid argument)"
	if got.Error() != want {
		t.Errorf("with status:\n got %q\nwant %q", got, want)
	}
	if !errors.Is(got, errno) {
		t.Error("errno is no longer wrapped")
	}

	got = monError("osd set", "  ", errno)
	want = "mon command osd set: rados: ret=-22, Invalid argument"
	if got.Error() != want {
		t.Errorf("without status:\n got %q\nwant %q", got, want)
	}
}
