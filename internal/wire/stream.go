package wire

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"syscall"
)

type Pilot interface{ Decide(Observation) Action }

const maxLine = 64 * 1024

// readLine consumes one whole record, including an oversized one. Its return
// value aliases the reader's buffer and must be decoded before the next read.
func readLine(r *bufio.Reader) ([]byte, error) {
	data, err := r.ReadSlice('\n')
	if errors.Is(err, bufio.ErrBufferFull) {
		for errors.Is(err, bufio.ErrBufferFull) {
			_, err = r.ReadSlice('\n')
		}
		return nil, err
	}
	if len(data) > maxLine {
		return nil, err
	}
	return data, err
}

func readable(fd int) (bool, error) {
	var reads syscall.FdSet
	if fd < 0 || fd/64 >= len(reads.Bits) {
		return false, fmt.Errorf("unsupported input descriptor %d", fd)
	}
	for {
		reads.Bits[fd/64] = 1 << uint(fd%64)
		count, err := syscall.Select(fd+1, &reads, nil, nil, &syscall.Timeval{})
		if err != syscall.EINTR {
			return count > 0, err
		}
	}
}

func Serve(in *os.File, out io.Writer, name string, init func(Hello) (Pilot, error)) error {
	reader := bufio.NewReaderSize(in, 4*maxLine)
	encoder := json.NewEncoder(out)
	var pilot Pilot
	for {
		var newest []byte
		var done bool
		for {
			line, err := readLine(reader)
			if err != nil && err != io.EOF {
				return err
			}
			if err == io.EOF {
				done = true
			}
			var header struct{ Type string }
			if json.Unmarshal(line, &header) == nil {
				switch header.Type {
				case "hello":
					var h Hello
					if err := json.Unmarshal(line, &h); err != nil {
						return fmt.Errorf("hello: %w", err)
					}
					if h.Protocol != "mechfight/1" {
						return fmt.Errorf("unsupported protocol %q", h.Protocol)
					}
					pilot, err = init(h)
					if err != nil {
						return err
					}
					if err = encoder.Encode(map[string]string{"type": "ready", "name": name}); err != nil {
						return err
					}
				case "obs":
					if pilot == nil {
						return errors.New("observation before hello")
					}
					newest = append(newest[:0], line...)
				case "end":
					done = true
				}
			}
			if done {
				break
			}
			if reader.Buffered() > 0 {
				// A partial next record should not delay an already complete decision.
				data, _ := reader.Peek(reader.Buffered())
				if bytes.IndexByte(data, '\n') >= 0 {
					continue
				}
			}
			ready, err := readable(int(in.Fd()))
			if err != nil {
				return err
			}
			if !ready {
				break
			}
		}
		if newest != nil {
			var obs Observation
			if err := json.Unmarshal(newest, &obs); err != nil {
				return fmt.Errorf("observation: %w", err)
			}
			act := pilot.Decide(obs)
			act.Tick = obs.Tick
			if err := encoder.Encode(act); err != nil {
				return err
			}
		}
		if done {
			return nil
		}
	}
}
