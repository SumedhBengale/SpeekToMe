package main

// The native runtime synchronizes ingress on each PCM message. Assemble its
// 560 ms step rather than sending one message for every ~20 ms Opus packet.
const pcmStepBytes = 16000 * 2 * 560 / 1000

type pcmChunkQueue struct{ pending []byte }

func (q *pcmChunkQueue) push(data []byte, write func([]byte) error) error {
	for len(data) > 0 {
		n := min(pcmStepBytes-len(q.pending), len(data))
		q.pending = append(q.pending, data[:n]...)
		data = data[n:]
		if len(q.pending) == pcmStepBytes {
			if err := q.flush(write); err != nil {
				return err
			}
		}
	}
	return nil
}
func (q *pcmChunkQueue) flush(write func([]byte) error) error {
	if len(q.pending) == 0 {
		return nil
	}
	if err := write(q.pending); err != nil {
		return err
	}
	q.pending = q.pending[:0]
	return nil
}
