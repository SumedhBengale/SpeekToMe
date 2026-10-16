package main

import (
	"bytes"
	"errors"
	"testing"
)

func TestPCMChunkQueuePreservesAudioAndCommitTail(t *testing.T) {
	input := make([]byte, pcmStepBytes*2+314)
	for i := range input {
		input[i] = byte(i)
	}
	var got []byte
	var sizes []int
	write := func(data []byte) error { got = append(got, data...); sizes = append(sizes, len(data)); return nil }
	var queue pcmChunkQueue
	for offset := 0; offset < len(input); {
		end := min(offset+640, len(input))
		if err := queue.push(input[offset:end], write); err != nil {
			t.Fatal(err)
		}
		offset = end
	}
	if len(sizes) != 2 || sizes[0] != pcmStepBytes || sizes[1] != pcmStepBytes {
		t.Fatal(sizes)
	}
	if err := queue.flush(write); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(input, got) || sizes[2] != 314 {
		t.Fatal("audio changed or tail lost", sizes)
	}
	if err := queue.flush(write); err != nil || len(sizes) != 3 {
		t.Fatal("duplicate flush")
	}
}
func TestPCMChunkQueueWriteFailure(t *testing.T) {
	var q pcmChunkQueue
	boom := errors.New("closed")
	if err := q.push(make([]byte, pcmStepBytes), func([]byte) error { return boom }); !errors.Is(err, boom) {
		t.Fatal(err)
	}
	if len(q.pending) != pcmStepBytes {
		t.Fatal("failed audio discarded")
	}
}
